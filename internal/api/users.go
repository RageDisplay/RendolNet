package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"rendolnet/internal/platform/auth"
	"rendolnet/internal/store"
)

const sessionCookie = "rn_session"

// loginLimiter — не больше 10 неудачных попыток с адреса за 5 минут.
type loginLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{hits: map[string][]time.Time{}} }

func (l *loginLimiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-5 * time.Minute)
	var keep []time.Time
	for _, t := range l.hits[ip] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	l.hits[ip] = keep
	return len(keep) >= 10
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[ip] = append(l.hits[ip], time.Now())
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, ip)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	ip := clientIP(r)
	if s.limiter.blocked(ip) {
		writeErr(w, http.StatusTooManyRequests, "слишком много попыток, подождите несколько минут")
		return
	}
	u, err := s.st.UserByName(r.Context(), strings.TrimSpace(req.Username))
	if err != nil || u.Disabled || !auth.CheckPassword(u.PasswordHash, req.Password) {
		s.limiter.fail(ip)
		s.st.Audit(r.Context(), store.AuditEntry{Username: req.Username, Action: "login.fail", RemoteAddr: ip})
		writeErr(w, http.StatusUnauthorized, "неверное имя или пароль")
		return
	}
	s.limiter.reset(ip)
	s.st.TouchLogin(r.Context(), u.ID)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: s.signer.Issue(u.ID, s.cfg.SessionTTL), Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode,
		MaxAge: int(s.cfg.SessionTTL.Seconds()),
	})
	s.st.Audit(r.Context(), store.AuditEntry{Username: u.Username, Action: "login", RemoteAddr: ip})
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFrom(r))
}

func (s *Server) handleOwnPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	u := userFrom(r)
	if !auth.CheckPassword(u.PasswordHash, req.Current) {
		writeErr(w, http.StatusForbidden, "текущий пароль указан неверно")
		return
	}
	if req.New == "" {
		writeErr(w, http.StatusBadRequest, "пустой пароль")
		return
	}
	h, err := auth.HashPassword(req.New)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.SetPassword(r.Context(), u.ID, h); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "user.password", u.Username, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.Users(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string     `json:"username"`
		FullName string     `json:"full_name"`
		Role     store.Role `json:"role"`
		Password string     `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" || !req.Role.Valid() {
		writeErr(w, http.StatusBadRequest, "нужны имя, пароль и роль admin, operator или viewer")
		return
	}
	h, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	u := &store.User{Username: req.Username, FullName: req.FullName, Role: req.Role, PasswordHash: h}
	if err := s.st.CreateUser(r.Context(), u); err != nil {
		writeErr(w, http.StatusConflict, "пользователь с таким именем уже есть")
		return
	}
	s.audit(r, "user.create", u.Username, map[string]any{"role": u.Role})
	writeJSON(w, http.StatusCreated, u)
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "неверный id")
		return
	}
	var req struct {
		FullName string     `json:"full_name"`
		Role     store.Role `json:"role"`
		Disabled bool       `json:"disabled"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !req.Role.Valid() {
		writeErr(w, http.StatusBadRequest, "неизвестная роль")
		return
	}
	u, err := s.st.User(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "нет такого пользователя")
		return
	}
	if u.Role == store.RoleAdmin && (req.Role != store.RoleAdmin || req.Disabled) {
		if n, _ := s.st.CountAdmins(r.Context()); n <= 1 {
			writeErr(w, http.StatusConflict, "нельзя убрать последнего администратора")
			return
		}
	}
	u.FullName, u.Role, u.Disabled = req.FullName, req.Role, req.Disabled
	if err := s.st.UpdateUser(r.Context(), u); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "user.update", u.Username, req)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "неверный id")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Password == "" {
		writeErr(w, http.StatusBadRequest, "пустой пароль")
		return
	}
	u, err := s.st.User(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "нет такого пользователя")
		return
	}
	h, _ := auth.HashPassword(req.Password)
	if err := s.st.SetPassword(r.Context(), id, h); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "user.password", u.Username, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "неверный id")
		return
	}
	if id == userFrom(r).ID {
		writeErr(w, http.StatusConflict, "нельзя удалить самого себя")
		return
	}
	u, err := s.st.User(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "нет такого пользователя")
		return
	}
	if u.Role == store.RoleAdmin {
		if n, _ := s.st.CountAdmins(r.Context()); n <= 1 {
			writeErr(w, http.StatusConflict, "нельзя удалить последнего администратора")
			return
		}
	}
	if err := s.st.DeleteUser(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "user.delete", u.Username, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Tokens(r.Context(), userFrom(r).ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Days int    `json:"days"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "нужно имя токена")
		return
	}
	plain, hash, err := auth.NewAPIToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var exp *time.Time
	if req.Days > 0 {
		t := time.Now().AddDate(0, 0, req.Days)
		exp = &t
	}
	tok, err := s.st.CreateToken(r.Context(), userFrom(r).ID, req.Name, hash, exp)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "token.create", req.Name, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"token": plain, "info": tok})
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "неверный id")
		return
	}
	err := s.st.DeleteToken(r.Context(), userFrom(r).ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "нет такого токена")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "token.delete", strconv.FormatInt(id, 10), nil)
	w.WriteHeader(http.StatusNoContent)
}
