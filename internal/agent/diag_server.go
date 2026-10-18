package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"regexp"
	"time"
)

var targetRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]{0,252}$`)

// одновременно не больше двух проверок: каждая держит сырой сокет
var diagSlots = make(chan struct{}, 2)

func (a *Agent) dnsServers() []string {
	var out []string
	if ap := a.Active(); ap != nil {
		for _, s := range ap.Config.System.DNS {
			if ip, err := netip.ParseAddr(s); err == nil && ip.Is4() {
				out = append(out, s)
			}
		}
	}
	if len(out) == 0 {
		out = []string{"1.1.1.1", "8.8.8.8"}
	}
	return out
}

func (a *Agent) diagHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/diag/routes", func(w http.ResponseWriter, r *http.Request) {
		reply(w, diagRoutes)
	})
	mux.HandleFunc("GET /v1/diag/fqdn", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.fq.snapshot())
	})
	mux.HandleFunc("GET /v1/diag/neighbors", func(w http.ResponseWriter, r *http.Request) {
		reply(w, diagNeighbors)
	})
	mux.HandleFunc("POST /v1/diag/ping", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Target    string `json:"target"`
			Interface string `json:"interface"`
			Count     int    `json:"count"`
		}
		if !diagRequest(w, r, &req) || !validTarget(w, req.Target) {
			return
		}
		if req.Interface != "" && !ifaceNameRe.MatchString(req.Interface) {
			writeErr(w, http.StatusBadRequest, "недопустимое имя интерфейса")
			return
		}
		if req.Count < 1 || req.Count > 10 {
			req.Count = 4
		}
		withSlot(w, r, 30*time.Second, func(ctx context.Context) (any, error) {
			return diagPing(ctx, req.Target, req.Interface, req.Count, a.dnsServers())
		})
	})
	mux.HandleFunc("POST /v1/diag/trace", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Target  string `json:"target"`
			MaxHops int    `json:"max_hops"`
		}
		if !diagRequest(w, r, &req) || !validTarget(w, req.Target) {
			return
		}
		if req.MaxHops < 1 || req.MaxHops > 30 {
			req.MaxHops = 20
		}
		withSlot(w, r, 45*time.Second, func(ctx context.Context) (any, error) {
			return diagTrace(ctx, req.Target, req.MaxHops, a.dnsServers())
		})
	})
	mux.HandleFunc("POST /v1/diag/lookup", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name   string `json:"name"`
			Server string `json:"server"`
		}
		if !diagRequest(w, r, &req) || !validTarget(w, req.Name) {
			return
		}
		if req.Server == "" {
			req.Server = a.dnsServers()[0]
		} else if _, err := netip.ParseAddr(req.Server); err != nil {
			writeErr(w, http.StatusBadRequest, "сервер DNS указывается IP-адресом")
			return
		}
		withSlot(w, r, 10*time.Second, func(ctx context.Context) (any, error) {
			return diagLookup(ctx, req.Name, req.Server)
		})
	})
}

var ifaceNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`)

func diagRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "тело запроса: "+err.Error())
		return false
	}
	return true
}

func validTarget(w http.ResponseWriter, t string) bool {
	if !targetRe.MatchString(t) {
		writeErr(w, http.StatusBadRequest, "укажите IP-адрес или имя узла")
		return false
	}
	return true
}

func withSlot(w http.ResponseWriter, r *http.Request, limit time.Duration, f func(context.Context) (any, error)) {
	select {
	case diagSlots <- struct{}{}:
		defer func() { <-diagSlots }()
	default:
		writeErr(w, http.StatusTooManyRequests, "уже идут две проверки, подождите")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), limit)
	defer cancel()
	v, err := f(ctx)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func reply[T any](w http.ResponseWriter, f func() (T, error)) {
	v, err := f()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}
