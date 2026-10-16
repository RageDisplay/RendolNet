package agent

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"rendolnet/internal/dataplane"
	"rendolnet/internal/netcfg"
)

type fakeDP struct {
	mu      sync.Mutex
	applied []*dataplane.Plan
	gen     uint32
}

func (f *fakeDP) Apply(p *dataplane.Plan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, p)
	f.gen++
	return nil
}
func (f *fakeDP) RefreshAddresses()                              {}
func (f *fakeDP) RuleCounters() map[string]dataplane.RuleCounter { return nil }
func (f *fakeDP) Stats() dataplane.Stats                         { return dataplane.Stats{} }
func (f *fakeDP) Sessions(int) []dataplane.Session               { return nil }
func (f *fakeDP) GC(func(dataplane.Event)) (int, int)            { return 0, 0 }
func (f *fakeDP) FlushSessions()                                 {}
func (f *fakeDP) Events(func(dataplane.Event)) error             { return nil }
func (f *fakeDP) XDPMode() string                                { return "native" }
func (f *fakeDP) Generation() uint32                             { return f.gen }

type fakeNet struct{ guardOff int }

func (f *fakeNet) Apply(context.Context, *netcfg.Config) error { return nil }
func (f *fakeNet) Links() ([]Link, error)                      { return nil, nil }
func (f *fakeNet) Watch(context.Context, func())               {}
func (f *fakeNet) Gateways() []Gateway                         { return nil }

func (f *fakeNet) DisableBootGuard() error {
	f.guardOff++
	return nil
}

func cfg(rules int) netcfg.Config {
	c := netcfg.Default(netcfg.Seed{Hostname: "fw", Mgmt: "eth0", WAN: "eth1", LAN: "eth2", LANAddr: "192.168.1.1/24", WANDHCP: true})
	c.Rules = c.Rules[:rules]
	return c
}

func TestConfirmTimeoutRollsBack(t *testing.T) {
	dir := t.TempDir()
	dp, nw := &fakeDP{}, &fakeNet{}
	a := New(slog.Default(), dp, nw, dir, "test")
	ctx := context.Background()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.Push(ctx, 1, cfg(1), 0); err != nil {
		t.Fatal(err)
	}
	if err := a.Push(ctx, 2, cfg(2), 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if s := a.Status(); s.Revision != 2 || s.Confirmed != 1 || s.Deadline == nil {
		t.Fatalf("после применения с подтверждением: %+v", s)
	}
	time.Sleep(300 * time.Millisecond)
	s := a.Status()
	if s.Revision != 1 || s.Rollback == nil || s.Rollback.Revision != 2 {
		t.Fatalf("откат не произошёл: %+v", s)
	}
	if nw.guardOff != 1 {
		t.Errorf("загрузочная защита снималась %d раз", nw.guardOff)
	}
}

func TestConfirmKeepsConfig(t *testing.T) {
	dir := t.TempDir()
	a := New(slog.Default(), &fakeDP{}, &fakeNet{}, dir, "test")
	ctx := context.Background()
	_ = a.Start(ctx)
	_ = a.Push(ctx, 1, cfg(1), 0)
	if err := a.Push(ctx, 2, cfg(2), 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := a.Confirm(2); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if s := a.Status(); s.Revision != 2 || s.Confirmed != 2 || s.Rollback != nil {
		t.Fatalf("подтверждённый конфиг откатился: %+v", s)
	}

	// после перезапуска поднимается подтверждённый
	b := New(slog.Default(), &fakeDP{}, &fakeNet{}, dir, "test")
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if s := b.Status(); s.Revision != 2 {
		t.Fatalf("после перезапуска ревизия %d", s.Revision)
	}
}

func TestRestartDropsPending(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	a := New(slog.Default(), &fakeDP{}, &fakeNet{}, dir, "test")
	_ = a.Start(ctx)
	_ = a.Push(ctx, 1, cfg(1), 0)
	_ = a.Push(ctx, 2, cfg(2), time.Hour)

	b := New(slog.Default(), &fakeDP{}, &fakeNet{}, dir, "test")
	_ = b.Start(ctx)
	if s := b.Status(); s.Revision != 1 {
		t.Fatalf("неподтверждённый конфиг пережил перезапуск: %d", s.Revision)
	}
}

func TestInvalidConfigRejected(t *testing.T) {
	a := New(slog.Default(), &fakeDP{}, &fakeNet{}, t.TempDir(), "test")
	ctx := context.Background()
	_ = a.Start(ctx)
	c := cfg(1)
	c.Rules[0].Action = "drop-it"
	if err := a.Push(ctx, 1, c, 0); err == nil {
		t.Fatal("невалидный конфиг принят")
	}
}

func TestEventAggregation(t *testing.T) {
	r := newEventRing(100)
	now := time.Now().Add(-2 * time.Second)
	for i := 0; i < 50; i++ {
		r.add(dataplane.Event{Time: now, Type: dataplane.EventDeny, DstPort: 22, Packets: 1, Count: 1})
	}
	r.add(dataplane.Event{Time: now, Type: dataplane.EventDeny, DstPort: 23, Packets: 1, Count: 1})
	got := r.Since(0, 0)
	if len(got) != 2 || got[0].Count != 50 {
		t.Fatalf("склейка: %+v", got)
	}
	if more := r.Since(got[1].Seq, 0); len(more) != 0 {
		t.Fatalf("повтор после курсора: %v", more)
	}
}

func TestEventCursorAfterRestart(t *testing.T) {
	r := newEventRing(100)
	r.add(dataplane.Event{Time: time.Now().Add(-2 * time.Second), Type: dataplane.EventDeny, DstPort: 1})
	if got := r.Since(5000, 0); len(got) != 1 {
		t.Fatalf("курсор из прошлой жизни агента скрыл события: %v", got)
	}
}
