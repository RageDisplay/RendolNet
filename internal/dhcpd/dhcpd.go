// Package dhcpd — DHCPv4-сервер для сетей LAN. Логика аренды отделена от
// сокетов: Handle принимает запрос и возвращает ответ.
package dhcpd

import (
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

type Static struct {
	MAC  string
	IP   netip.Addr
	Name string
}

type Config struct {
	Iface  string
	Addr   netip.Prefix // адрес узла с маской, он же шлюз и server id
	Start  netip.Addr
	End    netip.Addr
	Lease  time.Duration
	DNS    []netip.Addr
	Domain string
	Static []Static
}

type Lease struct {
	Iface    string    `json:"interface"`
	MAC      string    `json:"mac"`
	IP       string    `json:"ip"`
	Hostname string    `json:"hostname,omitempty"`
	Expires  time.Time `json:"expires"`
	Static   bool      `json:"static,omitempty"`
}

// Пока клиент не подтвердил предложение, адрес держится за ним недолго.
const offerHold = time.Minute

type lease struct {
	ip       netip.Addr
	hostname string
	expires  time.Time
}

type Server struct {
	cfg Config

	mu     sync.Mutex
	leases map[string]*lease // по MAC
	// адреса, от которых клиент отказался (DHCPDECLINE): кто-то занял их сам
	declined map[netip.Addr]time.Time
}

func New(cfg Config, prev []Lease) *Server {
	s := &Server{cfg: cfg, leases: map[string]*lease{}, declined: map[netip.Addr]time.Time{}}
	for _, l := range prev {
		ip, err := netip.ParseAddr(l.IP)
		if err != nil || l.Static || !s.inRange(ip) {
			continue
		}
		s.leases[l.MAC] = &lease{ip: ip, hostname: l.Hostname, expires: l.Expires}
	}
	return s
}

func (s *Server) inRange(ip netip.Addr) bool {
	return ip.Is4() && !ip.Less(s.cfg.Start) && !s.cfg.End.Less(ip) && ip != s.cfg.Addr.Addr()
}

func (s *Server) static(mac string) (Static, bool) {
	for _, st := range s.cfg.Static {
		if st.MAC == mac {
			return st, true
		}
	}
	return Static{}, false
}

func (s *Server) staticIP(ip netip.Addr) bool {
	return slices.ContainsFunc(s.cfg.Static, func(st Static) bool { return st.IP == ip })
}

// free: адрес можно отдать mac. Чужие истёкшие аренды не мешают.
func (s *Server) free(mac string, ip netip.Addr, now time.Time) bool {
	if !s.inRange(ip) || s.staticIP(ip) {
		return false
	}
	if until, ok := s.declined[ip]; ok && now.Before(until) {
		return false
	}
	for m, l := range s.leases {
		if m != mac && l.ip == ip && now.Before(l.expires) {
			return false
		}
	}
	return true
}

func (s *Server) pick(mac string, want netip.Addr, now time.Time) (netip.Addr, bool) {
	if st, ok := s.static(mac); ok {
		return st.IP, true
	}
	if l := s.leases[mac]; l != nil && s.free(mac, l.ip, now) {
		return l.ip, true
	}
	if want.IsValid() && s.free(mac, want, now) {
		return want, true
	}
	// сначала адреса, которые ещё никому не выдавались, потом истёкшие
	used := map[netip.Addr]bool{}
	for _, l := range s.leases {
		used[l.ip] = true
	}
	var expired netip.Addr
	for ip := s.cfg.Start; ip.IsValid() && !s.cfg.End.Less(ip); ip = ip.Next() {
		if !s.free(mac, ip, now) {
			continue
		}
		if !used[ip] {
			return ip, true
		}
		if !expired.IsValid() {
			expired = ip
		}
	}
	return expired, expired.IsValid()
}

func addrOf(ip net.IP) netip.Addr {
	a, ok := netip.AddrFromSlice(ip.To4())
	if !ok || a.IsUnspecified() {
		return netip.Addr{}
	}
	return a
}

// Handle разбирает запрос клиента. nil — отвечать не нужно.
func (s *Server) Handle(req *dhcpv4.DHCPv4, now time.Time) *dhcpv4.DHCPv4 {
	if req.OpCode != dhcpv4.OpcodeBootRequest || len(req.ClientHWAddr) != 6 {
		return nil
	}
	mac := req.ClientHWAddr.String()
	self := s.cfg.Addr.Addr()
	if sid := addrOf(req.ServerIdentifier()); sid.IsValid() && sid != self {
		// клиент выбрал другой сервер: наше предложение больше не нужно
		s.mu.Lock()
		if l := s.leases[mac]; l != nil && l.expires.Sub(now) <= offerHold {
			delete(s.leases, mac)
		}
		s.mu.Unlock()
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch req.MessageType() {
	case dhcpv4.MessageTypeDiscover:
		ip, ok := s.pick(mac, addrOf(req.RequestedIPAddress()), now)
		if !ok {
			return nil
		}
		if _, st := s.static(mac); !st {
			l := s.leases[mac]
			if l == nil || l.ip != ip || l.expires.Before(now.Add(offerHold)) {
				s.leases[mac] = &lease{ip: ip, hostname: req.HostName(), expires: now.Add(offerHold)}
			}
		}
		return s.reply(req, dhcpv4.MessageTypeOffer, ip)

	case dhcpv4.MessageTypeRequest:
		ip := addrOf(req.RequestedIPAddress())
		if !ip.IsValid() {
			ip = addrOf(req.ClientIPAddr) // продление
		}
		if !ip.IsValid() {
			return nil
		}
		if st, ok := s.static(mac); ok {
			if st.IP != ip {
				return s.nak(req)
			}
			return s.reply(req, dhcpv4.MessageTypeAck, ip)
		}
		if !s.cfg.Addr.Masked().Contains(ip) {
			return s.nak(req)
		}
		if !s.free(mac, ip, now) {
			return s.nak(req)
		}
		s.leases[mac] = &lease{ip: ip, hostname: req.HostName(), expires: now.Add(s.cfg.Lease)}
		return s.reply(req, dhcpv4.MessageTypeAck, ip)

	case dhcpv4.MessageTypeRelease:
		if l := s.leases[mac]; l != nil && l.ip == addrOf(req.ClientIPAddr) {
			l.expires = now
		}
	case dhcpv4.MessageTypeDecline:
		if ip := addrOf(req.RequestedIPAddress()); ip.IsValid() {
			s.declined[ip] = now.Add(s.cfg.Lease)
			delete(s.leases, mac)
		}
	case dhcpv4.MessageTypeInform:
		return s.reply(req, dhcpv4.MessageTypeAck, netip.Addr{})
	}
	return nil
}

func (s *Server) reply(req *dhcpv4.DHCPv4, t dhcpv4.MessageType, ip netip.Addr) *dhcpv4.DHCPv4 {
	self := net.IP(s.cfg.Addr.Addr().AsSlice())
	mods := []dhcpv4.Modifier{
		dhcpv4.WithMessageType(t),
		dhcpv4.WithServerIP(self),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(self)),
		dhcpv4.WithOption(dhcpv4.OptSubnetMask(net.CIDRMask(s.cfg.Addr.Bits(), 32))),
		dhcpv4.WithOption(dhcpv4.OptRouter(self)),
	}
	if ip.IsValid() {
		mods = append(mods,
			dhcpv4.WithYourIP(net.IP(ip.AsSlice())),
			dhcpv4.WithOption(dhcpv4.OptIPAddressLeaseTime(s.cfg.Lease)),
		)
	}
	if len(s.cfg.DNS) > 0 {
		dns := make([]net.IP, len(s.cfg.DNS))
		for i, d := range s.cfg.DNS {
			dns[i] = d.AsSlice()
		}
		mods = append(mods, dhcpv4.WithOption(dhcpv4.OptDNS(dns...)))
	}
	if s.cfg.Domain != "" {
		mods = append(mods, dhcpv4.WithOption(dhcpv4.OptDomainName(s.cfg.Domain)))
	}
	resp, err := dhcpv4.NewReplyFromRequest(req, mods...)
	if err != nil {
		return nil
	}
	return resp
}

func (s *Server) nak(req *dhcpv4.DHCPv4) *dhcpv4.DHCPv4 {
	self := net.IP(s.cfg.Addr.Addr().AsSlice())
	resp, err := dhcpv4.NewReplyFromRequest(req,
		dhcpv4.WithMessageType(dhcpv4.MessageTypeNak),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(self)),
	)
	if err != nil {
		return nil
	}
	return resp
}

// Leases — действующие аренды и привязки, отсортированные по адресу.
func (s *Server) Leases(now time.Time) []Lease {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Lease
	for mac, l := range s.leases {
		if now.Before(l.expires) {
			out = append(out, Lease{Iface: s.cfg.Iface, MAC: mac, IP: l.ip.String(), Hostname: l.hostname, Expires: l.expires})
		}
	}
	for _, st := range s.cfg.Static {
		out = append(out, Lease{Iface: s.cfg.Iface, MAC: st.MAC, IP: st.IP.String(), Hostname: st.Name, Static: true})
	}
	slices.SortFunc(out, func(a, b Lease) int {
		x, _ := netip.ParseAddr(a.IP)
		y, _ := netip.ParseAddr(b.IP)
		return x.Compare(y)
	})
	return out
}

// Snapshot — все записи вместе с истёкшими, для сохранения на диск:
// клиент, вернувшийся после выключения, получит прежний адрес.
func (s *Server) Snapshot() []Lease {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Lease, 0, len(s.leases))
	for mac, l := range s.leases {
		out = append(out, Lease{Iface: s.cfg.Iface, MAC: mac, IP: l.ip.String(), Hostname: l.hostname, Expires: l.expires})
	}
	return out
}
