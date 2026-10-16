package dataplane

import (
	"encoding/binary"
	"net/netip"
)

// Минимальный сборщик кадров для тестов: Ethernet + IPv4/IPv6 + TCP/UDP/ICMP.

type testPkt struct {
	src, dst     netip.Addr
	sport, dport uint16
	proto        uint8
	flags        uint8 // TCP
	icmpType     uint8
	icmpID       uint16
	payload      []byte
}

const (
	tcpFIN = 0x01
	tcpSYN = 0x02
	tcpRST = 0x04
	tcpACK = 0x10
)

func csum(data []byte, initial uint32) uint16 {
	s := initial
	for i := 0; i+1 < len(data); i += 2 {
		s += uint32(binary.BigEndian.Uint16(data[i:]))
	}
	if len(data)%2 == 1 {
		s += uint32(data[len(data)-1]) << 8
	}
	for s > 0xffff {
		s = (s & 0xffff) + (s >> 16)
	}
	return ^uint16(s)
}

func pseudoSum(src, dst netip.Addr, proto uint8, length int) uint32 {
	var s uint32
	add := func(b []byte) {
		for i := 0; i+1 < len(b); i += 2 {
			s += uint32(binary.BigEndian.Uint16(b[i:]))
		}
	}
	if src.Is4() {
		a, b := src.As4(), dst.As4()
		add(a[:])
		add(b[:])
	} else {
		a, b := src.As16(), dst.As16()
		add(a[:])
		add(b[:])
	}
	s += uint32(proto) + uint32(length)
	return s
}

func (p testPkt) l4() []byte {
	var l4 []byte
	switch p.proto {
	case 6:
		l4 = make([]byte, 20+len(p.payload))
		binary.BigEndian.PutUint16(l4[0:], p.sport)
		binary.BigEndian.PutUint16(l4[2:], p.dport)
		binary.BigEndian.PutUint32(l4[4:], 1000)
		if p.flags&tcpACK != 0 {
			binary.BigEndian.PutUint32(l4[8:], 5000)
		}
		l4[12] = 5 << 4
		l4[13] = p.flags
		binary.BigEndian.PutUint16(l4[14:], 65535)
		copy(l4[20:], p.payload)
		binary.BigEndian.PutUint16(l4[16:], csum(l4, pseudoSum(p.src, p.dst, 6, len(l4))))
	case 17:
		l4 = make([]byte, 8+len(p.payload))
		binary.BigEndian.PutUint16(l4[0:], p.sport)
		binary.BigEndian.PutUint16(l4[2:], p.dport)
		binary.BigEndian.PutUint16(l4[4:], uint16(len(l4)))
		copy(l4[8:], p.payload)
		binary.BigEndian.PutUint16(l4[6:], csum(l4, pseudoSum(p.src, p.dst, 17, len(l4))))
	case 1, 58:
		l4 = make([]byte, 8+len(p.payload))
		l4[0] = p.icmpType
		binary.BigEndian.PutUint16(l4[4:], p.icmpID)
		binary.BigEndian.PutUint16(l4[6:], 1)
		copy(l4[8:], p.payload)
		if p.proto == 1 {
			binary.BigEndian.PutUint16(l4[2:], csum(l4, 0))
		} else {
			binary.BigEndian.PutUint16(l4[2:], csum(l4, pseudoSum(p.src, p.dst, 58, len(l4))))
		}
	}
	return l4
}

func (p testPkt) frame() []byte {
	l4 := p.l4()
	eth := make([]byte, 14)
	copy(eth[0:6], []byte{0x02, 0, 0, 0, 0, 0x01})
	copy(eth[6:12], []byte{0x02, 0, 0, 0, 0, 0x02})
	if p.src.Is4() {
		binary.BigEndian.PutUint16(eth[12:], 0x0800)
		ip := make([]byte, 20)
		ip[0] = 0x45
		binary.BigEndian.PutUint16(ip[2:], uint16(20+len(l4)))
		ip[8] = 64
		ip[9] = p.proto
		a, b := p.src.As4(), p.dst.As4()
		copy(ip[12:], a[:])
		copy(ip[16:], b[:])
		binary.BigEndian.PutUint16(ip[10:], csum(ip, 0))
		return append(append(eth, ip...), l4...)
	}
	binary.BigEndian.PutUint16(eth[12:], 0x86dd)
	ip := make([]byte, 40)
	ip[0] = 0x60
	binary.BigEndian.PutUint16(ip[4:], uint16(len(l4)))
	ip[6] = p.proto
	ip[7] = 64
	a, b := p.src.As16(), p.dst.As16()
	copy(ip[8:], a[:])
	copy(ip[24:], b[:])
	return append(append(eth, ip...), l4...)
}

type parsed struct {
	src, dst     netip.Addr
	sport, dport uint16
	proto        uint8
	flags        uint8
	icmpID       uint16
	ipOK, l4OK   bool
	ack          uint32
}

func parseFrame(f []byte) parsed {
	var r parsed
	if binary.BigEndian.Uint16(f[12:]) == 0x0800 {
		ip := f[14:34]
		r.ipOK = csum(ip, 0) == 0
		r.src = netip.AddrFrom4([4]byte(ip[12:16]))
		r.dst = netip.AddrFrom4([4]byte(ip[16:20]))
		r.proto = ip[9]
		tot := int(binary.BigEndian.Uint16(ip[2:]))
		l4 := f[34 : 14+tot]
		r.parseL4(l4)
		switch r.proto {
		case 1:
			r.l4OK = csum(l4, 0) == 0
		default:
			r.l4OK = csum(l4, pseudoSum(r.src, r.dst, r.proto, len(l4))) == 0
		}
		return r
	}
	ip := f[14:54]
	r.ipOK = true
	r.src = netip.AddrFrom16([16]byte(ip[8:24]))
	r.dst = netip.AddrFrom16([16]byte(ip[24:40]))
	r.proto = ip[6]
	plen := int(binary.BigEndian.Uint16(ip[4:]))
	l4 := f[54 : 54+plen]
	r.parseL4(l4)
	r.l4OK = csum(l4, pseudoSum(r.src, r.dst, r.proto, len(l4))) == 0
	return r
}

func (r *parsed) parseL4(l4 []byte) {
	switch r.proto {
	case 6, 17:
		r.sport = binary.BigEndian.Uint16(l4[0:])
		r.dport = binary.BigEndian.Uint16(l4[2:])
		if r.proto == 6 {
			r.flags = l4[13]
			r.ack = binary.BigEndian.Uint32(l4[8:])
		}
	case 1, 58:
		r.icmpID = binary.BigEndian.Uint16(l4[4:])
	}
}
