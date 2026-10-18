package dhcpd

import (
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

var t0 = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func srv() *Server {
	return New(Config{
		Iface: "lan0", Addr: netip.MustParsePrefix("192.168.1.1/24"),
		Start: netip.MustParseAddr("192.168.1.100"), End: netip.MustParseAddr("192.168.1.102"),
		Lease: time.Hour, DNS: []netip.Addr{netip.MustParseAddr("192.168.1.1")},
		Static: []Static{{MAC: "02:00:00:00:00:99", IP: netip.MustParseAddr("192.168.1.50"), Name: "printer"}},
	}, nil)
}

func msg(t *testing.T, mac string, typ dhcpv4.MessageType, mods ...dhcpv4.Modifier) *dhcpv4.DHCPv4 {
	t.Helper()
	hw, _ := net.ParseMAC(mac)
	m, err := dhcpv4.New(append([]dhcpv4.Modifier{dhcpv4.WithHwAddr(hw), dhcpv4.WithMessageType(typ)}, mods...)...)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func obtain(t *testing.T, s *Server, mac string, now time.Time) netip.Addr {
	t.Helper()
	off := s.Handle(msg(t, mac, dhcpv4.MessageTypeDiscover), now)
	if off == nil || off.MessageType() != dhcpv4.MessageTypeOffer {
		t.Fatalf("%s: нет предложения", mac)
	}
	ack := s.Handle(msg(t, mac, dhcpv4.MessageTypeRequest,
		dhcpv4.WithOption(dhcpv4.OptRequestedIPAddress(off.YourIPAddr)),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(net.IPv4(192, 168, 1, 1)))), now)
	if ack == nil || ack.MessageType() != dhcpv4.MessageTypeAck {
		t.Fatalf("%s: нет подтверждения: %v", mac, ack)
	}
	if !ack.YourIPAddr.Equal(off.YourIPAddr) || !ack.Router()[0].Equal(net.IPv4(192, 168, 1, 1)) || ack.IPAddressLeaseTime(0) != time.Hour {
		t.Fatalf("ответ: %s", ack.Summary())
	}
	a, _ := netip.AddrFromSlice(ack.YourIPAddr.To4())
	return a
}

func TestLeaseCycle(t *testing.T) {
	s := srv()
	a := obtain(t, s, "02:00:00:00:00:01", t0)
	b := obtain(t, s, "02:00:00:00:00:02", t0)
	if a.String() != "192.168.1.100" || b.String() != "192.168.1.101" {
		t.Fatalf("%s %s", a, b)
	}
	// тот же клиент снова получает свой адрес
	if again := obtain(t, s, "02:00:00:00:00:01", t0.Add(time.Minute)); again != a {
		t.Fatalf("повторно выдан %s", again)
	}
	obtain(t, s, "02:00:00:00:00:03", t0)
	if s.Handle(msg(t, "02:00:00:00:00:04", dhcpv4.MessageTypeDiscover), t0) != nil {
		t.Fatal("пул исчерпан, а предложение есть")
	}
	// после истечения аренды адрес уходит новому клиенту
	if c := obtain(t, s, "02:00:00:00:00:04", t0.Add(2*time.Hour)); !c.IsValid() {
		t.Fatal("истёкший адрес не переиспользован")
	}
	if n := len(s.Leases(t0.Add(2 * time.Hour))); n != 2 { // новая аренда + привязка
		t.Fatalf("аренд %d", n)
	}
}

func TestStaticAndNak(t *testing.T) {
	s := srv()
	if a := obtain(t, s, "02:00:00:00:00:99", t0); a.String() != "192.168.1.50" {
		t.Fatalf("привязка выдала %s", a)
	}
	// чужой адрес из другой сети после переезда клиента — NAK
	r := s.Handle(msg(t, "02:00:00:00:00:05", dhcpv4.MessageTypeRequest,
		dhcpv4.WithOption(dhcpv4.OptRequestedIPAddress(net.IPv4(10, 0, 0, 5)))), t0)
	if r == nil || r.MessageType() != dhcpv4.MessageTypeNak {
		t.Fatalf("ждали NAK: %v", r)
	}
	// адрес привязки другому клиенту не отдаётся
	r = s.Handle(msg(t, "02:00:00:00:00:05", dhcpv4.MessageTypeRequest,
		dhcpv4.WithOption(dhcpv4.OptRequestedIPAddress(net.IPv4(192, 168, 1, 50)))), t0)
	if r == nil || r.MessageType() != dhcpv4.MessageTypeNak {
		t.Fatalf("ждали NAK на чужую привязку: %v", r)
	}
}

func TestOtherServerChosen(t *testing.T) {
	s := srv()
	s.Handle(msg(t, "02:00:00:00:00:01", dhcpv4.MessageTypeDiscover), t0)
	r := s.Handle(msg(t, "02:00:00:00:00:01", dhcpv4.MessageTypeRequest,
		dhcpv4.WithOption(dhcpv4.OptRequestedIPAddress(net.IPv4(192, 168, 1, 100))),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(net.IPv4(192, 168, 1, 254)))), t0)
	if r != nil {
		t.Fatal("ответ на запрос к другому серверу")
	}
	if len(s.Snapshot()) != 0 {
		t.Fatal("предложение не снято")
	}
}

func TestRestoreLeases(t *testing.T) {
	s := srv()
	a := obtain(t, s, "02:00:00:00:00:07", t0)
	s2 := New(s.cfg, s.Snapshot())
	if b := obtain(t, s2, "02:00:00:00:00:07", t0.Add(3*time.Hour)); a != b {
		t.Fatalf("после перезапуска %s вместо %s", b, a)
	}
}
