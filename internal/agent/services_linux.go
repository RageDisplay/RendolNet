//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"rendolnet/internal/dhcpd"
	"rendolnet/internal/dnsfwd"
	"rendolnet/internal/netcfg"
)

// services — DHCP-серверы и DNS-прокси для LAN. Сервис перезапускается,
// только если его собственные настройки изменились, аренды при этом
// переносятся.
type services struct {
	log       *slog.Logger
	leaseFile string

	mu     sync.Mutex
	dhcp   map[string]*dhcpRun
	dns    *dnsfwd.Forwarder
	dnsKey string
	// observe получает ответы DNS-прокси (домены в объектах адресов)
	observe func([]byte)
	saved   []dhcpd.Lease // аренды с диска, пока серверы не подняты
}

type dhcpRun struct {
	key    string
	srv    *dhcpd.Server
	cancel context.CancelFunc
	done   chan struct{}
}

func newServices(log *slog.Logger, stateDir string) *services {
	s := &services{log: log, dhcp: map[string]*dhcpRun{}}
	if stateDir != "" {
		s.leaseFile = filepath.Join(stateDir, "dhcp-leases.json")
		if b, err := os.ReadFile(s.leaseFile); err == nil {
			_ = json.Unmarshal(b, &s.saved)
		}
	}
	return s
}

func (s *services) sync(c *netcfg.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncDHCP(c)
	s.syncDNS(c)
}

func dhcpConfig(c *netcfg.Config, it *netcfg.Interface) (dhcpd.Config, bool) {
	d := it.DHCPServer
	if d == nil || !d.Enabled || !it.Enabled || it.DHCP || it.Role != netcfg.RoleLAN {
		return dhcpd.Config{}, false
	}
	pfx, ok := netcfg.DHCPPrefix(it)
	if !ok {
		return dhcpd.Config{}, false
	}
	cfg := dhcpd.Config{Iface: it.Name, Addr: pfx, Domain: d.Domain, Lease: time.Duration(d.LeaseMinutes) * time.Minute}
	if cfg.Lease == 0 {
		cfg.Lease = netcfg.DefaultLeaseMinutes * time.Minute
	}
	cfg.Start, _ = netip.ParseAddr(d.RangeStart)
	cfg.End, _ = netip.ParseAddr(d.RangeEnd)
	dns := d.DNS
	if len(dns) == 0 {
		if c.DNSProxy.Enabled {
			dns = []string{pfx.Addr().String()}
		} else {
			dns = c.System.DNS
		}
	}
	for _, a := range dns {
		if ip, err := netip.ParseAddr(a); err == nil && ip.Is4() {
			cfg.DNS = append(cfg.DNS, ip)
		}
	}
	for _, st := range d.Static {
		ip, err := netip.ParseAddr(st.Address)
		if err != nil {
			continue
		}
		cfg.Static = append(cfg.Static, dhcpd.Static{MAC: strings.ToLower(st.MAC), IP: ip, Name: st.Name})
	}
	return cfg, true
}

func (s *services) syncDHCP(c *netcfg.Config) {
	want := map[string]dhcpd.Config{}
	for i := range c.Interfaces {
		if cfg, ok := dhcpConfig(c, &c.Interfaces[i]); ok {
			want[cfg.Iface] = cfg
		}
	}
	for name, r := range s.dhcp {
		cfg, ok := want[name]
		if b, _ := json.Marshal(cfg); ok && string(b) == r.key {
			continue
		}
		r.cancel()
		<-r.done
		s.saved = append(s.saved, r.srv.Snapshot()...)
		delete(s.dhcp, name)
		s.log.Info("dhcp-сервер остановлен", "iface", name)
	}
	for name, cfg := range want {
		if s.dhcp[name] != nil {
			continue
		}
		var prev []dhcpd.Lease
		for _, l := range s.saved {
			if l.Iface == name {
				prev = append(prev, l)
			}
		}
		s.saved = slices.DeleteFunc(s.saved, func(l dhcpd.Lease) bool { return l.Iface == name })
		key, _ := json.Marshal(cfg)
		ctx, cancel := context.WithCancel(context.Background())
		r := &dhcpRun{key: string(key), srv: dhcpd.New(cfg, prev), cancel: cancel, done: make(chan struct{})}
		s.dhcp[name] = r
		go s.serve(ctx, r)
		s.log.Info("dhcp-сервер запущен", "iface", name, "range", cfg.Start.String()+"-"+cfg.End.String())
	}
}

// serve держит сервер поднятым: сокет не откроется, пока интерфейса нет.
func (s *services) serve(ctx context.Context, r *dhcpRun) {
	defer close(r.done)
	for ctx.Err() == nil {
		if err := r.srv.Serve(ctx, s.saveLeases); err != nil {
			s.log.Warn("dhcp-сервер", "err", err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

func (s *services) saveLeases() {
	if s.leaseFile == "" {
		return
	}
	s.mu.Lock()
	all := slices.Clone(s.saved)
	for _, r := range s.dhcp {
		all = append(all, r.srv.Snapshot()...)
	}
	s.mu.Unlock()
	b, _ := json.Marshal(all)
	tmp := s.leaseFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		_ = os.Rename(tmp, s.leaseFile)
	}
}

func (s *services) syncDNS(c *netcfg.Config) {
	var listen []netip.Addr
	var ups []netip.AddrPort
	if c.DNSProxy.Enabled {
		for _, it := range c.Interfaces {
			if it.Role != netcfg.RoleLAN || !it.Enabled || it.DHCP {
				continue
			}
			for _, a := range it.Addresses {
				if p, err := netip.ParsePrefix(a); err == nil && p.Addr().Is4() {
					listen = append(listen, p.Addr())
				}
			}
		}
		for _, d := range c.System.DNS {
			if ip, err := netip.ParseAddr(d); err == nil {
				ups = append(ups, netip.AddrPortFrom(ip, 53))
			}
		}
		if len(ups) == 0 {
			ups = []netip.AddrPort{netip.MustParseAddrPort("1.1.1.1:53"), netip.MustParseAddrPort("8.8.8.8:53")}
		}
	}
	key, _ := json.Marshal([]any{listen, ups})
	if string(key) == s.dnsKey {
		return
	}
	if s.dns != nil {
		s.dns.Close()
		s.dns = nil
	}
	s.dnsKey = string(key)
	if len(listen) == 0 {
		return
	}
	f, err := dnsfwd.Start(s.log, listen, 53, ups, s.observe)
	if err != nil {
		s.log.Warn("dns-прокси", "err", err)
	}
	if f == nil {
		s.dnsKey = "" // повторить при следующем применении
	}
	s.dns = f
	if f != nil {
		s.log.Info("dns-прокси запущен", "listen", listen, "upstreams", ups)
	}
}

// SetDNSObserver задаётся до первого Apply.
func (n *LinuxNetwork) SetDNSObserver(f func([]byte)) {
	n.svc.mu.Lock()
	n.svc.observe = f
	n.svc.mu.Unlock()
}

func (n *LinuxNetwork) Leases() []dhcpd.Lease {
	n.svc.mu.Lock()
	defer n.svc.mu.Unlock()
	out := []dhcpd.Lease{}
	for _, r := range n.svc.dhcp {
		out = append(out, r.srv.Leases(time.Now())...)
	}
	slices.SortStableFunc(out, func(a, b dhcpd.Lease) int { return strings.Compare(a.Iface, b.Iface) })
	return out
}
