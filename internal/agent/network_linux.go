//go:build linux

package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/nftables"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"rendolnet/internal/netcfg"
)

// Маршруты, которые ставит агент, помечены своим номером протокола: так при
// синхронизации не задеваются маршруты k3s, flannel и интерфейса управления.
const (
	rtProtoStatic = 186
	rtProtoDHCP   = 187
	linkAlias     = "rendolnet"
	bootTable     = "rendolnet_boot"
)

type LinuxNetwork struct {
	log *slog.Logger

	mu   sync.Mutex
	dhcp map[string]*dhcpRunner
}

func NewLinuxNetwork(log *slog.Logger) *LinuxNetwork {
	return &LinuxNetwork{log: log, dhcp: map[string]*dhcpRunner{}}
}

func (n *LinuxNetwork) Apply(ctx context.Context, c *netcfg.Config) error {
	n.sysctls()

	byName := map[string]*netcfg.Interface{}
	for i := range c.Interfaces {
		byName[c.Interfaces[i].Name] = &c.Interfaces[i]
	}

	var errs []error
	if err := n.syncVLANs(c); err != nil {
		errs = append(errs, err)
	}
	for _, it := range c.Interfaces {
		if it.Role == netcfg.RoleMgmt {
			continue // адресом управления владеет ОС, см. docs/network.md
		}
		if err := n.applyIface(&it); err != nil {
			errs = append(errs, err)
		}
	}
	n.syncDHCP(c)
	if err := n.syncRoutes(c); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (n *LinuxNetwork) sysctls() {
	set := map[string]string{
		"net/ipv4/ip_forward":                       "1",
		"net/ipv6/conf/all/forwarding":              "1",
		"net/ipv4/conf/all/rp_filter":               "2",
		"net/ipv4/conf/default/rp_filter":           "2",
		"net/ipv4/ip_local_port_range":              "32768\t40999",
		"net/netfilter/nf_conntrack_tcp_be_liberal": "1",
	}
	for k, v := range set {
		p := filepath.Join("/proc/sys", k)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := os.WriteFile(p, []byte(v), 0o644); err != nil {
			n.log.Warn("sysctl", "key", k, "err", err)
		}
	}
}

func (n *LinuxNetwork) syncVLANs(c *netcfg.Config) error {
	want := map[string]*netcfg.Interface{}
	for i := range c.Interfaces {
		if c.Interfaces[i].VLAN != nil {
			want[c.Interfaces[i].Name] = &c.Interfaces[i]
		}
	}
	links, err := netlink.LinkList()
	if err != nil {
		return err
	}
	for _, l := range links {
		if l.Type() == "vlan" && l.Attrs().Alias == linkAlias {
			if _, ok := want[l.Attrs().Name]; !ok {
				_ = netlink.LinkDel(l)
			}
		}
	}
	var errs []error
	for name, it := range want {
		parent, err := netlink.LinkByName(it.VLAN.Parent)
		if err != nil {
			errs = append(errs, fmt.Errorf("vlan %s: родитель %s: %w", name, it.VLAN.Parent, err))
			continue
		}
		if l, err := netlink.LinkByName(name); err == nil {
			if v, ok := l.(*netlink.Vlan); ok && v.VlanId == it.VLAN.ID && v.ParentIndex == parent.Attrs().Index {
				continue
			}
			_ = netlink.LinkDel(l)
		}
		v := &netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: name, ParentIndex: parent.Attrs().Index}, VlanId: it.VLAN.ID}
		if err := netlink.LinkAdd(v); err != nil {
			errs = append(errs, fmt.Errorf("vlan %s: %w", name, err))
			continue
		}
		l, _ := netlink.LinkByName(name)
		_ = netlink.LinkSetAlias(l, linkAlias)
		_ = netlink.LinkSetUp(parent)
	}
	return errors.Join(errs...)
}

func (n *LinuxNetwork) applyIface(it *netcfg.Interface) error {
	l, err := netlink.LinkByName(it.Name)
	if err != nil {
		return fmt.Errorf("интерфейс %s: %w", it.Name, err)
	}
	if it.MTU > 0 && l.Attrs().MTU != it.MTU {
		if err := netlink.LinkSetMTU(l, it.MTU); err != nil {
			return fmt.Errorf("%s: MTU: %w", it.Name, err)
		}
	}
	if !it.Enabled {
		return netlink.LinkSetDown(l)
	}
	if err := netlink.LinkSetUp(l); err != nil {
		return fmt.Errorf("%s: up: %w", it.Name, err)
	}
	if it.DHCP {
		return nil
	}

	want := map[string]bool{}
	for _, a := range it.Addresses {
		p := netip.MustParsePrefix(a)
		want[p.String()] = true
	}
	cur, err := netlink.AddrList(l, netlink.FAMILY_ALL)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, a := range cur {
		if a.IP.IsLinkLocalUnicast() {
			continue
		}
		ones, _ := a.Mask.Size()
		ip, _ := netip.AddrFromSlice(a.IP)
		key := netip.PrefixFrom(ip.Unmap(), ones).String()
		if !want[key] {
			_ = netlink.AddrDel(l, &a)
			continue
		}
		have[key] = true
	}
	for key := range want {
		if have[key] {
			continue
		}
		addr, err := netlink.ParseAddr(key)
		if err != nil {
			return err
		}
		if err := netlink.AddrAdd(l, addr); err != nil && !errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("%s: адрес %s: %w", it.Name, key, err)
		}
	}
	return nil
}

func (n *LinuxNetwork) syncRoutes(c *netcfg.Config) error {
	type rkey struct{ dst, gw string }
	want := map[rkey]netcfg.Route{}
	for _, r := range c.Routes {
		dst := netip.MustParsePrefix(r.Destination).Masked()
		want[rkey{dst.String(), r.Gateway}] = r
	}
	cur, err := netlink.RouteListFiltered(netlink.FAMILY_ALL, &netlink.Route{Protocol: rtProtoStatic}, netlink.RT_FILTER_PROTOCOL)
	if err != nil {
		return err
	}
	for _, r := range cur {
		dst := "0.0.0.0/0"
		if r.Family == unix.AF_INET6 {
			dst = "::/0"
		}
		if r.Dst != nil {
			dst = r.Dst.String()
		}
		gw := ""
		if r.Gw != nil {
			gw = r.Gw.String()
		}
		if _, ok := want[rkey{dst, gw}]; !ok {
			_ = netlink.RouteDel(&r)
		}
	}
	var errs []error
	for _, r := range want {
		pfx := netip.MustParsePrefix(r.Destination).Masked()
		route := &netlink.Route{
			Dst:      &net.IPNet{IP: pfx.Addr().AsSlice(), Mask: net.CIDRMask(pfx.Bits(), pfx.Addr().BitLen())},
			Protocol: rtProtoStatic,
			Priority: r.Metric,
		}
		if r.Gateway != "" {
			route.Gw = net.ParseIP(r.Gateway)
		}
		if r.Interface != "" {
			l, err := netlink.LinkByName(r.Interface)
			if err != nil {
				errs = append(errs, fmt.Errorf("маршрут %s: %w", r.Destination, err))
				continue
			}
			route.LinkIndex = l.Attrs().Index
		}
		if err := netlink.RouteReplace(route); err != nil {
			errs = append(errs, fmt.Errorf("маршрут %s: %w", r.Destination, err))
		}
	}
	return errors.Join(errs...)
}

func (n *LinuxNetwork) syncDHCP(c *netcfg.Config) {
	n.mu.Lock()
	defer n.mu.Unlock()
	want := map[string]bool{}
	for _, it := range c.Interfaces {
		if it.DHCP && it.Enabled && it.Role != netcfg.RoleMgmt {
			want[it.Name] = true
		}
	}
	for name, r := range n.dhcp {
		if !want[name] {
			r.stop()
			delete(n.dhcp, name)
		}
	}
	for name := range want {
		if _, ok := n.dhcp[name]; !ok {
			r := newDHCPRunner(n.log.With("iface", name), name)
			n.dhcp[name] = r
			go r.run()
		}
	}
}

// skipLink — служебные интерфейсы узла, которые не показываются в интерфейсе.
func skipLink(name string) bool {
	for _, p := range []string{"lo", "cni", "flannel", "veth", "kube-", "docker", "dummy", "tunl", "sit", "ip6tnl"} {
		if name == p || strings.HasPrefix(name, p) && p != "lo" {
			return true
		}
	}
	return false
}

func (n *LinuxNetwork) Links() ([]Link, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}
	var out []Link
	for _, l := range links {
		a := l.Attrs()
		if skipLink(a.Name) {
			continue
		}
		lk := Link{
			Name: a.Name, Index: a.Index, MAC: a.HardwareAddr.String(), Kind: l.Type(),
			Up: a.Flags&net.FlagUp != 0, Carrier: a.OperState == netlink.OperUp, MTU: a.MTU,
			Addresses: []string{},
		}
		if s := a.Statistics; s != nil {
			lk.RxBytes, lk.TxBytes = s.RxBytes, s.TxBytes
			lk.RxPackets, lk.TxPackets = s.RxPackets, s.TxPackets
			lk.RxDropped, lk.TxDropped = s.RxDropped, s.TxDropped
		}
		if b, err := os.ReadFile(filepath.Join("/sys/class/net", a.Name, "speed")); err == nil {
			if v, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && v > 0 {
				lk.Speed = v
			}
		}
		if addrs, err := netlink.AddrList(l, netlink.FAMILY_ALL); err == nil {
			for _, ad := range addrs {
				lk.Addresses = append(lk.Addresses, ad.IPNet.String())
			}
		}
		out = append(out, lk)
	}
	return out, nil
}

// Watch вызывает onChange при смене адресов: DHCP на WAN меняет адрес, и
// маскарад должен узнать об этом сразу, а не при следующем коммите.
func (n *LinuxNetwork) Watch(ctx context.Context, onChange func()) {
	for ctx.Err() == nil {
		ch := make(chan netlink.AddrUpdate, 64)
		done := make(chan struct{})
		if err := netlink.AddrSubscribe(ch, done); err != nil {
			n.log.Warn("подписка на адреса", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		debounce := time.NewTimer(time.Hour)
		debounce.Stop()
	loop:
		for {
			select {
			case <-ctx.Done():
				close(done)
				return
			case _, ok := <-ch:
				if !ok {
					break loop
				}
				debounce.Reset(300 * time.Millisecond)
			case <-debounce.C:
				onChange()
			}
		}
		close(done)
	}
}

// DisableBootGuard удаляет таблицу nftables, которая с момента загрузки
// держит транзитный трафик закрытым, пока не поднимется eBPF-политика.
func (n *LinuxNetwork) DisableBootGuard() error {
	conn, err := nftables.New()
	if err != nil {
		return err
	}
	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return err
	}
	for _, t := range tables {
		if t.Name == bootTable {
			conn.DelTable(t)
			return conn.Flush()
		}
	}
	return nil
}
