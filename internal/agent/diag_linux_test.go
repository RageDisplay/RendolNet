//go:build linux

package agent

import (
	"context"
	"net"
	"os"
	"runtime"
	"testing"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// Топология: тест (10.1.0.1) — маршрутизатор (10.1.0.2 | 10.2.0.1) — цель (10.2.0.2).
// Нужны права root, запускается с RN_BPF_TEST=1 (make test-bpf).
func TestPingAndTrace(t *testing.T) {
	if os.Getenv("RN_BPF_TEST") == "" {
		t.Skip("нужен RN_BPF_TEST=1 и права root")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	host, _ := netns.Get()
	defer host.Close()
	defer func() { _ = netns.Set(host) }()

	nsR, err := netns.New()
	if err != nil {
		t.Fatal(err)
	}
	defer nsR.Close()
	nsT, _ := netns.New()
	defer nsT.Close()
	_ = netns.Set(host)

	pair := func(a, b string, bNS netns.NsHandle) {
		if err := netlink.LinkAdd(&netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: a}, PeerName: b}); err != nil {
			t.Fatal(err)
		}
		l, _ := netlink.LinkByName(b)
		_ = netlink.LinkSetNsFd(l, int(bNS))
	}
	up := func(name, cidr string) {
		l, err := netlink.LinkByName(name)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := netlink.ParseAddr(cidr)
		_ = netlink.AddrAdd(l, a)
		_ = netlink.LinkSetUp(l)
	}
	route := func(dst, gw string) {
		_, n, _ := net.ParseCIDR(dst)
		if err := netlink.RouteAdd(&netlink.Route{Dst: n, Gw: net.ParseIP(gw)}); err != nil {
			t.Fatal(err)
		}
	}

	pair("rn-a0", "rn-r0", nsR)
	defer func() {
		if l, err := netlink.LinkByName("rn-a0"); err == nil {
			_ = netlink.LinkDel(l)
		}
	}()
	up("rn-a0", "10.1.0.1/24")
	route("10.2.0.0/24", "10.1.0.2")

	_ = netns.Set(nsR)
	pair("rn-r1", "rn-t0", nsT)
	up("rn-r0", "10.1.0.2/24")
	up("rn-r1", "10.2.0.1/24")
	if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	_ = netns.Set(nsT)
	up("rn-t0", "10.2.0.2/24")
	lo, _ := netlink.LinkByName("lo")
	_ = netlink.LinkSetUp(lo)
	route("0.0.0.0/0", "10.2.0.1")

	_ = netns.Set(host)
	ctx := context.Background()
	p, err := diagPing(ctx, "10.2.0.2", "", 2, nil)
	if err != nil || p.Received != 2 {
		t.Fatalf("ping: %v %+v", err, p)
	}
	tr, err := diagTrace(ctx, "10.2.0.2", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Reached || len(tr.Hops) != 2 || tr.Hops[0].Address != "10.1.0.2" || tr.Hops[1].Address != "10.2.0.2" {
		t.Fatalf("трассировка: %+v", tr)
	}
	if _, err := diagPing(ctx, "10.9.9.9", "rn-a0", 1, nil); err != nil {
		t.Fatalf("ping без ответа должен вернуть потерю, а не ошибку: %v", err)
	}
	routes, err := diagRoutes()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range routes {
		found = found || (r.Destination == "10.2.0.0/24" && r.Gateway == "10.1.0.2" && r.Interface == "rn-a0")
	}
	if !found {
		t.Fatalf("маршрута нет: %+v", routes)
	}
	nb, err := diagNeighbors()
	if err != nil || len(nb) == 0 {
		t.Fatalf("соседи: %v %+v", err, nb)
	}
}
