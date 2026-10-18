package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"rendolnet/internal/agent"
	"rendolnet/internal/agentclient"
	"rendolnet/internal/store"
)

// trafficRing — скорость по интерфейсам за последний час с шагом 10 секунд,
// для графиков на главной. В базу пишутся минутные средние.
type trafficRing struct {
	mu     sync.Mutex
	size   int
	points []trafficSample
	last   map[string]agent.Link
	lastAt time.Time
	minute map[string][2]int64
	minN   int
}

type trafficSample struct {
	At    time.Time           `json:"at"`
	Rates map[string][2]int64 `json:"rates"` // iface → [rx bps, tx bps]
}

func newTrafficRing(size int) *trafficRing {
	return &trafficRing{size: size, minute: map[string][2]int64{}}
}

// add возвращает минутные средние, когда минута накопилась.
func (t *trafficRing) add(now time.Time, links []agent.Link) []store.TrafficPoint {
	t.mu.Lock()
	defer t.mu.Unlock()
	cur := map[string]agent.Link{}
	for _, l := range links {
		cur[l.Name] = l
	}
	defer func() {
		t.last = cur
		t.lastAt = now
	}()
	if t.last == nil {
		return nil
	}
	dt := now.Sub(t.lastAt).Seconds()
	if dt <= 0 {
		return nil
	}
	s := trafficSample{At: now, Rates: map[string][2]int64{}}
	for name, l := range cur {
		p, ok := t.last[name]
		if !ok || l.RxBytes < p.RxBytes || l.TxBytes < p.TxBytes {
			continue
		}
		r := [2]int64{int64(float64(l.RxBytes-p.RxBytes) * 8 / dt), int64(float64(l.TxBytes-p.TxBytes) * 8 / dt)}
		s.Rates[name] = r
		m := t.minute[name]
		t.minute[name] = [2]int64{m[0] + r[0], m[1] + r[1]}
	}
	t.points = append(t.points, s)
	if len(t.points) > t.size {
		t.points = t.points[len(t.points)-t.size:]
	}
	t.minN++
	if t.minN < 6 {
		return nil
	}
	var out []store.TrafficPoint
	at := now.Truncate(time.Minute)
	for name, sum := range t.minute {
		out = append(out, store.TrafficPoint{At: at, Iface: name, RxBps: sum[0] / int64(t.minN), TxBps: sum[1] / int64(t.minN)})
	}
	t.minute = map[string][2]int64{}
	t.minN = 0
	return out
}

func (t *trafficRing) snapshot() []trafficSample {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]trafficSample(nil), t.points...)
}

// Collect собирает события и трафик с узла и чистит старые данные.
func (s *Server) Collect(ctx context.Context) {
	var cursor uint64
	evTick := time.NewTicker(2 * time.Second)
	trTick := time.NewTicker(10 * time.Second)
	prune := time.NewTicker(time.Hour)
	defer evTick.Stop()
	defer trTick.Stop()
	defer prune.Stop()
	s.pruneOld(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-evTick.C:
			cursor = s.pullEvents(ctx, cursor)
		case now := <-trTick.C:
			s.host.sample()
			links, err := s.agent.Links(ctx)
			if err != nil {
				continue
			}
			if pts := s.traffic.add(now, links); len(pts) > 0 {
				if err := s.st.InsertTraffic(ctx, pts); err != nil {
					s.log.Warn("запись трафика", "err", err)
				}
			}
		case <-prune.C:
			s.pruneOld(ctx)
		}
	}
}

func (s *Server) pullEvents(ctx context.Context, cursor uint64) uint64 {
	for i := 0; i < 10; i++ {
		evs, err := s.agent.Events(ctx, cursor, 2000)
		if err != nil || len(evs) == 0 {
			return cursor
		}
		batch := make([]store.Event, 0, len(evs))
		for _, e := range evs {
			batch = append(batch, store.Event{
				At: e.Time, Type: string(e.Type), Action: e.Action, Proto: int16(e.Proto),
				Src: e.Src, Dst: e.Dst, SrcPort: int32(e.SrcPort), DstPort: int32(e.DstPort),
				FromZone: e.FromZone, ToZone: e.ToZone, RuleID: e.RuleID, Iface: e.Iface,
				Packets: int64(e.Packets), Bytes: int64(e.Bytes), Count: int32(e.Count),
			})
			cursor = e.Seq
		}
		if err := s.st.InsertEvents(ctx, batch); err != nil {
			s.log.Warn("запись событий", "err", err)
			return cursor
		}
		if len(evs) < 2000 {
			return cursor
		}
	}
	return cursor
}

func (s *Server) pruneOld(ctx context.Context) {
	set := s.settings(ctx)
	if n, err := s.st.PruneEvents(ctx, time.Now().AddDate(0, 0, -set.EventRetentionDays)); err == nil && n > 0 {
		s.log.Info("удалены старые события", "count", n)
	}
	_ = s.st.PruneTraffic(ctx, time.Now().AddDate(0, 0, -set.TrafficRetentionDays))
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp := map[string]any{
		"version": s.cfg.Version,
		"host":    s.host.snapshot(s.cfg.DataDir),
		"traffic": s.traffic.snapshot(),
	}
	if st, err := s.agent.Status(ctx); err == nil {
		resp["agent"] = st
	} else {
		resp["agent_error"] = err.Error()
	}
	if links, err := s.agent.Links(ctx); err == nil {
		resp["links"] = links
	}
	if rev, err := s.st.Running(ctx); err == nil {
		rev.Body = nil
		resp["revision"] = rev
	}
	if c, err := s.st.Candidate(ctx); err == nil {
		if rev, err := s.st.Running(ctx); err == nil {
			resp["candidate_dirty"] = diffChanged(configDiff(rev.Body, &c.Body))
		}
	}
	since := time.Now().Add(-time.Hour)
	if srcs, ports, err := s.st.TopDenied(ctx, since, 8); err == nil {
		resp["top_denied_sources"] = srcs
		resp["top_denied_ports"] = ports
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleLinks(w http.ResponseWriter, r *http.Request) {
	links, err := s.agent.Links(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, links)
}

func (s *Server) handleDiag(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var in any
	if r.Method == http.MethodPost {
		var body json.RawMessage
		if !readJSON(w, r, &body) {
			return
		}
		in = body
	}
	out, err := s.agent.Diag(r.Context(), r.Method, name, in)
	var ae *agentclient.Error
	switch {
	case errors.As(err, &ae) && ae.Status < 500:
		writeErr(w, ae.Status, ae.Message)
	case err != nil:
		writeErr(w, http.StatusBadGateway, err.Error())
	default:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}
}

func (s *Server) handleLeases(w http.ResponseWriter, r *http.Request) {
	l, err := s.agent.Leases(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	ss, err := s.agent.Sessions(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].Idle < ss[j].Idle })
	writeJSON(w, http.StatusOK, ss)
}

func (s *Server) handleFlushSessions(w http.ResponseWriter, r *http.Request) {
	if err := s.agent.FlushSessions(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.audit(r, "sessions.flush", "", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCounters(w http.ResponseWriter, r *http.Request) {
	c, err := s.agent.Counters(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.EventFilter{
		Type: q.Get("type"), Action: q.Get("action"), Addr: q.Get("addr"), RuleID: q.Get("rule"),
	}
	f.Port, _ = strconv.Atoi(q.Get("port"))
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	f.Before, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	if v := q.Get("since"); v != "" {
		f.Since, _ = time.Parse(time.RFC3339, v)
	}
	if v := q.Get("until"); v != "" {
		f.Until, _ = time.Parse(time.RFC3339, v)
	}
	evs, err := s.st.Events(r.Context(), f)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if evs == nil {
		evs = []store.Event{}
	}
	writeJSON(w, http.StatusOK, evs)
}

func (s *Server) handleTraffic(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 24*30 {
		hours = 24
	}
	pts, err := s.st.Traffic(r.Context(), time.Now().Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if pts == nil {
		pts = []store.TrafficPoint{}
	}
	writeJSON(w, http.StatusOK, pts)
}
