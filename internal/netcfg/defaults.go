package netcfg

import (
	"fmt"
	"net/netip"
)

// Seed — то, что знает установщик: какие порты куда смотрят.
type Seed struct {
	Hostname string
	Mgmt     string
	WAN      string
	LANs     []SeedLAN
	WANDHCP  bool
	WANAddr  string
	Gateway  string
}

type SeedLAN struct {
	Name string
	Addr string // 192.168.1.1/24
}

// LANZone: первая LAN — зона lan, следующие — lan2, lan3…
func LANZone(i int) string {
	if i == 0 {
		return "lan"
	}
	return fmt.Sprintf("lan%d", i+1)
}

// DefaultDHCPRange: .100–.199 в той /24, где стоит адрес узла (или .10–.99,
// если узел сам попадает в .100–.199). В сетях мельче /24 диапазон не
// угадывается, сервер там включают вручную.
func DefaultDHCPRange(cidr string) (string, string, bool) {
	p, err := netip.ParsePrefix(cidr)
	if err != nil || !p.Addr().Is4() || p.Bits() > 24 {
		return "", "", false
	}
	b := p.Addr().As4()
	lo, hi := byte(100), byte(199)
	if b[3] >= lo && b[3] <= hi {
		lo, hi = 10, 99
	}
	b[3] = lo
	start := netip.AddrFrom4(b).String()
	b[3] = hi
	return start, netip.AddrFrom4(b).String(), true
}

// Default собирает стартовый конфиг: LAN ходит в интернет через маскарад,
// снаружи на узел ничего не пускается, управление — только через mgmt.
func Default(s Seed) Config {
	c := Config{
		Schema: SchemaVersion,
		System: System{Hostname: s.Hostname, DNS: []string{"1.1.1.1", "8.8.8.8"}},
		Zones: []Zone{
			{Name: "wan", Description: "Внешняя сеть"},
			{Name: "mgmt", Description: "Сеть управления"},
		},
		Objects: Objects{
			Addresses: []AddressObject{},
			Groups:    []AddressGroup{},
			Services: []Service{
				{Name: "dns", Proto: ProtoUDP, Ports: "53"},
				{Name: "dns-tcp", Proto: ProtoTCP, Ports: "53"},
				{Name: "web", Proto: ProtoTCP, Ports: "80,443"},
				{Name: "ssh", Proto: ProtoTCP, Ports: "22"},
			},
		},
		Management: Management{Interface: s.Mgmt, Ports: []int{22, 443}},
		Protection: Protection{LogDefaultDeny: true},
		DNSProxy:   DNSProxy{Enabled: true},
		NAT:        NAT{Source: []SNATRule{}, Destination: []DNATRule{}},
		Routes:     []Route{},
	}
	if s.Mgmt != "" {
		c.Interfaces = append(c.Interfaces, Interface{Name: s.Mgmt, Role: RoleMgmt, Zone: "mgmt", Enabled: true})
	}
	if s.WAN != "" {
		wan := Interface{Name: s.WAN, Role: RoleWAN, Zone: "wan", Enabled: true, DHCP: s.WANDHCP}
		if !s.WANDHCP && s.WANAddr != "" {
			wan.Addresses = []string{s.WANAddr}
		}
		c.Interfaces = append(c.Interfaces, wan)
		c.NAT.Source = append(c.NAT.Source, SNATRule{
			ID: "snat-lan", Name: "LAN в интернет", Enabled: true, Interface: s.WAN,
		})
		if s.Gateway != "" {
			c.Routes = append(c.Routes, Route{Destination: "0.0.0.0/0", Gateway: s.Gateway, Interface: s.WAN})
		}
	}
	// Зоны LAN есть всегда, хотя бы одна: правила ниже на неё ссылаются, а
	// порт можно назначить позже. Между собой LAN по умолчанию не связаны.
	lanZones := []string{"lan"}
	c.Zones = append(c.Zones, Zone{Name: "lan", Description: "Внутренняя сеть"})
	for i, l := range s.LANs {
		z := LANZone(i)
		if i > 0 {
			lanZones = append(lanZones, z)
			c.Zones = append(c.Zones, Zone{Name: z, Description: fmt.Sprintf("Внутренняя сеть %d", i+1)})
		}
		lan := Interface{Name: l.Name, Role: RoleLAN, Zone: z, Enabled: true}
		if l.Addr != "" {
			lan.Addresses = []string{l.Addr}
			if start, end, ok := DefaultDHCPRange(l.Addr); ok {
				lan.DHCPServer = &DHCPServer{Enabled: true, RangeStart: start, RangeEnd: end}
			}
		}
		c.Interfaces = append(c.Interfaces, lan)
	}
	c.Rules = []Rule{
		{ID: "r-lan-out", Name: "LAN в интернет", Enabled: true, From: lanZones, To: []string{"wan"}, Action: ActionAllow},
		{ID: "r-lan-self", Name: "LAN к узлу: DNS и ping", Enabled: true, From: lanZones, To: []string{ZoneSelf},
			Services: []string{"dns", "dns-tcp", "icmp", "icmpv6"}, Action: ActionAllow},
		{ID: "r-ping-in", Name: "Ping снаружи", Enabled: false, From: []string{"wan"}, To: []string{ZoneSelf},
			Services: []string{"icmp"}, Action: ActionAllow},
	}
	return c
}
