// Package api — HTTP API и раздача веб-интерфейса rendol-api.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"rendolnet/internal/agentclient"
	"rendolnet/internal/platform/auth"
	"rendolnet/internal/store"
)

type Config struct {
	Version      string
	WebDir       string
	HostDir      string // общий с rendol-updater каталог
	DataDir      string // для отображения занятого места
	ZapretStatus string
	SessionTTL   time.Duration
}

type Server struct {
	cfg    Config
	log    *slog.Logger
	st     *store.Store
	agent  *agentclient.Client
	signer *auth.Signer

	commitMu sync.Mutex
	limiter  *loginLimiter
	traffic  *trafficRing
	host     *hostSampler
	started  time.Time
}

func New(cfg Config, log *slog.Logger, st *store.Store, ag *agentclient.Client, signer *auth.Signer) *Server {
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	return &Server{
		cfg: cfg, log: log, st: st, agent: ag, signer: signer,
		limiter: newLoginLimiter(), traffic: newTrafficRing(360), host: newHostSampler(),
		started: time.Now(),
	}
}

type ctxKey int

const userKey ctxKey = 1

func userFrom(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	need := func(role store.Role, h http.HandlerFunc) http.Handler {
		return s.authn(role, h)
	}
	V, O, A := store.RoleViewer, store.RoleOperator, store.RoleAdmin

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.cfg.Version})
	})
	mux.HandleFunc("POST /v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /v1/auth/banner", s.handleBanner)
	mux.Handle("GET /v1/auth/me", need(V, s.handleMe))
	mux.Handle("POST /v1/auth/password", need(V, s.handleOwnPassword))

	mux.Handle("GET /v1/users", need(A, s.handleUsers))
	mux.Handle("POST /v1/users", need(A, s.handleCreateUser))
	mux.Handle("PUT /v1/users/{id}", need(A, s.handleUpdateUser))
	mux.Handle("POST /v1/users/{id}/password", need(A, s.handleSetPassword))
	mux.Handle("DELETE /v1/users/{id}", need(A, s.handleDeleteUser))
	mux.Handle("GET /v1/tokens", need(V, s.handleTokens))
	mux.Handle("POST /v1/tokens", need(V, s.handleCreateToken))
	mux.Handle("DELETE /v1/tokens/{id}", need(V, s.handleDeleteToken))

	mux.Handle("GET /v1/config/running", need(V, s.handleRunning))
	mux.Handle("GET /v1/config/candidate", need(V, s.handleCandidate))
	mux.Handle("PUT /v1/config/candidate", need(O, s.handlePutCandidate))
	mux.Handle("POST /v1/config/candidate/reset", need(O, s.handleResetCandidate))
	mux.Handle("GET /v1/config/diff", need(V, s.handleDiff))
	mux.Handle("POST /v1/config/validate", need(V, s.handleValidate))
	mux.Handle("POST /v1/config/commit", need(O, s.handleCommit))
	mux.Handle("POST /v1/config/confirm", need(O, s.handleConfirm))
	mux.Handle("GET /v1/config/revisions", need(V, s.handleRevisions))
	mux.Handle("GET /v1/config/revisions/{id}", need(V, s.handleRevision))
	mux.Handle("POST /v1/config/revisions/{id}/load", need(O, s.handleLoadRevision))
	mux.Handle("GET /v1/config/export", need(V, s.handleExport))

	mux.Handle("GET /v1/dashboard", need(V, s.handleDashboard))
	mux.Handle("GET /v1/links", need(V, s.handleLinks))
	mux.Handle("GET /v1/sessions", need(V, s.handleSessions))
	mux.Handle("DELETE /v1/sessions", need(O, s.handleFlushSessions))
	mux.Handle("GET /v1/counters", need(V, s.handleCounters))
	mux.Handle("GET /v1/events", need(V, s.handleEvents))
	mux.Handle("GET /v1/traffic", need(V, s.handleTraffic))

	mux.Handle("GET /v1/system", need(V, s.handleSystem))
	mux.Handle("POST /v1/system/ssh", need(A, s.handleSSH))
	mux.Handle("POST /v1/system/{action}", need(A, s.handlePower))
	mux.Handle("GET /v1/audit", need(A, s.handleAudit))
	mux.Handle("GET /v1/settings", need(V, s.handleSettings))
	mux.Handle("PUT /v1/settings", need(A, s.handlePutSettings))
	mux.Handle("GET /v1/update", need(V, s.handleUpdateStatus))
	mux.Handle("POST /v1/update/upload", need(A, s.handleUpdateUpload))
	mux.Handle("POST /v1/update/apply", need(A, s.handleUpdateApply))
	mux.Handle("DELETE /v1/update/staged", need(A, s.handleUpdateDiscard))

	mux.Handle("GET /v1/zapret/strategies", need(V, s.handleZapretStrategies))
	mux.Handle("GET /v1/zapret/status", need(V, s.handleZapretStatus))

	mux.Handle("/v1/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "нет такого метода API")
	}))
	mux.Handle("/", s.web())

	return s.recoverer(s.secureHeaders(mux))
}

// authn пускает по cookie сессии или по токену API в заголовке Authorization.
func (s *Server) authn(min store.Role, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var u *store.User
		var err error
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			u, err = s.st.UserByToken(r.Context(), auth.HashAPIToken(strings.TrimPrefix(h, "Bearer ")))
		} else if c, cerr := r.Cookie(sessionCookie); cerr == nil {
			// Изменяющие запросы с cookie требуют заголовок, который браузер
			// не отправит с чужого сайта без preflight.
			if r.Method != http.MethodGet && r.Header.Get("X-RN-Request") != "1" {
				writeErr(w, http.StatusForbidden, "нет заголовка X-RN-Request")
				return
			}
			var id int64
			if id, err = s.signer.Verify(c.Value); err == nil {
				u, err = s.st.User(r.Context(), id)
			}
		} else {
			err = errors.New("нет сессии")
		}
		if err != nil || u == nil || u.Disabled {
			writeErr(w, http.StatusUnauthorized, "требуется вход")
			return
		}
		if !u.Role.AtLeast(min) {
			writeErr(w, http.StatusForbidden, "недостаточно прав")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

func (s *Server) secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				s.log.Error("panic", "path", r.URL.Path, "err", v, "stack", string(debug.Stack()))
				writeErr(w, http.StatusInternalServerError, "внутренняя ошибка")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// web раздаёт собранный интерфейс; неизвестные пути получают index.html,
// маршрутизацию делает сам интерфейс.
func (s *Server) web() http.Handler {
	dir := s.cfg.WebDir
	if dir == "" {
		return http.NotFoundHandler()
	}
	root := os.DirFS(dir)
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(root, p); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			p = "index.html"
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) audit(r *http.Request, action, target string, details any) {
	name := ""
	if u := userFrom(r); u != nil {
		name = u.Username
	}
	s.st.Audit(r.Context(), store.AuditEntry{
		Username: name, Action: action, Target: target, Details: details, RemoteAddr: clientIP(r),
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20))
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON: "+err.Error())
		return false
	}
	return true
}
