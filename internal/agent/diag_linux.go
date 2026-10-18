//go:build linux

package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func resolve4(ctx context.Context, target string, servers []string) (net.IP, error) {
	if a, err := netip.ParseAddr(target); err == nil {
		if !a.Is4() {
			return nil, errors.New("пока поддерживается только IPv4")
		}
		return a.AsSlice(), nil
	}
	var last error
	for _, s := range servers {
		ips, err := resolver(s).LookupIP(ctx, "ip4", target)
		if err == nil && len(ips) > 0 {
			return ips[0], nil
		}
		last = err
	}
	return nil, fmt.Errorf("%s не разрешается: %w", target, last)
}

func diagPing(ctx context.Context, target, iface string, count int, servers []string) (*PingResult, error) {
	dst, err := resolve4(ctx, target, servers)
	if err != nil {
		return nil, err
	}
	p, err := newPinger(iface)
	if err != nil {
		return nil, err
	}
	defer p.close()
	res := &PingResult{Target: target, Address: dst.String(), Replies: []PingReply{}}
	for i := 1; i <= count && ctx.Err() == nil; i++ {
		start := time.Now()
		r, err := p.echo(dst, uint16(i), 64, 2*time.Second)
		if err != nil {
			return nil, err
		}
		if r.reached {
			res.Received++
			res.Replies = append(res.Replies, PingReply{Seq: i, RTTms: ms(r.rtt)})
		} else {
			res.Replies = append(res.Replies, PingReply{Seq: i, Lost: true})
		}
		if i < count {
			select {
			case <-ctx.Done():
			case <-time.After(time.Second - time.Since(start)):
			}
		}
	}
	return res, nil
}

func diagTrace(ctx context.Context, target string, maxHops int, servers []string) (*TraceResult, error) {
	dst, err := resolve4(ctx, target, servers)
	if err != nil {
		return nil, err
	}
	p, err := newPinger("")
	if err != nil {
		return nil, err
	}
	defer p.close()
	res := &TraceResult{Target: target, Address: dst.String(), Hops: []Hop{}}
	silent := 0
	for ttl := 1; ttl <= maxHops && ctx.Err() == nil; ttl++ {
		r, err := p.echo(dst, uint16(ttl), ttl, time.Second)
		if err != nil {
			return nil, err
		}
		h := Hop{TTL: ttl}
		if r.from != nil {
			h.Address, h.RTTms = r.from.String(), ms(r.rtt)
			silent = 0
		} else {
			silent++
		}
		res.Hops = append(res.Hops, h)
		if r.reached || (r.from != nil && r.from.Equal(dst)) {
			res.Reached = true
			break
		}
		// пять молчащих узлов подряд — дальше обычно тоже тишина
		if silent >= 5 {
			break
		}
	}
	return res, nil
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

var protoNames = map[netlink.RouteProtocol]string{
	unix.RTPROT_KERNEL: "kernel", unix.RTPROT_BOOT: "boot", unix.RTPROT_STATIC: "static",
	unix.RTPROT_DHCP: "dhcp", unix.RTPROT_RA: "ra",
	rtProtoStatic: "rendolnet", rtProtoDHCP: "rendolnet-dhcp",
}

func diagRoutes() ([]RouteEntry, error) {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_ALL, &netlink.Route{Table: unix.RT_TABLE_MAIN}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return nil, err
	}
	names := linkNames()
	out := []RouteEntry{}
	for _, r := range routes {
		if r.Family == unix.AF_INET6 && r.Dst != nil && r.Dst.IP.IsLinkLocalUnicast() {
			continue
		}
		e := RouteEntry{Interface: names[r.LinkIndex], Metric: r.Priority, Protocol: protoNames[r.Protocol]}
		if e.Protocol == "" {
			e.Protocol = strconv.Itoa(int(r.Protocol))
		}
		switch {
		case r.Dst != nil:
			e.Destination = r.Dst.String()
		case r.Family == unix.AF_INET6:
			e.Destination = "::/0"
		default:
			e.Destination = "0.0.0.0/0"
		}
		if r.Gw != nil {
			e.Gateway = r.Gw.String()
		}
		if r.Src != nil {
			e.Source = r.Src.String()
		}
		out = append(out, e)
	}
	return out, nil
}

var nudNames = map[int]string{
	netlink.NUD_REACHABLE: "reachable", netlink.NUD_STALE: "stale", netlink.NUD_DELAY: "delay",
	netlink.NUD_PROBE: "probe", netlink.NUD_FAILED: "failed", netlink.NUD_INCOMPLETE: "incomplete",
	netlink.NUD_PERMANENT: "permanent",
}

func diagNeighbors() ([]Neighbor, error) {
	names := linkNames()
	out := []Neighbor{}
	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		list, err := netlink.NeighList(0, fam)
		if err != nil {
			return nil, err
		}
		for _, n := range list {
			st, ok := nudNames[n.State]
			name := names[n.LinkIndex]
			if !ok || name == "" || skipLink(name) || n.IP.IsMulticast() {
				continue
			}
			nb := Neighbor{Address: n.IP.String(), Interface: name, State: st}
			if len(n.HardwareAddr) > 0 {
				nb.MAC = n.HardwareAddr.String()
			}
			out = append(out, nb)
		}
	}
	slices.SortFunc(out, func(a, b Neighbor) int {
		x, _ := netip.ParseAddr(a.Address)
		y, _ := netip.ParseAddr(b.Address)
		return x.Compare(y)
	})
	return out, nil
}

func linkNames() map[int]string {
	m := map[int]string{}
	links, _ := netlink.LinkList()
	for _, l := range links {
		m[l.Attrs().Index] = l.Attrs().Name
	}
	return m
}

func diagLookup(ctx context.Context, name, server string) (*LookupResult, error) {
	start := time.Now()
	addrs, err := resolver(server).LookupHost(ctx, name)
	if err != nil {
		return nil, err
	}
	return &LookupResult{Name: name, Server: server, Addresses: addrs, RTTms: ms(time.Since(start))}, nil
}
