package agent

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"rendolnet/internal/dataplane"
)

// Адрес домена живёт в наборе TTL записи (в пределах минуты и суток) плюс
// запас: клиент может держать ответ в своём кэше дольше, чем просил сервер.
const (
	fqdnMinTTL  = time.Minute
	fqdnMaxTTL  = 24 * time.Hour
	fqdnGrace   = 10 * time.Minute
	fqdnRefresh = 5 * time.Minute
)

type fqdn struct {
	mu      sync.Mutex
	learned map[string]map[netip.Addr]time.Time // домен → адрес → до какого времени
	sets    map[string][]uint32                 // домен → наборы, из плана
	static  map[uint32]map[netip.Prefix]bool    // что в наборе из самого конфига
}

func newFQDN() *fqdn {
	return &fqdn{learned: map[string]map[netip.Addr]time.Time{}, sets: map[string][]uint32{}, static: map[uint32]map[netip.Prefix]bool{}}
}

type FQDNAddr struct {
	Address string    `json:"address"`
	Until   time.Time `json:"until"`
}

type FQDNInfo struct {
	Domain    string     `json:"domain"`
	Addresses []FQDNAddr `json:"addresses"`
}

func matchDomain(name, domain string) bool {
	return name == domain || strings.HasSuffix(name, "."+domain)
}

func host(a netip.Addr) netip.Prefix { return netip.PrefixFrom(a, a.BitLen()) }

// prepare вызывается под f.mu перед dp.Apply: запоминает состав наборов из
// конфига и дописывает в план уже известные адреса, иначе применение их
// сотрёт.
func (f *fqdn) prepare(p *dataplane.Plan, now time.Time) {
	f.static = map[uint32]map[netip.Prefix]bool{}
	for id, ps := range p.Sets {
		m := map[netip.Prefix]bool{}
		for _, x := range ps {
			m[x] = true
		}
		f.static[id] = m
	}
	for d := range f.learned {
		if _, ok := p.DomainSets[d]; !ok {
			delete(f.learned, d) // домена больше нет ни в одном объекте
		}
	}
	for d, ids := range p.DomainSets {
		for a, until := range f.learned[d] {
			if now.After(until) {
				continue
			}
			for _, id := range ids {
				if !f.static[id][host(a)] {
					p.Sets[id] = append(p.Sets[id], host(a))
				}
			}
		}
	}
	f.sets = p.DomainSets
}

type setOp struct {
	set uint32
	pfx netip.Prefix
}

// learn запоминает адрес домена. Возвращает наборы, куда адрес надо
// добавить: пусто, если он там уже есть.
func (f *fqdn) learn(domain string, a netip.Addr, ttl time.Duration, now time.Time) []setOp {
	ttl = min(max(ttl, fqdnMinTTL), fqdnMaxTTL)
	until := now.Add(ttl + fqdnGrace)
	m := f.learned[domain]
	if m == nil {
		m = map[netip.Addr]time.Time{}
		f.learned[domain] = m
	}
	prev, had := m[a]
	if !had || until.After(prev) {
		m[a] = until
	}
	if had {
		return nil
	}
	var ops []setOp
	for _, id := range f.sets[domain] {
		ops = append(ops, setOp{id, host(a)})
	}
	return ops
}

// expire убирает просроченные адреса. Возвращает, что удалить из наборов:
// адрес остаётся, пока он нужен другому домену того же набора или есть в
// конфиге.
func (f *fqdn) expire(now time.Time) []setOp {
	var ops []setOp
	for d, m := range f.learned {
		for a, until := range m {
			if now.Before(until) {
				continue
			}
			delete(m, a)
			for _, id := range f.sets[d] {
				if !f.heldBy(id, a, now) {
					ops = append(ops, setOp{id, host(a)})
				}
			}
		}
	}
	return ops
}

func (f *fqdn) heldBy(set uint32, a netip.Addr, now time.Time) bool {
	if f.static[set][host(a)] {
		return true
	}
	for d, ids := range f.sets {
		if until, ok := f.learned[d][a]; ok && now.Before(until) && slices.Contains(ids, set) {
			return true
		}
	}
	return false
}

func (f *fqdn) snapshot() []FQDNInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []FQDNInfo{}
	for d := range f.sets {
		info := FQDNInfo{Domain: d, Addresses: []FQDNAddr{}}
		for a, until := range f.learned[d] {
			info.Addresses = append(info.Addresses, FQDNAddr{a.String(), until})
		}
		slices.SortFunc(info.Addresses, func(x, y FQDNAddr) int { return strings.Compare(x.Address, y.Address) })
		out = append(out, info)
	}
	slices.SortFunc(out, func(x, y FQDNInfo) int { return strings.Compare(x.Domain, y.Domain) })
	return out
}

func (f *fqdn) domains() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.sets))
	for d := range f.sets {
		out = append(out, d)
	}
	return out
}

type dnsAddr struct {
	a   netip.Addr
	ttl time.Duration
}

// parseDNS достаёт из ответа все имена (вопрос, владельцы записей, цели
// CNAME) и все адреса. Адреса считаются ответом на любое из этих имён:
// youtube.com → CNAME youtube-ui.l.google.com → A.
func parseDNS(b []byte) (names []string, addrs []dnsAddr) {
	var p dnsmessage.Parser
	h, err := p.Start(b)
	if err != nil || !h.Response || h.RCode != dnsmessage.RCodeSuccess {
		return nil, nil
	}
	norm := func(n dnsmessage.Name) string { return strings.ToLower(strings.TrimSuffix(n.String(), ".")) }
	qs, err := p.AllQuestions()
	if err != nil {
		return nil, nil
	}
	for _, q := range qs {
		names = append(names, norm(q.Name))
	}
	for {
		rh, err := p.AnswerHeader()
		if err != nil {
			break
		}
		names = append(names, norm(rh.Name))
		ttl := time.Duration(rh.TTL) * time.Second
		switch rh.Type {
		case dnsmessage.TypeA:
			r, err := p.AResource()
			if err != nil {
				return names, addrs
			}
			addrs = append(addrs, dnsAddr{netip.AddrFrom4(r.A), ttl})
		case dnsmessage.TypeAAAA:
			r, err := p.AAAAResource()
			if err != nil {
				return names, addrs
			}
			addrs = append(addrs, dnsAddr{netip.AddrFrom16(r.AAAA), ttl})
		case dnsmessage.TypeCNAME:
			r, err := p.CNAMEResource()
			if err != nil {
				return names, addrs
			}
			names = append(names, norm(r.CNAME))
		default:
			if err := p.SkipAnswer(); err != nil {
				return names, addrs
			}
		}
	}
	return names, addrs
}

// ObserveDNS — наблюдатель DNS-прокси. Вызывается до отдачи ответа клиенту.
func (a *Agent) ObserveDNS(resp []byte) {
	names, addrs := parseDNS(resp)
	if len(addrs) == 0 {
		return
	}
	a.fq.mu.Lock()
	defer a.fq.mu.Unlock()
	now := time.Now()
	for d := range a.fq.sets {
		if !slices.ContainsFunc(names, func(n string) bool { return matchDomain(n, d) }) {
			continue
		}
		for _, x := range addrs {
			a.applyOps(a.fq.learn(d, x.a, x.ttl, now), true)
		}
	}
}

func (a *Agent) applyOps(ops []setOp, add bool) {
	for _, op := range ops {
		if err := a.dp.SetAddr(op.set, op.pfx, add); err != nil {
			a.log.Warn("набор адресов домена", "err", err)
		}
	}
}

// fqdnLoop убирает просроченные адреса и сам разрешает домены объектов:
// клиент мог получить ответ раньше, чем домен появился в объекте, и
// больше не спрашивать.
func (a *Agent) fqdnLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	last := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.fqKick:
			last = time.Time{}
		case <-t.C:
		}
		a.fq.mu.Lock()
		a.applyOps(a.fq.expire(time.Now()), false)
		a.fq.mu.Unlock()
		if time.Since(last) >= fqdnRefresh {
			last = time.Now()
			a.resolveDomains(ctx)
		}
	}
}

func (a *Agent) resolveDomains(ctx context.Context) {
	servers := a.dnsServers()
	for _, d := range a.fq.domains() {
		for _, s := range servers {
			lctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			ips, err := resolver(s).LookupNetIP(lctx, "ip", d)
			cancel()
			if err != nil {
				continue
			}
			a.fq.mu.Lock()
			for _, ip := range ips {
				a.applyOps(a.fq.learn(d, ip.Unmap(), fqdnRefresh, time.Now()), true)
			}
			a.fq.mu.Unlock()
			break
		}
	}
}

// resolver ходит к DNS-серверам из конфига, а не к resolv.conf пода: так
// результат совпадает с тем, что увидят клиенты через DNS-прокси.
func resolver(server string) *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		d := net.Dialer{Timeout: 3 * time.Second}
		return d.DialContext(ctx, network, net.JoinHostPort(server, "53"))
	}}
}
