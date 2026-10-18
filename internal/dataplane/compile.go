// Package dataplane переводит конфиг в содержимое eBPF-карт и следит за тем,
// чтобы ядро видело либо старую политику целиком, либо новую.
package dataplane

import (
	"fmt"
	"net/netip"
	"sort"

	"rendolnet/internal/netcfg"
)

const (
	MaxRules     = 4096
	MaxSNATRules = 64
	MaxMgmtPorts = 8

	ZoneSelf uint16 = 0

	RoleWAN  uint8 = 1
	RoleLAN  uint8 = 2
	RoleMgmt uint8 = 3

	ActAllow  uint8 = 1
	ActDeny   uint8 = 2
	ActReject uint8 = 3

	RuleFlagLog uint8 = 1

	RuleIDDNAT    uint32 = 0xfffffffe
	RuleIDMgmt    uint32 = 0xfffffffd
	RuleIDDefault uint32 = 0xffffffff
)

type CompiledRule struct {
	SrcSet uint32
	DstSet uint32
	Slot   uint32
	PortLo uint16
	PortHi uint16
	Proto  uint8
	Action uint8
	Flags  uint8
}

type Span struct {
	Start, Count uint32
}

type PairKey struct {
	From, To uint16
}

type IfacePlan struct {
	Zone uint16
	Role uint8
}

type SNATPlan struct {
	Interface string
	SrcSet    uint32
	ToAddr    netip.Addr // невалидный — адрес интерфейса
	Slot      uint32
}

type DNATPlan struct {
	Interface string
	Proto     uint8
	Port      uint16
	ToAddr    netip.Addr
	ToPort    uint16
	Allow     bool
}

type MgmtPlan struct {
	Interface string
	Set       uint32
	Ports     []uint16
}

// Plan — результат компиляции, ещё не привязанный к индексам интерфейсов
// и банку карт: это делает загрузчик.
type Plan struct {
	Zones  map[string]uint16
	Ifaces map[string]IfacePlan
	Rules  []CompiledRule
	Pairs  map[PairKey]Span
	Sets   map[uint32][]netip.Prefix
	// DomainSets — в какие наборы агент добавляет адреса, узнанные из DNS
	// для домена.
	DomainSets map[string][]uint32
	SNAT       []SNATPlan
	DNAT       []DNATPlan
	Mgmt       MgmtPlan
	Blocklist  []netip.Prefix
	SYNRate    uint32
	SYNBurst   uint32
	LogDefault bool
	// RuleSlots — номер счётчика для каждого правила. Номера стабильны между
	// компиляциями, чтобы статистика не прыгала с правила на правило.
	RuleSlots map[string]uint32
}

var protoNum = map[netcfg.Proto]uint8{
	netcfg.ProtoAny: 0, netcfg.ProtoTCP: 6, netcfg.ProtoUDP: 17,
	netcfg.ProtoICMP: 1, netcfg.ProtoICMPv6: 58,
}

type compiler struct {
	plan   *Plan
	res    *netcfg.Resolver
	setIDs map[string]uint32
}

// Compile собирает план. prevSlots — номера счётчиков из прошлой компиляции.
func Compile(c *netcfg.Config, prevSlots map[string]uint32) (*Plan, error) {
	if err := netcfg.Validate(c); err != nil {
		return nil, err
	}
	p := &Plan{
		Zones:      map[string]uint16{netcfg.ZoneSelf: ZoneSelf},
		Ifaces:     map[string]IfacePlan{},
		Pairs:      map[PairKey]Span{},
		Sets:       map[uint32][]netip.Prefix{},
		DomainSets: map[string][]uint32{},
		RuleSlots:  map[string]uint32{},
		SYNRate:    uint32(c.Protection.SYNRate),
		SYNBurst:   uint32(c.Protection.SYNBurst),
		LogDefault: c.Protection.LogDefaultDeny,
	}
	cp := &compiler{plan: p, res: netcfg.NewResolver(c), setIDs: map[string]uint32{}}

	for i, z := range c.Zones {
		p.Zones[z.Name] = uint16(i + 1)
	}
	roles := map[netcfg.Role]uint8{netcfg.RoleWAN: RoleWAN, netcfg.RoleLAN: RoleLAN, netcfg.RoleMgmt: RoleMgmt}
	for _, it := range c.Interfaces {
		if !it.Enabled {
			continue
		}
		p.Ifaces[it.Name] = IfacePlan{Zone: p.Zones[it.Zone], Role: roles[it.Role]}
	}

	cp.assignSlots(c.Rules, prevSlots)

	perPair := map[PairKey][]CompiledRule{}
	var pairOrder []PairKey
	for _, r := range c.Rules {
		if !r.Enabled {
			continue
		}
		entries, err := cp.rule(r)
		if err != nil {
			return nil, fmt.Errorf("правило %q: %w", r.Name, err)
		}
		for _, from := range cp.zones(r.From, false) {
			for _, to := range cp.zones(r.To, true) {
				k := PairKey{from, to}
				if _, ok := perPair[k]; !ok {
					pairOrder = append(pairOrder, k)
				}
				perPair[k] = append(perPair[k], entries...)
			}
		}
	}
	sort.Slice(pairOrder, func(i, j int) bool {
		if pairOrder[i].From != pairOrder[j].From {
			return pairOrder[i].From < pairOrder[j].From
		}
		return pairOrder[i].To < pairOrder[j].To
	})
	for _, k := range pairOrder {
		list := perPair[k]
		p.Pairs[k] = Span{Start: uint32(len(p.Rules)), Count: uint32(len(list))}
		p.Rules = append(p.Rules, list...)
	}
	if len(p.Rules) > MaxRules {
		return nil, fmt.Errorf("после раскрытия зон и сервисов получилось %d записей, предел %d", len(p.Rules), MaxRules)
	}

	for _, s := range c.NAT.Source {
		if !s.Enabled {
			continue
		}
		set, err := cp.set(s.Source)
		if err != nil {
			return nil, err
		}
		sp := SNATPlan{Interface: s.Interface, SrcSet: set}
		if s.Translate != "" {
			sp.ToAddr = netip.MustParseAddr(s.Translate)
		}
		p.SNAT = append(p.SNAT, sp)
	}
	if len(p.SNAT) > MaxSNATRules {
		return nil, fmt.Errorf("правил SNAT больше %d", MaxSNATRules)
	}

	for _, d := range c.NAT.Destination {
		if !d.Enabled {
			continue
		}
		p.DNAT = append(p.DNAT, DNATPlan{
			Interface: d.Interface, Proto: protoNum[d.Proto], Port: uint16(d.Port),
			ToAddr: netip.MustParseAddr(d.ToAddress), ToPort: uint16(d.ToPort), Allow: d.AllowPolicy,
		})
	}

	if c.Management.Interface != "" {
		set, err := cp.set(c.Management.AllowFrom)
		if err != nil {
			return nil, err
		}
		p.Mgmt = MgmtPlan{Interface: c.Management.Interface, Set: set}
		for _, port := range c.Management.Ports {
			if len(p.Mgmt.Ports) == MaxMgmtPorts {
				return nil, fmt.Errorf("портов управления больше %d", MaxMgmtPorts)
			}
			p.Mgmt.Ports = append(p.Mgmt.Ports, uint16(port))
		}
	}

	for _, b := range c.Protection.Blocklist {
		pfx, _ := netcfg.ParseAddrOrPrefix(b)
		p.Blocklist = append(p.Blocklist, pfx)
	}
	return p, nil
}

func (cp *compiler) assignSlots(rules []netcfg.Rule, prev map[string]uint32) {
	used := map[uint32]bool{}
	for _, r := range rules {
		if s, ok := prev[r.ID]; ok && s < MaxRules && !used[s] {
			cp.plan.RuleSlots[r.ID] = s
			used[s] = true
		}
	}
	next := uint32(0)
	for _, r := range rules {
		if _, ok := cp.plan.RuleSlots[r.ID]; ok {
			continue
		}
		for used[next] {
			next++
		}
		cp.plan.RuleSlots[r.ID] = next
		used[next] = true
	}
}

// zones раскрывает список зон. "any" в источнике не включает self: трафик от
// самого узла политикой не проверяется.
func (cp *compiler) zones(names []string, withSelf bool) []uint16 {
	seen := map[uint16]bool{}
	var out []uint16
	add := func(id uint16) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, n := range names {
		if n == "any" {
			for name, id := range cp.plan.Zones {
				if name == netcfg.ZoneSelf && !withSelf {
					continue
				}
				add(id)
			}
			continue
		}
		if id, ok := cp.plan.Zones[n]; ok {
			add(id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// set возвращает номер набора адресов; 0 — любой адрес. Одинаковые списки
// делят один набор.
func (cp *compiler) set(refs []string) (uint32, error) {
	pfx, err := cp.res.Addresses(refs)
	if err != nil {
		return 0, err
	}
	if pfx == nil {
		return 0, nil
	}
	doms := cp.res.Domains(refs)
	key := fmt.Sprint(pfx, doms)
	if id, ok := cp.setIDs[key]; ok {
		return id, nil
	}
	id := uint32(len(cp.setIDs) + 1)
	cp.setIDs[key] = id
	cp.plan.Sets[id] = pfx
	for _, d := range doms {
		cp.plan.DomainSets[d] = append(cp.plan.DomainSets[d], id)
	}
	return id, nil
}

func (cp *compiler) rule(r netcfg.Rule) ([]CompiledRule, error) {
	src, err := cp.set(r.Source)
	if err != nil {
		return nil, err
	}
	dst, err := cp.set(r.Destination)
	if err != nil {
		return nil, err
	}
	svcs, err := cp.res.Services(r.Services)
	if err != nil {
		return nil, err
	}
	base := CompiledRule{SrcSet: src, DstSet: dst, Slot: cp.plan.RuleSlots[r.ID]}
	switch r.Action {
	case netcfg.ActionAllow:
		base.Action = ActAllow
	case netcfg.ActionDeny:
		base.Action = ActDeny
	case netcfg.ActionReject:
		base.Action = ActReject
	}
	if r.Log {
		base.Flags |= RuleFlagLog
	}
	if svcs == nil {
		return []CompiledRule{base}, nil
	}
	var out []CompiledRule
	for _, s := range svcs {
		e := base
		e.Proto = protoNum[s.Proto]
		if len(s.Ports) == 0 {
			out = append(out, e)
			continue
		}
		for _, pr := range s.Ports {
			e.PortLo, e.PortHi = pr.Lo, pr.Hi
			out = append(out, e)
		}
	}
	return out, nil
}
