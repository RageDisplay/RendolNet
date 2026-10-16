package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"rendolnet/internal/agentclient"
	"rendolnet/internal/netcfg"
	"rendolnet/internal/store"
)

func (s *Server) handleRunning(w http.ResponseWriter, r *http.Request) {
	rev, err := s.st.Running(r.Context())
	if err != nil {
		writeErr(w, http.StatusNotFound, "действующего конфига нет")
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

func (s *Server) handleCandidate(w http.ResponseWriter, r *http.Request) {
	c, err := s.candidate(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// candidate возвращает кандидата; если его ещё нет — копию действующего.
func (s *Server) candidate(ctx context.Context) (*store.Candidate, error) {
	c, err := s.st.Candidate(ctx)
	if err == nil {
		return c, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	rev, err := s.st.Running(ctx)
	if err != nil {
		return nil, fmt.Errorf("нет ни кандидата, ни действующего конфига")
	}
	if err := s.st.PutCandidate(ctx, rev.Body, rev.ID, "system"); err != nil {
		return nil, err
	}
	return s.st.Candidate(ctx)
}

func (s *Server) handlePutCandidate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body         netcfg.Config `json:"body"`
		BaseRevision int64         `json:"base_revision"`
		IfUnmodified *time.Time    `json:"if_unmodified,omitempty"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.IfUnmodified != nil {
		if cur, err := s.st.Candidate(r.Context()); err == nil && cur.UpdatedAt.Sub(*req.IfUnmodified).Abs() > time.Millisecond {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":      "кандидата изменил " + cur.UpdatedBy + ", пока вы редактировали",
				"updated_by": cur.UpdatedBy, "updated_at": cur.UpdatedAt,
			})
			return
		}
	}
	req.Body.Schema = netcfg.SchemaVersion
	if err := s.st.PutCandidate(r.Context(), &req.Body, req.BaseRevision, userFrom(r).Username); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	c, _ := s.st.Candidate(r.Context())
	resp := map[string]any{"candidate": c}
	if err := netcfg.ValidateCommit(&req.Body); err != nil {
		resp["validation"] = err
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleResetCandidate(w http.ResponseWriter, r *http.Request) {
	rev, err := s.st.Running(r.Context())
	if err != nil {
		writeErr(w, http.StatusNotFound, "действующего конфига нет")
		return
	}
	if err := s.st.PutCandidate(r.Context(), rev.Body, rev.ID, userFrom(r).Username); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "config.discard", "", nil)
	c, _ := s.st.Candidate(r.Context())
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var from, to any
	q := r.URL.Query()
	if a := q.Get("from"); a != "" {
		id, _ := strconv.ParseInt(a, 10, 64)
		rev, err := s.st.Revision(ctx, id)
		if err != nil {
			writeErr(w, http.StatusNotFound, "нет ревизии "+a)
			return
		}
		from = rev.Body
	} else if rev, err := s.st.Running(ctx); err == nil {
		from = rev.Body
	}
	if b := q.Get("to"); b != "" {
		id, _ := strconv.ParseInt(b, 10, 64)
		rev, err := s.st.Revision(ctx, id)
		if err != nil {
			writeErr(w, http.StatusNotFound, "нет ревизии "+b)
			return
		}
		to = rev.Body
	} else {
		c, err := s.candidate(ctx)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		to = &c.Body
	}
	d := configDiff(from, to)
	writeJSON(w, http.StatusOK, map[string]any{"changed": diffChanged(d), "lines": d})
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	var c netcfg.Config
	if !readJSON(w, r, &c) {
		return
	}
	if err := netcfg.Validate(&c); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"errors": []any{}})
}

func (s *Server) handleCommit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Comment        string `json:"comment"`
		ConfirmSeconds int    `json:"confirm_seconds"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.ConfirmSeconds < 0 || req.ConfirmSeconds > 3600 {
		writeErr(w, http.StatusBadRequest, "время подтверждения — от 0 до 3600 секунд")
		return
	}
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	ctx := r.Context()

	cand, err := s.candidate(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := netcfg.ValidateCommit(&cand.Body); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, err)
		return
	}
	running, _ := s.st.Running(ctx)
	if running != nil {
		if running.State == store.RevPending {
			writeErr(w, http.StatusConflict, fmt.Sprintf("ревизия %d ещё ждёт подтверждения", running.ID))
			return
		}
		if !diffChanged(configDiff(running.Body, &cand.Body)) {
			writeErr(w, http.StatusBadRequest, "изменений нет")
			return
		}
	}

	rev := &store.Revision{Author: userFrom(r).Username, Comment: req.Comment, State: store.RevPending, Body: &cand.Body}
	confirm := time.Duration(req.ConfirmSeconds) * time.Second
	if confirm > 0 {
		dl := time.Now().Add(confirm)
		rev.ConfirmDeadline = &dl
	}
	if err := s.st.InsertRevision(ctx, rev); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, err = s.agent.Push(ctx, rev.ID, &cand.Body, confirm)
	if err != nil {
		_ = s.st.SetRevisionState(ctx, rev.ID, store.RevFailed, err.Error())
		s.audit(r, "config.commit.fail", strconv.FormatInt(rev.ID, 10), map[string]string{"error": err.Error()})
		var ae *agentclient.Error
		if errors.As(err, &ae) && ae.Validation != nil {
			writeJSON(w, http.StatusUnprocessableEntity, ae.Validation)
			return
		}
		writeErr(w, http.StatusBadGateway, "узел не применил конфиг: "+err.Error())
		return
	}
	if confirm == 0 {
		_ = s.st.SetRevisionState(ctx, rev.ID, store.RevCommitted, "")
		rev.State = store.RevCommitted
		rev.ConfirmDeadline = nil
	}
	_ = s.st.PutCandidate(ctx, &cand.Body, rev.ID, userFrom(r).Username)
	s.audit(r, "config.commit", strconv.FormatInt(rev.ID, 10), map[string]any{"comment": req.Comment, "confirm_seconds": req.ConfirmSeconds})
	rev.Body = nil
	writeJSON(w, http.StatusOK, rev)
}

func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	ctx := r.Context()
	running, err := s.st.Running(ctx)
	if err != nil || running.State != store.RevPending {
		writeErr(w, http.StatusConflict, "нет ревизии, ожидающей подтверждения")
		return
	}
	if err := s.agent.Confirm(ctx, running.ID); err != nil {
		writeErr(w, http.StatusConflict, "узел не принял подтверждение: "+err.Error()+". Возможно, время вышло и конфиг уже откатился.")
		return
	}
	_ = s.st.SetRevisionState(ctx, running.ID, store.RevCommitted, "")
	s.audit(r, "config.confirm", strconv.FormatInt(running.ID, 10), nil)
	writeJSON(w, http.StatusOK, map[string]any{"id": running.ID, "state": store.RevCommitted})
}

func (s *Server) handleRevisions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	revs, total, err := s.st.Revisions(r.Context(), limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": revs, "total": total})
}

func (s *Server) handleRevision(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "неверный id")
		return
	}
	rev, err := s.st.Revision(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "нет такой ревизии")
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

// handleLoadRevision кладёт старую ревизию в кандидата. Откат — это обычный
// коммит после загрузки, с тем же подтверждением и следом в истории.
func (s *Server) handleLoadRevision(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "неверный id")
		return
	}
	rev, err := s.st.Revision(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "нет такой ревизии")
		return
	}
	base := int64(0)
	if running, err := s.st.Running(r.Context()); err == nil {
		base = running.ID
	}
	if err := s.st.PutCandidate(r.Context(), rev.Body, base, userFrom(r).Username); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "config.load", strconv.FormatInt(id, 10), nil)
	c, _ := s.st.Candidate(r.Context())
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	var body *netcfg.Config
	name := "running"
	if q := r.URL.Query().Get("revision"); q != "" {
		id, _ := strconv.ParseInt(q, 10, 64)
		rev, err := s.st.Revision(r.Context(), id)
		if err != nil {
			writeErr(w, http.StatusNotFound, "нет такой ревизии")
			return
		}
		body, name = rev.Body, "r"+q
	} else if rev, err := s.st.Running(r.Context()); err == nil {
		body = rev.Body
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="rendolnet-%s.json"`, name))
	writeJSON(w, http.StatusOK, body)
}

// Reconcile сверяет базу с узлом: отмечает откаты по таймеру и возвращает
// узлу последний подтверждённый конфиг, если тот его потерял.
func (s *Server) Reconcile(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		s.reconcileOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) reconcileOnce(ctx context.Context) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	st, err := s.agent.Status(ctx)
	if err != nil {
		return
	}
	pending, _ := s.st.PendingRevisions(ctx)
	for _, p := range pending {
		switch {
		case st.Rollback != nil && st.Rollback.Revision == p.ID:
			_ = s.st.SetRevisionState(ctx, p.ID, store.RevRolledBack, "не подтверждён вовремя")
			s.st.Audit(ctx, store.AuditEntry{Username: "system", Action: "config.rollback", Target: strconv.FormatInt(p.ID, 10)})
		case st.Confirmed == p.ID:
			_ = s.st.SetRevisionState(ctx, p.ID, store.RevCommitted, "")
		case st.Revision != p.ID && p.ConfirmDeadline != nil && time.Now().After(p.ConfirmDeadline.Add(10*time.Second)):
			_ = s.st.SetRevisionState(ctx, p.ID, store.RevRolledBack, "узел работает с другой ревизией")
		}
	}
	if len(pending) > 0 && st.Deadline != nil {
		return
	}
	last, err := s.st.LastConfirmed(ctx)
	if err != nil {
		return
	}
	if st.Revision != last.ID && st.Deadline == nil && len(last.Body.Interfaces) > 0 {
		s.log.Warn("узел работает не с последней подтверждённой ревизией, отправляем её", "node", st.Revision, "db", last.ID)
		if _, err := s.agent.Push(ctx, last.ID, last.Body, 0); err != nil {
			s.log.Error("повторная отправка конфига", "err", err)
		}
	}
}

// Bootstrap заводит первую ревизию из того, с чем узел уже работает
// (конфиг, записанный установщиком).
func (s *Server) Bootstrap(ctx context.Context) error {
	if _, err := s.st.Running(ctx); err == nil {
		return nil
	}
	var body netcfg.Config
	ap, err := s.agent.Config(ctx)
	if errors.Is(err, agentclient.ErrUnavailable) {
		return err
	}
	fromNode := err == nil
	if fromNode {
		body = ap.Config
	} else {
		// пустой конфиг узлу не отправляется: без интерфейсов он снял бы
		// загрузочную защиту, не включив фильтрацию
		s.log.Warn("у узла нет конфига, создаётся пустой", "err", err)
		body = netcfg.Default(netcfg.Seed{Hostname: "rendolnet"})
	}
	rev := &store.Revision{Author: "system", Comment: "начальный конфиг", State: store.RevImported, Body: &body}
	if err := s.st.InsertRevision(ctx, rev); err != nil {
		return err
	}
	if fromNode {
		// перенумеровать ревизию узла под номер в базе
		if _, err := s.agent.Push(ctx, rev.ID, &body, 0); err != nil {
			s.log.Warn("не удалось отправить начальный конфиг", "err", err)
		}
	}
	return s.st.PutCandidate(ctx, &body, rev.ID, "system")
}
