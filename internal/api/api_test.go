package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"rendolnet/internal/agent"
	"rendolnet/internal/agentclient"
	"rendolnet/internal/dataplane"
	"rendolnet/internal/netcfg"
	"rendolnet/internal/platform/auth"
	"rendolnet/internal/platform/db"
	"rendolnet/internal/store"
)

// Сквозной тест: настоящий Postgres (RN_TEST_DATABASE_URL), настоящий агент
// поверх unix-сокета, dataplane и сеть — заглушки.

type stubDP struct{ events []dataplane.Event }

func (d *stubDP) Apply(*dataplane.Plan) error { return nil }
func (d *stubDP) RefreshAddresses()           {}
func (d *stubDP) RuleCounters() map[string]dataplane.RuleCounter {
	return map[string]dataplane.RuleCounter{}
}
func (d *stubDP) Stats() dataplane.Stats              { return dataplane.Stats{} }
func (d *stubDP) Sessions(int) []dataplane.Session    { return nil }
func (d *stubDP) GC(func(dataplane.Event)) (int, int) { return 0, 0 }
func (d *stubDP) FlushSessions()                      {}
func (d *stubDP) XDPMode() string                     { return "native" }
func (d *stubDP) Generation() uint32                  { return 1 }
func (d *stubDP) Events(fn func(dataplane.Event)) error {
	for _, e := range d.events {
		fn(e)
	}
	return nil
}

type stubNet struct{}

func (stubNet) Apply(context.Context, *netcfg.Config) error { return nil }
func (stubNet) Links() ([]agent.Link, error) {
	return []agent.Link{{Name: "eth1", Up: true, RxBytes: 1000, TxBytes: 2000}}, nil
}
func (stubNet) Watch(context.Context, func()) {}
func (stubNet) DisableBootGuard() error       { return nil }

type client struct {
	t    *testing.T
	base string
	hc   *http.Client
}

func (c *client) do(method, path string, in any, out any) int {
	c.t.Helper()
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, body)
	req.Header.Set("X-RN-Request", "1")
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("%s %s: %v: %s", method, path, err, raw)
		}
	}
	return resp.StatusCode
}

func setup(t *testing.T) (*client, *Server, *agent.Agent) {
	dsn := os.Getenv("RN_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("нет RN_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := db.Open(ctx, log, dsn, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, _ = pool.Exec(ctx, `drop schema public cascade; create schema public`)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	st := store.New(pool)
	if err := EnsureAdmin(ctx, log, st, "admin"); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	seed := netcfg.Default(netcfg.Seed{Hostname: "fw", Mgmt: "eth0", WAN: "eth1", LAN: "eth2", LANAddr: "192.168.1.1/24", WANDHCP: true})
	dp := &stubDP{events: []dataplane.Event{{
		Time: time.Now().Add(-5 * time.Second), Type: dataplane.EventDeny, Action: "deny", Proto: 6,
		Src: netip.MustParseAddr("198.51.100.7"), Dst: netip.MustParseAddr("203.0.113.2"), DstPort: 22, Count: 1, Packets: 1,
	}}}
	ag := agent.New(log, dp, stubNet{}, filepath.Join(dir, "agent"), "test")
	if err := ag.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ag.Push(ctx, 0, seed, 0); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "agent.sock")
	l, err := agent.ListenUnix(sock)
	if err != nil {
		t.Fatal(err)
	}
	asrv := &http.Server{Handler: ag.Handler()}
	go asrv.Serve(l)
	t.Cleanup(func() { asrv.Close() })

	srv := New(Config{Version: "1.0.0", HostDir: filepath.Join(dir, "host")}, log, st, agentclient.New(sock), auth.NewSigner([]byte("test-key-test-key-test-key-12345")))
	if err := srv.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, base: ts.URL, hc: &http.Client{Jar: jar}}
	if code := c.do("POST", "/v1/auth/login", map[string]string{"username": "admin", "password": "admin"}, nil); code != 200 {
		t.Fatalf("вход: %d", code)
	}
	return c, srv, ag
}

func TestLoginRequired(t *testing.T) {
	c, _, _ := setup(t)
	anon := &client{t: t, base: c.base, hc: &http.Client{}}
	if code := anon.do("GET", "/v1/config/candidate", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("без входа: %d", code)
	}
	if code := anon.do("POST", "/v1/auth/login", map[string]string{"username": "admin", "password": "nope"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("неверный пароль: %d", code)
	}
}

func TestCommitConfirmAndRollback(t *testing.T) {
	c, srv, ag := setup(t)
	ctx := context.Background()

	var cand store.Candidate
	if code := c.do("GET", "/v1/config/candidate", nil, &cand); code != 200 {
		t.Fatalf("кандидат: %d", code)
	}
	cand.Body.Rules[2].Enabled = true
	var put map[string]any
	if code := c.do("PUT", "/v1/config/candidate", map[string]any{"body": cand.Body, "base_revision": cand.BaseRevision}, &put); code != 200 {
		t.Fatalf("сохранение кандидата: %d %v", code, put)
	}
	var diff struct {
		Changed bool       `json:"changed"`
		Lines   []DiffLine `json:"lines"`
	}
	c.do("GET", "/v1/config/diff", nil, &diff)
	if !diff.Changed {
		t.Fatal("diff не видит правку")
	}

	var rev store.Revision
	if code := c.do("POST", "/v1/config/commit", map[string]any{"comment": "ping снаружи", "confirm_seconds": 1}, &rev); code != 200 {
		t.Fatalf("commit: %d", code)
	}
	if rev.State != store.RevPending {
		t.Fatalf("состояние после commit с подтверждением: %s", rev.State)
	}
	if ag.Status().Revision != rev.ID {
		t.Fatalf("агент не получил ревизию %d", rev.ID)
	}
	if code := c.do("POST", "/v1/config/commit", map[string]any{"comment": "ещё"}, nil); code != http.StatusConflict {
		t.Fatalf("второй commit при неподтверждённом: %d", code)
	}

	time.Sleep(1500 * time.Millisecond)
	srv.reconcileOnce(ctx)
	got, _ := srv.st.Revision(ctx, rev.ID)
	if got.State != store.RevRolledBack {
		t.Fatalf("ревизия после истечения: %s", got.State)
	}

	// повтор с подтверждением
	if code := c.do("POST", "/v1/config/commit", map[string]any{"comment": "второй раз", "confirm_seconds": 30}, &rev); code != 200 {
		t.Fatalf("commit: %d", code)
	}
	if code := c.do("POST", "/v1/config/confirm", nil, nil); code != 200 {
		t.Fatalf("confirm: %d", code)
	}
	got, _ = srv.st.Revision(ctx, rev.ID)
	if got.State != store.RevCommitted || ag.Status().Confirmed != rev.ID {
		t.Fatalf("после подтверждения: %s, агент %d", got.State, ag.Status().Confirmed)
	}
	if code := c.do("POST", "/v1/config/commit", map[string]any{"comment": "пусто"}, nil); code != http.StatusBadRequest {
		t.Fatalf("commit без изменений: %d", code)
	}
}

func TestCommitInvalid(t *testing.T) {
	c, _, _ := setup(t)
	var cand store.Candidate
	c.do("GET", "/v1/config/candidate", nil, &cand)
	cand.Body.Rules[0].From = []string{"marsian"}
	c.do("PUT", "/v1/config/candidate", map[string]any{"body": cand.Body}, nil)
	var ve netcfg.ValidationError
	if code := c.do("POST", "/v1/config/commit", map[string]any{}, &ve); code != http.StatusUnprocessableEntity || len(ve.Errors) == 0 {
		t.Fatalf("невалидный кандидат: %d %+v", code, ve)
	}
}

func TestEventsIngest(t *testing.T) {
	c, srv, _ := setup(t)
	srv.pullEvents(context.Background(), 0)
	var evs []store.Event
	c.do("GET", "/v1/events?type=deny&addr=198.51.100.0/24", nil, &evs)
	if len(evs) != 1 || evs[0].DstPort != 22 {
		t.Fatalf("события: %+v", evs)
	}
	var dash map[string]any
	if code := c.do("GET", "/v1/dashboard", nil, &dash); code != 200 || dash["top_denied_sources"] == nil {
		t.Fatalf("дашборд: %d %v", code, dash)
	}
}

func TestUsersAndRoles(t *testing.T) {
	c, _, _ := setup(t)
	if code := c.do("POST", "/v1/users", map[string]string{"username": "ops", "password": "1", "role": "viewer"}, nil); code != 201 {
		t.Fatalf("создание: %d", code)
	}
	jar, _ := cookiejar.New(nil)
	v := &client{t: t, base: c.base, hc: &http.Client{Jar: jar}}
	v.do("POST", "/v1/auth/login", map[string]string{"username": "ops", "password": "1"}, nil)
	if code := v.do("POST", "/v1/config/commit", map[string]any{}, nil); code != http.StatusForbidden {
		t.Fatalf("viewer делает commit: %d", code)
	}
	if code := v.do("GET", "/v1/config/running", nil, nil); code != 200 {
		t.Fatalf("viewer читает конфиг: %d", code)
	}
	var users []store.User
	c.do("GET", "/v1/users", nil, &users)
	for _, u := range users {
		if u.Username == "admin" {
			if code := c.do("DELETE", "/v1/users/"+strconv.FormatInt(u.ID, 10), nil, nil); code != http.StatusConflict {
				t.Fatalf("удаление себя: %d", code)
			}
		}
	}
}
