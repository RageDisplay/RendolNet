package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"rendolnet/internal/dataplane"
	"rendolnet/internal/netcfg"
)

// Dataplane — то, что агенту нужно от eBPF. Интерфейс ради тестов без ядра.
type Dataplane interface {
	Apply(*dataplane.Plan) error
	RefreshAddresses()
	RuleCounters() map[string]dataplane.RuleCounter
	Stats() dataplane.Stats
	Sessions(limit int) []dataplane.Session
	GC(func(dataplane.Event)) (int, int)
	FlushSessions()
	Events(func(dataplane.Event)) error
	XDPMode() string
	Generation() uint32
}

// Network — настройка интерфейсов, адресов и маршрутов в ядре.
type Network interface {
	Apply(ctx context.Context, c *netcfg.Config) error
	Links() ([]Link, error)
	Watch(ctx context.Context, onChange func())
	DisableBootGuard() error
}

type Link struct {
	Name      string   `json:"name"`
	Index     int      `json:"index"`
	MAC       string   `json:"mac"`
	Kind      string   `json:"kind"`
	Up        bool     `json:"up"`
	Carrier   bool     `json:"carrier"`
	MTU       int      `json:"mtu"`
	Speed     int      `json:"speed_mbps,omitempty"`
	Addresses []string `json:"addresses"`
	RxBytes   uint64   `json:"rx_bytes"`
	TxBytes   uint64   `json:"tx_bytes"`
	RxPackets uint64   `json:"rx_packets"`
	TxPackets uint64   `json:"tx_packets"`
	RxDropped uint64   `json:"rx_dropped"`
	TxDropped uint64   `json:"tx_dropped"`
}

type Rollback struct {
	Time     time.Time `json:"time"`
	Revision int64     `json:"revision"`
	Reason   string    `json:"reason"`
}

type Agent struct {
	log     *slog.Logger
	dp      Dataplane
	net     Network
	st      store
	version string

	mu        sync.Mutex
	active    *Applied
	confirmed *Applied
	timer     *time.Timer
	slots     map[string]uint32
	rollback  *Rollback
	lastErr   string
	events    *eventRing
	bootGuard bool
}

func New(log *slog.Logger, dp Dataplane, nw Network, stateDir, version string) *Agent {
	return &Agent{
		log: log, dp: dp, net: nw, st: store{dir: stateDir}, version: version,
		slots: map[string]uint32{}, events: newEventRing(20000), bootGuard: true,
	}
}

// Start применяет подтверждённый конфиг с диска. Неподтверждённый после
// перезапуска не восстанавливается: если узел перезагрузился посреди
// проверки, правильнее вернуться к рабочему варианту.
func (a *Agent) Start(ctx context.Context) error {
	a.st.remove("pending.json")
	conf, err := a.st.load("confirmed.json")
	if err != nil {
		return err
	}
	if conf == nil {
		a.log.Warn("подтверждённого конфига нет, узел ждёт первого применения из rendol-api")
	} else if err := a.apply(ctx, conf); err != nil {
		a.lastErr = err.Error()
		return fmt.Errorf("agent: применение сохранённого конфига: %w", err)
	} else {
		a.confirmed = conf
	}

	go a.net.Watch(ctx, a.dp.RefreshAddresses)
	go a.gcLoop(ctx)
	go func() {
		if err := a.dp.Events(a.events.add); err != nil {
			a.log.Error("чтение событий dataplane", "err", err)
		}
	}()
	return nil
}

func (a *Agent) apply(ctx context.Context, ap *Applied) error {
	plan, err := dataplane.Compile(&ap.Config, a.slots)
	if err != nil {
		return err
	}
	if err := a.net.Apply(ctx, &ap.Config); err != nil {
		return err
	}
	if err := a.dp.Apply(plan); err != nil {
		return err
	}
	a.slots = plan.RuleSlots
	ap.Applied = time.Now()
	a.active = ap
	a.lastErr = ""
	if a.bootGuard {
		if err := a.net.DisableBootGuard(); err != nil {
			a.log.Warn("не удалось снять загрузочную защиту nftables", "err", err)
		} else {
			a.bootGuard = false
		}
	}
	a.log.Info("конфиг применён", "revision", ap.Revision, "gen", a.dp.Generation())
	return nil
}

var ErrNoPending = errors.New("agent: нет конфига, ожидающего подтверждения")

// Push применяет новый конфиг. confirm > 0 — через столько времени без
// подтверждения агент сам вернёт предыдущий.
func (a *Agent) Push(ctx context.Context, rev int64, c netcfg.Config, confirm time.Duration) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	ap := &Applied{Revision: rev, Config: c}
	if confirm > 0 {
		dl := time.Now().Add(confirm)
		ap.Deadline = &dl
	}
	if err := a.apply(ctx, ap); err != nil {
		a.lastErr = err.Error()
		if a.confirmed != nil {
			if rerr := a.apply(ctx, a.confirmed); rerr != nil {
				a.log.Error("откат после неудачного применения тоже не удался", "err", rerr)
			}
		}
		return err
	}
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}
	if confirm <= 0 {
		a.confirmed = ap
		a.st.remove("pending.json")
		return a.st.save("confirmed.json", ap)
	}
	if err := a.st.save("pending.json", ap); err != nil {
		return err
	}
	a.timer = time.AfterFunc(confirm, func() { a.expire(rev) })
	return nil
}

func (a *Agent) expire(rev int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active == nil || a.active.Revision != rev || a.active.Deadline == nil {
		return
	}
	a.log.Warn("конфиг не подтверждён вовремя, возврат к предыдущему", "revision", rev)
	a.rollback = &Rollback{Time: time.Now(), Revision: rev, Reason: "не подтверждён"}
	a.timer = nil
	a.st.remove("pending.json")
	if a.confirmed != nil {
		if err := a.apply(context.Background(), a.confirmed); err != nil {
			a.lastErr = err.Error()
			a.log.Error("откат не удался", "err", err)
		}
	}
}

func (a *Agent) Confirm(rev int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active == nil || a.active.Deadline == nil || (rev != 0 && a.active.Revision != rev) {
		return ErrNoPending
	}
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}
	a.active.Deadline = nil
	a.confirmed = a.active
	a.st.remove("pending.json")
	return a.st.save("confirmed.json", a.active)
}

type Status struct {
	Version    string          `json:"version"`
	Revision   int64           `json:"revision"`
	Confirmed  int64           `json:"confirmed_revision"`
	Deadline   *time.Time      `json:"deadline,omitempty"`
	Applied    time.Time       `json:"applied"`
	Generation uint32          `json:"generation"`
	XDPMode    string          `json:"xdp_mode"`
	LastError  string          `json:"last_error,omitempty"`
	Rollback   *Rollback       `json:"rollback,omitempty"`
	Stats      dataplane.Stats `json:"stats"`
	BootGuard  bool            `json:"boot_guard"`
}

func (a *Agent) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := Status{
		Version: a.version, Generation: a.dp.Generation(), XDPMode: a.dp.XDPMode(),
		LastError: a.lastErr, Rollback: a.rollback, Stats: a.dp.Stats(), BootGuard: a.bootGuard,
	}
	if a.active != nil {
		s.Revision = a.active.Revision
		s.Deadline = a.active.Deadline
		s.Applied = a.active.Applied
	}
	if a.confirmed != nil {
		s.Confirmed = a.confirmed.Revision
	}
	return s
}

func (a *Agent) Active() *Applied {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.active
}

func (a *Agent) gcLoop(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			removed, live := a.dp.GC(a.events.add)
			if removed > 0 {
				a.log.Debug("уборка сессий", "removed", removed, "live", live)
			}
		}
	}
}
