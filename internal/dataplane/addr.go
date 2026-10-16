package dataplane

import (
	"encoding/binary"
	"net/netip"

	bpf "rendolnet/internal/dataplane/ebpf"
)

// В картах адрес — 16 байт в сетевом порядке, IPv4 как ::ffff:a.b.c.d.
// Поля uint32 читаются машиной как little-endian, поэтому байты кладём
// через NativeEndian, не трогая порядок внутри.
func toAddr(a netip.Addr) bpf.RendolAddr {
	b := a.As16()
	var r bpf.RendolAddr
	for i := 0; i < 4; i++ {
		r.W[i] = binary.NativeEndian.Uint32(b[i*4:])
	}
	return r
}

func fromAddr(r bpf.RendolAddr) netip.Addr {
	var b [16]byte
	for i := 0; i < 4; i++ {
		binary.NativeEndian.PutUint32(b[i*4:], r.W[i])
	}
	return netip.AddrFrom16(b).Unmap()
}

// be32 — IPv4 в том же виде, в каком его видит программа в iphdr.
func be32(a netip.Addr) uint32 {
	b := a.As4()
	return binary.NativeEndian.Uint32(b[:])
}

func fromBE32(v uint32) netip.Addr {
	var b [4]byte
	binary.NativeEndian.PutUint32(b[:], v)
	return netip.AddrFrom4(b)
}

func htons(v uint16) uint16 {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	return binary.NativeEndian.Uint16(b[:])
}

func ntohs(v uint16) uint16 {
	var b [2]byte
	binary.NativeEndian.PutUint16(b[:], v)
	return binary.BigEndian.Uint16(b[:])
}

// prefixBits — длина префикса в 128-битном пространстве.
func prefixBits(p netip.Prefix) uint32 {
	if p.Addr().Is4() {
		return uint32(96 + p.Bits())
	}
	return uint32(p.Bits())
}
