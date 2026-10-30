//go:build linux

package agent

import (
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/google/nftables"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"

	"rendolnet/internal/dnsfwd"
	"rendolnet/internal/netcfg"
)

// Клиент (10.3.0.2) спрашивает 198.51.100.53, а отвечает DNS-прокси узла
// (10.3.0.1): таблица rendol_dns заворачивает запрос, conntrack подменяет
// адрес в ответе. Нужны права root, запускается с RN_BPF_TEST=1.
func TestDNSRedirect(t *testing.T) {
	if os.Getenv("RN_BPF_TEST") == "" {
		t.Skip("нужен RN_BPF_TEST=1 и права root")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	host, _ := netns.Get()
	defer host.Close()
	defer func() { _ = netns.Set(host) }()

	// узел — отдельное пространство, чтобы правила nftables не задели тестовый хост
	node, err := netns.New()
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	cli, _ := netns.New()
	defer cli.Close()

	_ = netns.Set(node)
	lo, _ := netlink.LinkByName("lo")
	_ = netlink.LinkSetUp(lo)
	if err := netlink.LinkAdd(&netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: "rn-dns0"}, PeerName: "rn-dns1"}); err != nil {
		t.Fatal(err)
	}
	peer, _ := netlink.LinkByName("rn-dns1")
	_ = netlink.LinkSetNsFd(peer, int(cli))
	l, _ := netlink.LinkByName("rn-dns0")
	a, _ := netlink.ParseAddr("10.3.0.1/24")
	_ = netlink.AddrAdd(l, a)
	_ = netlink.LinkSetUp(l)

	// Вышестоящий сервер отвечает своим же запросом с флагом ответа. Он в
	// исходном пространстве: прокси ходит к нему из своих горутин, а их потоки
	// в пространство узла не переключались.
	_ = netns.Set(host)
	up, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	_ = netns.Set(node)
	if err != nil {
		t.Fatal(err)
	}
	defer up.Close()
	go func() {
		b := make([]byte, 512)
		for {
			n, p, err := up.ReadFrom(b)
			if err != nil {
				return
			}
			b[2] |= 0x80
			_, _ = up.WriteTo(b[:n], p)
		}
	}()
	_ = netns.Set(host)
	upTCP, err := net.Listen("tcp", up.LocalAddr().String())
	_ = netns.Set(node)
	if err != nil {
		t.Fatal(err)
	}
	defer upTCP.Close()
	go func() {
		for {
			conn, err := upTCP.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				var size [2]byte
				if _, err := io.ReadFull(conn, size[:]); err != nil {
					return
				}
				b := make([]byte, binary.BigEndian.Uint16(size[:]))
				if _, err := io.ReadFull(conn, b); err != nil {
					return
				}
				b[2] |= 0x80
				_, _ = conn.Write(size[:])
				_, _ = conn.Write(b)
			}(conn)
		}
	}()
	f, err := dnsfwd.Start(slog.Default(), []netip.Addr{netip.MustParseAddr("10.3.0.1")}, 53,
		[]netip.AddrPort{netip.MustParseAddrPort(up.LocalAddr().String())}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	c := netcfg.Config{
		Interfaces: []netcfg.Interface{{Name: "rn-dns0", Role: netcfg.RoleLAN, Enabled: true, Addresses: []string{"10.3.0.1/24"}}},
		DNSProxy:   netcfg.DNSProxy{Enabled: true, Intercept: true},
	}
	if err := syncDNSRedirect(&c, nftables.WithNetNSFd(int(node))); err != nil {
		t.Fatal(err)
	}

	_ = netns.Set(cli)
	cl, _ := netlink.LinkByName("rn-dns1")
	ca, _ := netlink.ParseAddr("10.3.0.2/24")
	_ = netlink.AddrAdd(cl, ca)
	_ = netlink.LinkSetUp(cl)
	_, def, _ := net.ParseCIDR("0.0.0.0/0")
	_ = netlink.RouteAdd(&netlink.Route{Dst: def, Gw: net.ParseIP("10.3.0.1")})
	conn, err := net.Dial("udp", "198.51.100.53:53")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	q := []byte{0xab, 0xcd, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0}
	_, _ = conn.Write(q)
	resp := make([]byte, 512)
	n, err := conn.Read(resp)
	if err != nil || n != len(q) || resp[0] != 0xab || resp[2]&0x80 == 0 {
		t.Fatalf("ответ через перехват UDP: %v %x", err, resp[:n])
	}

	tcp, err := net.DialTimeout("tcp", "198.51.100.53:53", 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	_ = tcp.SetDeadline(time.Now().Add(3 * time.Second))
	msg := make([]byte, 2+len(q))
	binary.BigEndian.PutUint16(msg, uint16(len(q)))
	copy(msg[2:], q)
	if _, err := tcp.Write(msg); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(tcp, resp[:2]); err != nil {
		t.Fatal(err)
	}
	n = int(binary.BigEndian.Uint16(resp[:2]))
	if _, err := io.ReadFull(tcp, resp[:n]); err != nil {
		t.Fatal(err)
	}
	if n != len(q) || resp[0] != 0xab || resp[2]&0x80 == 0 {
		t.Fatalf("ответ через перехват TCP: %x", resp[:n])
	}

	// выключение убирает таблицу
	_ = netns.Set(node)
	c.DNSProxy.Intercept = false
	if err := syncDNSRedirect(&c, nftables.WithNetNSFd(int(node))); err != nil {
		t.Fatal(err)
	}
	_ = netns.Set(cli)
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	_, _ = conn.Write(q)
	if _, err := conn.Read(resp); err == nil {
		t.Fatal("после выключения перехвата ответ всё ещё приходит")
	}
}
