//go:build linux

package dhcpd

import (
	"context"
	"net"
	"net/netip"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// Настоящий обмен через veth: сервер на rn-srv, клиент в отдельном netns.
// Нужны права root, запускается с RN_BPF_TEST=1 (make test-bpf).
func TestServeOverVeth(t *testing.T) {
	if os.Getenv("RN_BPF_TEST") == "" {
		t.Skip("нужен RN_BPF_TEST=1 и права root")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	host, _ := netns.Get()
	defer host.Close()
	cli, err := netns.New()
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	_ = netns.Set(host)

	veth := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: "rn-srv"}, PeerName: "rn-cli"}
	if err := netlink.LinkAdd(veth); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = netlink.LinkDel(veth) }()
	srvLink, _ := netlink.LinkByName("rn-srv")
	cliLink, _ := netlink.LinkByName("rn-cli")
	_ = netlink.LinkSetNsFd(cliLink, int(cli))
	addr, _ := netlink.ParseAddr("192.168.77.1/24")
	_ = netlink.AddrAdd(srvLink, addr)
	_ = netlink.LinkSetUp(srvLink)

	s := New(Config{
		Iface: "rn-srv", Addr: netip.MustParsePrefix("192.168.77.1/24"),
		Start: netip.MustParseAddr("192.168.77.100"), End: netip.MustParseAddr("192.168.77.110"),
		Lease: time.Hour, DNS: []netip.Addr{netip.MustParseAddr("192.168.77.1")},
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	acked := make(chan struct{}, 4)
	go func() { _ = s.Serve(ctx, func() { acked <- struct{}{} }) }()
	time.Sleep(200 * time.Millisecond)

	_ = netns.Set(cli)
	l, _ := netlink.LinkByName("rn-cli")
	_ = netlink.LinkSetUp(l)
	c, err := nclient4.New("rn-cli", nclient4.WithTimeout(2*time.Second), nclient4.WithRetry(3))
	if err != nil {
		_ = netns.Set(host)
		t.Fatal(err)
	}
	lease, err := c.Request(context.Background())
	_ = c.Close()
	_ = netns.Set(host)
	if err != nil {
		t.Fatal(err)
	}
	if !lease.ACK.YourIPAddr.Equal(net.IPv4(192, 168, 77, 100)) || !lease.ACK.Router()[0].Equal(net.IPv4(192, 168, 77, 1)) {
		t.Fatalf("аренда: %s", lease.ACK.Summary())
	}
	select {
	case <-acked:
	case <-time.After(time.Second):
		t.Fatal("changed не вызван")
	}
	if ls := s.Leases(time.Now()); len(ls) != 1 || ls[0].IP != "192.168.77.100" {
		t.Fatalf("аренды: %+v", ls)
	}
}
