package dnsfwd

import (
	"log/slog"
	"net"
	"net/netip"
	"testing"
	"time"
)

// fakeUpstream отвечает на любой запрос его же байтами с флагом ответа.
func fakeUpstream(t *testing.T) (udp *net.UDPConn, tcp net.Listener) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	tcp, err = net.Listen("tcp", udp.LocalAddr().String())
	if err != nil {
		udp.Close()
		t.Skip("порт занят:", err)
	}
	go func() {
		b := make([]byte, 512)
		for {
			n, peer, err := udp.ReadFrom(b)
			if err != nil {
				return
			}
			b[2] |= 0x80
			_, _ = udp.WriteTo(b[:n], peer)
		}
	}()
	go func() {
		for {
			c, err := tcp.Accept()
			if err != nil {
				return
			}
			q, _ := readMsg(c)
			q[2] |= 0x80
			_ = writeMsg(c, q)
			c.Close()
		}
	}()
	return udp, tcp
}

func TestForward(t *testing.T) {
	u, l := fakeUpstream(t)
	defer u.Close()
	defer l.Close()
	seen := make(chan int, 4)
	up := netip.MustParseAddrPort(u.LocalAddr().String())
	f, err := Start(slog.Default(), []netip.Addr{netip.MustParseAddr("127.0.0.1")}, 25353, []netip.AddrPort{up}, func(b []byte) { seen <- len(b) })
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	q := []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0}
	c, err := net.Dial("udp", "127.0.0.1:25353")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = c.Write(q)
	resp := make([]byte, 512)
	n, err := c.Read(resp)
	if err != nil || n != len(q) || resp[0] != 0x12 || resp[2]&0x80 == 0 {
		t.Fatalf("udp: %v %x", err, resp[:n])
	}
	if <-seen != len(q) {
		t.Fatal("наблюдатель не увидел ответ")
	}

	tc, err := net.Dial("tcp", "127.0.0.1:25353")
	if err != nil {
		t.Fatal(err)
	}
	_ = tc.SetDeadline(time.Now().Add(3 * time.Second))
	_ = writeMsg(tc, q)
	r, err := readMsg(tc)
	if err != nil || r[1] != 0x34 || r[2]&0x80 == 0 {
		t.Fatalf("tcp: %v %x", err, r)
	}
}
