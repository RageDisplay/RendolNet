//go:build linux

package agent

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// announce рассылает gratuitous ARP. Без него роутер провайдера держит в кэше
// старый MAC для адреса, пока запись не истечёт, и ответы уходят мимо узла.
func announce(ifname string, ip net.IP) error {
	ip4 := ip.To4()
	if ip4 == nil {
		return nil
	}
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return err
	}
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_DGRAM, int(htons(unix.ETH_P_ARP)))
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()

	pkt := make([]byte, 28)
	binary.BigEndian.PutUint16(pkt[0:], 1) // ethernet
	binary.BigEndian.PutUint16(pkt[2:], unix.ETH_P_IP)
	pkt[4], pkt[5] = 6, 4
	binary.BigEndian.PutUint16(pkt[6:], 1) // request
	copy(pkt[8:], ifi.HardwareAddr)
	copy(pkt[14:], ip4)
	copy(pkt[24:], ip4)

	to := &unix.SockaddrLinklayer{Protocol: htons(unix.ETH_P_ARP), Ifindex: ifi.Index, Halen: 6}
	copy(to.Addr[:], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	for i := 0; i < 2; i++ {
		if err := unix.Sendto(fd, pkt, 0, to); err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

func htons(v uint16) uint16 { return v<<8 | v>>8 }

// Gateways пингует шлюзы маршрутов по умолчанию через их интерфейсы.
func (n *LinuxNetwork) Gateways() []Gateway {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{Table: unix.RT_TABLE_MAIN}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return nil
	}
	var out []Gateway
	for _, r := range routes {
		if r.Gw == nil {
			continue
		}
		if r.Dst != nil {
			if ones, _ := r.Dst.Mask.Size(); ones != 0 {
				continue
			}
		}
		l, err := netlink.LinkByIndex(r.LinkIndex)
		if err != nil || skipLink(l.Attrs().Name) {
			continue
		}
		g := Gateway{Interface: l.Attrs().Name, Address: r.Gw.String(), Checked: time.Now()}
		var rtt time.Duration
		for try := 0; try < 3; try++ {
			if rtt, err = ping(l.Attrs().Name, r.Gw, time.Second); err == nil {
				break
			}
		}
		if err != nil {
			g.Error = err.Error()
		} else {
			g.OK, g.RTTms = true, float64(rtt.Microseconds())/1000
		}
		out = append(out, g)
	}
	return out
}

var errTimeout = errors.New("нет ответа")

func ping(ifname string, dst net.IP, timeout time.Duration) (time.Duration, error) {
	p, err := newPinger(ifname)
	if err != nil {
		return 0, err
	}
	defer p.close()
	r, err := p.echo(dst, 1, 64, timeout)
	if err != nil {
		return 0, err
	}
	if !r.reached {
		return 0, fmt.Errorf("%s: %w", dst, errTimeout)
	}
	return r.rtt, nil
}

// pinger — сырой ICMP-сокет. Сырой сокет получает все ICMP узла, поэтому
// свои ответы отбираются по идентификатору и номеру.
type pinger struct {
	fd int
	id uint16
}

func newPinger(ifname string) (*pinger, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_ICMP)
	if err != nil {
		return nil, err
	}
	if ifname != "" {
		if err := unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ifname); err != nil {
			_ = unix.Close(fd)
			return nil, err
		}
	}
	return &pinger{fd: fd, id: uint16(rand.Uint32())}, nil
}

func (p *pinger) close() { _ = unix.Close(p.fd) }

type echoResult struct {
	from    net.IP // кто ответил: адресат или маршрутизатор по пути
	rtt     time.Duration
	reached bool // ответил сам адресат
}

// echo шлёт один запрос с заданным TTL. Ответом считается echo reply от
// адресата или time exceeded / unreachable, внутри которого наш запрос.
// Нет ответа за timeout — результат без from и без ошибки.
func (p *pinger) echo(dst net.IP, seq uint16, ttl int, timeout time.Duration) (echoResult, error) {
	if err := unix.SetsockoptInt(p.fd, unix.IPPROTO_IP, unix.IP_TTL, ttl); err != nil {
		return echoResult{}, err
	}
	req := []byte{8, 0, 0, 0, byte(p.id >> 8), byte(p.id), byte(seq >> 8), byte(seq), 'r', 'n'}
	binary.BigEndian.PutUint16(req[2:], icmpChecksum(req))
	var to unix.SockaddrInet4
	copy(to.Addr[:], dst.To4())
	start := time.Now()
	if err := unix.Sendto(p.fd, req, 0, &to); err != nil {
		return echoResult{}, err
	}
	mine := func(m []byte) bool {
		return len(m) >= 8 && m[0] == 8 && binary.BigEndian.Uint16(m[4:]) == p.id && binary.BigEndian.Uint16(m[6:]) == seq
	}
	buf := make([]byte, 1500)
	deadline := start.Add(timeout)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return echoResult{}, nil
		}
		tv := unix.NsecToTimeval(left.Nanoseconds())
		_ = unix.SetsockoptTimeval(p.fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
		n, from, err := unix.Recvfrom(p.fd, buf, 0)
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				continue
			}
			return echoResult{}, err
		}
		if n < 20 {
			continue
		}
		hl := int(buf[0]&0x0f) * 4
		if n < hl+8 {
			continue
		}
		m := buf[hl:n]
		sa, ok := from.(*unix.SockaddrInet4)
		if !ok {
			continue
		}
		src := net.IP(append([]byte(nil), sa.Addr[:]...))
		switch m[0] {
		case 0: // echo reply
			if src.Equal(dst.To4()) && binary.BigEndian.Uint16(m[4:]) == p.id && binary.BigEndian.Uint16(m[6:]) == seq {
				return echoResult{from: src, rtt: time.Since(start), reached: true}, nil
			}
		case 3, 11: // unreachable, time exceeded: внутри заголовок нашего запроса
			in := m[8:]
			if len(in) < 20 {
				continue
			}
			ihl := int(in[0]&0x0f) * 4
			if len(in) >= ihl+8 && net.IP(in[16:20]).Equal(dst.To4()) && mine(in[ihl:]) {
				return echoResult{from: src, rtt: time.Since(start), reached: m[0] == 3 && src.Equal(dst.To4())}, nil
			}
		}
	}
}

func icmpChecksum(b []byte) uint16 {
	var s uint32
	for i := 0; i+1 < len(b); i += 2 {
		s += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		s += uint32(b[len(b)-1]) << 8
	}
	for s>>16 != 0 {
		s = s&0xffff + s>>16
	}
	return ^uint16(s)
}
