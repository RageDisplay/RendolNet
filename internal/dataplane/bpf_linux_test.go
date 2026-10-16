//go:build linux

package dataplane

import (
	"encoding/binary"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"rendolnet/internal/netcfg"
)

// Тесты гоняют настоящие программы через BPF_PROG_TEST_RUN. Нужны root и
// отдельное сетевое пространство: make test-bpf запускает их в
// привилегированном контейнере.

const (
	actOK       = 0
	actShot     = 2
	actRedirect = 7
	rnMark      = 0x10000000
)

var (
	lanAddr  = netip.MustParseAddr("192.168.1.1")
	wanAddr  = netip.MustParseAddr("203.0.113.2")
	client   = netip.MustParseAddr("192.168.1.10")
	server   = netip.MustParseAddr("192.168.1.20")
	internet = netip.MustParseAddr("198.51.100.1")
)

type env struct {
	t   *testing.T
	dp  *Dataplane
	idx map[string]int
}

func setupEnv(t *testing.T, mutate func(*netcfg.Config)) *env {
	t.Helper()
	if os.Getuid() != 0 || os.Getenv("RN_BPF_TEST") == "" {
		t.Skip("нужны root и RN_BPF_TEST=1 (make test-bpf)")
	}
	mustDummy(t, "rn-lan", "192.168.1.1/24")
	mustDummy(t, "rn-wan", "203.0.113.2/24")
	mustDummy(t, "rn-mgmt", "10.0.0.1/24")
	for _, f := range []string{"/proc/sys/net/ipv4/ip_forward", "/proc/sys/net/ipv6/conf/all/forwarding"} {
		_ = os.WriteFile(f, []byte("1"), 0o644)
	}
	wan, _ := netlink.LinkByName("rn-wan")
	_ = netlink.RouteReplace(&netlink.Route{
		LinkIndex: wan.Attrs().Index,
		Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
		Gw:        net.ParseIP("203.0.113.1"),
	})

	pin := t.TempDir()
	if err := unix.Mount("bpf", pin, "bpf", 0, ""); err != nil {
		t.Fatalf("mount bpffs: %v", err)
	}
	t.Cleanup(func() { _ = unix.Unmount(pin, 0) })

	c := netcfg.Default(netcfg.Seed{
		Hostname: "fw", Mgmt: "rn-mgmt", WAN: "rn-wan", LAN: "rn-lan",
		LANAddr: "192.168.1.1/24", WANAddr: "203.0.113.2/24",
	})
	c.Rules = []netcfg.Rule{
		{ID: "web", Name: "web", Enabled: true, From: []string{"lan"}, To: []string{"wan"}, Services: []string{"tcp/80,443", "icmp"}, Action: netcfg.ActionAllow},
		{ID: "rej", Name: "rej", Enabled: true, From: []string{"lan"}, To: []string{"wan"}, Services: []string{"tcp/25"}, Action: netcfg.ActionReject},
		{ID: "dns", Name: "dns", Enabled: true, From: []string{"lan"}, To: []string{"self"}, Services: []string{"udp/53"}, Action: netcfg.ActionAllow},
	}
	c.NAT.Destination = []netcfg.DNATRule{{
		ID: "fwd", Name: "fwd", Enabled: true, Interface: "rn-wan", Proto: netcfg.ProtoTCP,
		Port: 8080, ToAddress: server.String(), ToPort: 80, AllowPolicy: true,
	}}
	c.Protection.Blocklist = []string{"192.0.2.0/24"}
	if mutate != nil {
		mutate(&c)
	}

	dp, err := Open(slog.Default(), pin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dp.Detach()
		_ = dp.Close()
	})
	plan, err := Compile(&c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := dp.Apply(plan); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, dp: dp, idx: map[string]int{}}
	for _, n := range []string{"rn-lan", "rn-wan", "rn-mgmt"} {
		l, _ := netlink.LinkByName(n)
		e.idx[n] = l.Attrs().Index
	}
	return e
}

func mustDummy(t *testing.T, name, cidr string) {
	t.Helper()
	if l, err := netlink.LinkByName(name); err == nil {
		_ = netlink.LinkDel(l)
	}
	d := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: name}}
	if err := netlink.LinkAdd(d); err != nil {
		t.Fatalf("dummy %s: %v", name, err)
	}
	l, _ := netlink.LinkByName(name)
	addr, _ := netlink.ParseAddr(cidr)
	if err := netlink.AddrAdd(l, addr); err != nil {
		t.Fatal(err)
	}
	if err := netlink.LinkSetUp(l); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = netlink.LinkDel(l) })
}

func (e *env) run(prog *ebpf.Program, frame []byte, iface string, mark uint32) (uint32, []byte) {
	e.t.Helper()
	ctx := make([]byte, 192)
	binary.NativeEndian.PutUint32(ctx[8:], mark)
	binary.NativeEndian.PutUint32(ctx[40:], uint32(e.idx[iface]))
	out := make([]byte, len(frame)+256)
	opts := &ebpf.RunOptions{Data: frame, DataOut: out, Context: ctx, ContextOut: make([]byte, 192)}
	ret, err := prog.Run(opts)
	if err != nil {
		e.t.Fatalf("prog run: %v", err)
	}
	return ret, opts.DataOut
}

func (e *env) in(p testPkt, iface string) (uint32, parsed) {
	ret, out := e.run(e.dp.objs.TcIngress, p.frame(), iface, 0)
	return ret, parseFrame(out)
}

func (e *env) out(p testPkt, iface string, forwarded bool) (uint32, parsed) {
	var mark uint32
	if forwarded {
		mark = rnMark
	}
	ret, out := e.run(e.dp.objs.TcEgress, p.frame(), iface, mark)
	return ret, parseFrame(out)
}

func TestForwardWithMasquerade(t *testing.T) {
	e := setupEnv(t, nil)

	syn := testPkt{src: client, dst: internet, sport: 40000, dport: 80, proto: 6, flags: tcpSYN}
	if ret, _ := e.in(syn, "rn-lan"); ret != actOK {
		t.Fatalf("SYN из LAN: %d", ret)
	}
	ret, got := e.out(syn, "rn-wan", true)
	if ret != actOK {
		t.Fatalf("egress: %d", ret)
	}
	if got.src != wanAddr || got.sport < 41000 {
		t.Fatalf("маскарад не сработал: %s:%d", got.src, got.sport)
	}
	if !got.ipOK || !got.l4OK {
		t.Fatalf("контрольные суммы после SNAT: ip=%v l4=%v", got.ipOK, got.l4OK)
	}
	natPort := got.sport

	synack := testPkt{src: internet, dst: wanAddr, sport: 80, dport: natPort, proto: 6, flags: tcpSYN | tcpACK}
	ret, back := e.in(synack, "rn-wan")
	if ret != actOK {
		t.Fatalf("ответ снаружи отброшен: %d", ret)
	}
	if back.dst != client || back.dport != 40000 || !back.l4OK || !back.ipOK {
		t.Fatalf("обратная трансляция: %s:%d csum=%v/%v", back.dst, back.dport, back.ipOK, back.l4OK)
	}

	ack := testPkt{src: client, dst: internet, sport: 40000, dport: 80, proto: 6, flags: tcpACK}
	e.in(ack, "rn-lan")
	_, got = e.out(ack, "rn-wan", true)
	if got.sport != natPort {
		t.Fatalf("второй пакет сессии получил другой порт: %d != %d", got.sport, natPort)
	}

	sessions := e.dp.Sessions(0)
	found := false
	for _, s := range sessions {
		if s.Src == client && s.DstPort == 80 {
			found = true
			if s.State != "established" || s.RuleID != "web" {
				t.Errorf("сессия: %+v", s)
			}
		}
	}
	if !found {
		t.Fatalf("сессии нет в таблице: %+v", sessions)
	}
	if c := e.dp.RuleCounters()["web"]; c.Packets != 1 {
		t.Errorf("счётчик правила: %+v", c)
	}
}

func TestPolicyDeny(t *testing.T) {
	e := setupEnv(t, nil)
	cases := []struct {
		name  string
		p     testPkt
		iface string
	}{
		{"порт не разрешён", testPkt{src: client, dst: internet, sport: 40001, dport: 22, proto: 6, flags: tcpSYN}, "rn-lan"},
		{"снаружи на узел", testPkt{src: internet, dst: wanAddr, sport: 5555, dport: 22, proto: 6, flags: tcpSYN}, "rn-wan"},
		{"снаружи в LAN", testPkt{src: internet, dst: client, sport: 5555, dport: 80, proto: 6, flags: tcpSYN}, "rn-wan"},
		{"UDP наружу", testPkt{src: client, dst: internet, sport: 5000, dport: 53, proto: 17}, "rn-lan"},
	}
	for _, c := range cases {
		if ret, _ := e.in(c.p, c.iface); ret != actShot {
			t.Errorf("%s: ожидался отбой, получено %d", c.name, ret)
		}
	}
	if s := e.dp.Stats(); s.PolicyDeny != uint64(len(cases)) {
		t.Errorf("счётчик отбоев: %d", s.PolicyDeny)
	}
}

func TestLocalServices(t *testing.T) {
	e := setupEnv(t, nil)

	dns := testPkt{src: client, dst: lanAddr, sport: 5353, dport: 53, proto: 17}
	if ret, _ := e.in(dns, "rn-lan"); ret != actOK {
		t.Fatalf("DNS к узлу из LAN: %d", ret)
	}
	// ответ узла уходит через egress без отметки — сессия уже есть
	reply := testPkt{src: lanAddr, dst: client, sport: 53, dport: 5353, proto: 17}
	if ret, _ := e.out(reply, "rn-lan", false); ret != actOK {
		t.Fatalf("ответ DNS: %d", ret)
	}

	mgmt := testPkt{src: netip.MustParseAddr("10.0.0.50"), dst: netip.MustParseAddr("10.0.0.1"), sport: 50000, dport: 443, proto: 6, flags: tcpSYN}
	if ret, _ := e.in(mgmt, "rn-mgmt"); ret != actOK {
		t.Fatalf("доступ управления: %d", ret)
	}
	mgmtSSHFromLAN := testPkt{src: client, dst: lanAddr, sport: 50000, dport: 443, proto: 6, flags: tcpSYN}
	if ret, _ := e.in(mgmtSSHFromLAN, "rn-lan"); ret != actShot {
		t.Fatalf("порт управления открыт из LAN: %d", ret)
	}
}

func TestLocalOutReplies(t *testing.T) {
	e := setupEnv(t, nil)
	req := testPkt{src: wanAddr, dst: internet, sport: 33000, dport: 443, proto: 6, flags: tcpSYN}
	if ret, got := e.out(req, "rn-wan", false); ret != actOK || got.src != wanAddr || got.sport != 33000 {
		t.Fatalf("исходящее от узла изменено или отброшено: %d %s:%d", ret, got.src, got.sport)
	}
	resp := testPkt{src: internet, dst: wanAddr, sport: 443, dport: 33000, proto: 6, flags: tcpSYN | tcpACK}
	if ret, _ := e.in(resp, "rn-wan"); ret != actOK {
		t.Fatalf("ответ на исходящее узла отброшен: %d", ret)
	}
}

func TestPortForward(t *testing.T) {
	e := setupEnv(t, nil)
	syn := testPkt{src: internet, dst: wanAddr, sport: 6000, dport: 8080, proto: 6, flags: tcpSYN}
	ret, got := e.in(syn, "rn-wan")
	if ret != actOK {
		t.Fatalf("проброс отброшен: %d", ret)
	}
	if got.dst != server || got.dport != 80 || !got.l4OK || !got.ipOK {
		t.Fatalf("DNAT: %s:%d csum=%v/%v", got.dst, got.dport, got.ipOK, got.l4OK)
	}
	reply := testPkt{src: server, dst: internet, sport: 80, dport: 6000, proto: 6, flags: tcpSYN | tcpACK}
	ret, got = e.in(reply, "rn-lan")
	if ret != actOK || got.src != wanAddr || got.sport != 8080 || !got.l4OK {
		t.Fatalf("обратная трансляция DNAT: %d %s:%d", ret, got.src, got.sport)
	}
	// после трансляции ответ не должен снова попасть под маскарад
	ret, got = e.out(testPkt{src: wanAddr, dst: internet, sport: 8080, dport: 6000, proto: 6, flags: tcpSYN | tcpACK}, "rn-wan", true)
	if ret != actOK || got.sport != 8080 {
		t.Fatalf("ответ проброса изменён на выходе: %d %d", ret, got.sport)
	}
}

func TestReject(t *testing.T) {
	e := setupEnv(t, nil)
	syn := testPkt{src: client, dst: internet, sport: 40002, dport: 25, proto: 6, flags: tcpSYN}
	ret, got := e.in(syn, "rn-lan")
	if ret != actRedirect {
		t.Fatalf("ожидался RST (redirect), получено %d", ret)
	}
	if got.src != internet || got.dst != client || got.dport != 40002 || got.flags&tcpRST == 0 {
		t.Fatalf("RST собран неверно: %+v", got)
	}
	if !got.ipOK || !got.l4OK {
		t.Fatalf("контрольные суммы RST: %v/%v", got.ipOK, got.l4OK)
	}
	if got.ack != 1001 {
		t.Errorf("ack в RST: %d", got.ack)
	}
}

func TestPingMasquerade(t *testing.T) {
	e := setupEnv(t, nil)
	echo := testPkt{src: client, dst: internet, proto: 1, icmpType: 8, icmpID: 777}
	if ret, _ := e.in(echo, "rn-lan"); ret != actOK {
		t.Fatalf("ping из LAN: %d", ret)
	}
	_, got := e.out(echo, "rn-wan", true)
	if got.src != wanAddr || !got.l4OK || !got.ipOK {
		t.Fatalf("SNAT ICMP: %s csum=%v/%v", got.src, got.ipOK, got.l4OK)
	}
	reply := testPkt{src: internet, dst: wanAddr, proto: 1, icmpType: 0, icmpID: got.icmpID}
	ret, back := e.in(reply, "rn-wan")
	if ret != actOK || back.dst != client || back.icmpID != 777 || !back.l4OK {
		t.Fatalf("ответ ping: %d %s id=%d", ret, back.dst, back.icmpID)
	}
}

func TestPolicyChangeKillsSessions(t *testing.T) {
	e := setupEnv(t, nil)
	syn := testPkt{src: client, dst: internet, sport: 40003, dport: 443, proto: 6, flags: tcpSYN}
	if ret, _ := e.in(syn, "rn-lan"); ret != actOK {
		t.Fatal(ret)
	}
	c := netcfg.Default(netcfg.Seed{Hostname: "fw", Mgmt: "rn-mgmt", WAN: "rn-wan", LAN: "rn-lan", LANAddr: "192.168.1.1/24", WANAddr: "203.0.113.2/24"})
	c.Rules = []netcfg.Rule{{ID: "web", Name: "web", Enabled: true, From: []string{"lan"}, To: []string{"wan"}, Services: []string{"tcp/80"}, Action: netcfg.ActionAllow}}
	plan, err := Compile(&c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.dp.Apply(plan); err != nil {
		t.Fatal(err)
	}
	next := testPkt{src: client, dst: internet, sport: 40003, dport: 443, proto: 6, flags: tcpACK}
	if ret, _ := e.in(next, "rn-lan"); ret != actShot {
		t.Fatalf("сессия, запрещённая новой политикой, продолжает ходить: %d", ret)
	}
}

func TestIPv6Forward(t *testing.T) {
	e := setupEnv(t, func(c *netcfg.Config) {
		c.Interfaces[2].Addresses = append(c.Interfaces[2].Addresses, "2001:db8:1::1/64")
	})
	lan, _ := netlink.LinkByName("rn-lan")
	a, _ := netlink.ParseAddr("2001:db8:1::1/64")
	_ = netlink.AddrAdd(lan, a)
	wan, _ := netlink.LinkByName("rn-wan")
	_ = netlink.RouteReplace(&netlink.Route{LinkIndex: wan.Attrs().Index, Dst: &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}})

	syn := testPkt{src: netip.MustParseAddr("2001:db8:1::10"), dst: netip.MustParseAddr("2001:db8:99::1"), sport: 40000, dport: 443, proto: 6, flags: tcpSYN}
	if ret, _ := e.in(syn, "rn-lan"); ret != actOK {
		t.Fatalf("IPv6 из LAN: %d", ret)
	}
	bad := syn
	bad.dport = 22
	if ret, _ := e.in(bad, "rn-lan"); ret != actShot {
		t.Fatalf("IPv6 на запрещённый порт: %d", ret)
	}
}

func TestXDPBlocklist(t *testing.T) {
	e := setupEnv(t, nil)
	frame := testPkt{src: netip.MustParseAddr("192.0.2.77"), dst: wanAddr, sport: 1, dport: 80, proto: 6, flags: tcpSYN}.frame()
	ret, err := e.dp.objs.XdpGuard.Run(&ebpf.RunOptions{Data: frame})
	if err != nil {
		t.Fatal(err)
	}
	if ret != 1 { // XDP_DROP
		t.Fatalf("адрес из блок-листа прошёл XDP: %d", ret)
	}
	frame = testPkt{src: internet, dst: wanAddr, sport: 1, dport: 80, proto: 6, flags: tcpSYN}.frame()
	if ret, _ = e.dp.objs.XdpGuard.Run(&ebpf.RunOptions{Data: frame}); ret != 2 {
		t.Fatalf("обычный адрес не прошёл XDP: %d", ret)
	}
}
