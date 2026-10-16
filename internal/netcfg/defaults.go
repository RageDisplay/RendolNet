package netcfg

// Seed — то, что знает установщик: какие порты куда смотрят.
type Seed struct {
	Hostname string
	Mgmt     string
	WAN      string
	LAN      string
	LANAddr  string // 192.168.1.1/24
	WANDHCP  bool
	WANAddr  string
	Gateway  string
}

// Default собирает стартовый конфиг: LAN ходит в интернет через маскарад,
// снаружи на узел ничего не пускается, управление — только через mgmt.
func Default(s Seed) Config {
	c := Config{
		Schema: SchemaVersion,
		System: System{Hostname: s.Hostname, DNS: []string{"1.1.1.1", "8.8.8.8"}},
		Zones: []Zone{
			{Name: "wan", Description: "Внешняя сеть"},
			{Name: "lan", Description: "Внутренняя сеть"},
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
	if s.LAN != "" {
		lan := Interface{Name: s.LAN, Role: RoleLAN, Zone: "lan", Enabled: true}
		if s.LANAddr != "" {
			lan.Addresses = []string{s.LANAddr}
		}
		c.Interfaces = append(c.Interfaces, lan)
	}
	c.Rules = []Rule{
		{ID: "r-lan-out", Name: "LAN в интернет", Enabled: true, From: []string{"lan"}, To: []string{"wan"}, Action: ActionAllow},
		{ID: "r-lan-self", Name: "LAN к узлу: DNS и ping", Enabled: true, From: []string{"lan"}, To: []string{ZoneSelf},
			Services: []string{"dns", "dns-tcp", "icmp", "icmpv6"}, Action: ActionAllow},
		{ID: "r-ping-in", Name: "Ping снаружи", Enabled: false, From: []string{"wan"}, To: []string{ZoneSelf},
			Services: []string{"icmp"}, Action: ActionAllow},
	}
	return c
}
