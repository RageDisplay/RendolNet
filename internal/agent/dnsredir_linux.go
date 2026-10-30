//go:build linux

package agent

import (
	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"

	"rendolnet/internal/netcfg"
)

const dnsTable = "rendol_dns"

// syncDNSRedirect держит таблицу перехвата DNS:
//
//	iifname "lan0" meta l4proto udp th dport 53 fib daddr type != local redirect to :53
//
// redirect отправляет запрос на основной адрес входящего интерфейса — там и
// слушает DNS-прокси. Обратную подмену адреса в ответе делает conntrack.
// eBPF такие запросы пропускает мимо политики (IF_F_DNS_REDIRECT).
// opts — для тестов: сокет netlink библиотека открывает в своём потоке, и
// пространство имён сети ей надо назвать явно.
func syncDNSRedirect(c *netcfg.Config, opts ...nftables.ConnOption) error {
	var ifaces []string
	for i := range c.Interfaces {
		if netcfg.DNSIntercepted(c, &c.Interfaces[i]) {
			ifaces = append(ifaces, c.Interfaces[i].Name)
		}
	}
	conn, err := nftables.New(opts...)
	if err != nil {
		return err
	}
	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyIPv4)
	if err != nil {
		return err
	}
	for _, t := range tables {
		if t.Name == dnsTable {
			conn.DelTable(t)
		}
	}
	if len(ifaces) == 0 {
		return conn.Flush()
	}
	t := conn.AddTable(&nftables.Table{Family: nftables.TableFamilyIPv4, Name: dnsTable})
	ch := conn.AddChain(&nftables.Chain{
		Name: "pre", Table: t, Type: nftables.ChainTypeNAT,
		Hooknum: nftables.ChainHookPrerouting, Priority: nftables.ChainPriorityNATDest,
	})
	for _, name := range ifaces {
		for _, proto := range []byte{unix.IPPROTO_UDP, unix.IPPROTO_TCP} {
			conn.AddRule(&nftables.Rule{Table: t, Chain: ch, Exprs: []expr.Any{
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ifname(name)},
				&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.BigEndian.PutUint16(53)},
				&expr.Fib{Register: 1, FlagDADDR: true, ResultADDRTYPE: true},
				&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: binaryutil.NativeEndian.PutUint32(unix.RTN_LOCAL)},
				&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(53)},
				&expr.Redir{RegisterProtoMin: 1},
			}})
		}
	}
	return conn.Flush()
}

func ifname(n string) []byte {
	b := make([]byte, unix.IFNAMSIZ)
	copy(b, n)
	return b
}
