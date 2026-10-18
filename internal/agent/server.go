package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"rendolnet/internal/netcfg"
)

// Handler — API агента. Слушает unix-сокет, доступный только rendol-api:
// авторизации здесь нет, её роль играют права на файл сокета.
func (a *Agent) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.Status())
	})
	mux.HandleFunc("GET /v1/config", func(w http.ResponseWriter, r *http.Request) {
		ap := a.Active()
		if ap == nil {
			writeErr(w, http.StatusNotFound, "конфиг ещё не применялся")
			return
		}
		writeJSON(w, http.StatusOK, ap)
	})
	mux.HandleFunc("PUT /v1/config", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Revision int64         `json:"revision"`
			Config   netcfg.Config `json:"config"`
			Confirm  int           `json:"confirm_seconds"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "тело запроса: "+err.Error())
			return
		}
		err := a.Push(r.Context(), req.Revision, req.Config, time.Duration(req.Confirm)*time.Second)
		var ve *netcfg.ValidationError
		switch {
		case errors.As(err, &ve):
			writeJSON(w, http.StatusUnprocessableEntity, ve)
		case err != nil:
			writeErr(w, http.StatusInternalServerError, err.Error())
		default:
			writeJSON(w, http.StatusOK, a.Status())
		}
	})
	mux.HandleFunc("POST /v1/config/confirm", func(w http.ResponseWriter, r *http.Request) {
		rev, _ := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
		if err := a.Confirm(rev); err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, a.Status())
	})
	a.diagHandlers(mux)
	mux.HandleFunc("GET /v1/dhcp/leases", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.net.Leases())
	})
	mux.HandleFunc("GET /v1/links", func(w http.ResponseWriter, r *http.Request) {
		links, err := a.net.Links()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, links)
	})
	mux.HandleFunc("GET /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 50000 {
			limit = 5000
		}
		writeJSON(w, http.StatusOK, a.dp.Sessions(limit))
	})
	mux.HandleFunc("DELETE /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		a.dp.FlushSessions()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/counters", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.dp.RuleCounters())
	})
	mux.HandleFunc("GET /v1/events", func(w http.ResponseWriter, r *http.Request) {
		since, _ := strconv.ParseUint(r.URL.Query().Get("since"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 10000 {
			limit = 2000
		}
		writeJSON(w, http.StatusOK, a.events.Since(since, limit))
	})
	return mux
}

// ListenUnix открывает сокет с правами 0660: группа — та же, что у rendol-api.
func ListenUnix(path string) (net.Listener, error) {
	_ = os.Remove(path)
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o660); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
