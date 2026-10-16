package netcfg

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

type FieldError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type ValidationError struct {
	Errors []FieldError `json:"errors"`
}

func (e *ValidationError) Error() string {
	if len(e.Errors) == 0 {
		return "netcfg: ошибок нет"
	}
	parts := make([]string, 0, len(e.Errors))
	for _, fe := range e.Errors {
		parts = append(parts, fe.Path+": "+fe.Message)
	}
	return "netcfg: " + strings.Join(parts, "; ")
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

type validator struct {
	errs []FieldError
}

func (v *validator) add(path, format string, args ...any) {
	v.errs = append(v.errs, FieldError{Path: path, Message: fmt.Sprintf(format, args...)})
}

// Validate проверяет конфиг целиком и возвращает *ValidationError со всеми
// найденными ошибками сразу: исправлять по одной через UI мучительно.
func Validate(c *Config) error {
	v := &validator{}

	if c.Schema != SchemaVersion {
		v.add("schema", "ожидается версия схемы %d, получена %d", SchemaVersion, c.Schema)
	}
	if c.System.Hostname != "" && !nameRe.MatchString(c.System.Hostname) {
		v.add("system.hostname", "недопустимое имя узла")
	}
	for i, d := range c.System.DNS {
		if _, err := netip.ParseAddr(d); err != nil {
			v.add(fmt.Sprintf("system.dns[%d]", i), "не IP-адрес: %q", d)
		}
	}

	zones := map[string]bool{ZoneSelf: true}
	for i, z := range c.Zones {
		p := fmt.Sprintf("zones[%d]", i)
		switch {
		case !nameRe.MatchString(z.Name):
			v.add(p+".name", "недопустимое имя зоны %q", z.Name)
		case z.Name == ZoneSelf || z.Name == "any":
			v.add(p+".name", "имя %q зарезервировано", z.Name)
		case zones[z.Name]:
			v.add(p+".name", "зона %q уже есть", z.Name)
		}
		zones[z.Name] = true
	}

	ifaces := map[string]*Interface{}
	mgmtCount := 0
	for i := range c.Interfaces {
		it := &c.Interfaces[i]
		p := fmt.Sprintf("interfaces[%d]", i)
		if !ifNameOK(it.Name) {
			v.add(p+".name", "недопустимое имя интерфейса %q", it.Name)
		} else if ifaces[it.Name] != nil {
			v.add(p+".name", "интерфейс %q описан дважды", it.Name)
		}
		ifaces[it.Name] = it
		switch it.Role {
		case RoleWAN, RoleLAN:
		case RoleMgmt:
			mgmtCount++
		default:
			v.add(p+".role", "роль должна быть wan, lan или mgmt")
		}
		if it.Zone == "" || !zones[it.Zone] || it.Zone == ZoneSelf {
			v.add(p+".zone", "неизвестная зона %q", it.Zone)
		}
		if it.DHCP && len(it.Addresses) > 0 {
			v.add(p+".addresses", "при DHCP статические адреса не задаются")
		}
		for j, a := range it.Addresses {
			if _, err := netip.ParsePrefix(a); err != nil {
				v.add(fmt.Sprintf("%s.addresses[%d]", p, j), "ожидается адрес с маской, например 192.0.2.1/24")
			}
		}
		if it.MTU != 0 && (it.MTU < 576 || it.MTU > 9216) {
			v.add(p+".mtu", "MTU вне диапазона 576–9216")
		}
		if it.VLAN != nil {
			if it.VLAN.ID < 1 || it.VLAN.ID > 4094 {
				v.add(p+".vlan.id", "VLAN ID вне диапазона 1–4094")
			}
			if it.VLAN.Parent == "" {
				v.add(p+".vlan.parent", "не указан родительский интерфейс")
			}
		}
	}
	for i, it := range c.Interfaces {
		if it.VLAN != nil && it.VLAN.Parent != "" {
			if parent := ifaces[it.VLAN.Parent]; parent != nil && parent.VLAN != nil {
				v.add(fmt.Sprintf("interfaces[%d].vlan.parent", i), "вложенные VLAN не поддерживаются")
			}
		}
	}
	if mgmtCount > 1 {
		v.add("interfaces", "интерфейс управления может быть только один")
	}

	for i, r := range c.Routes {
		p := fmt.Sprintf("routes[%d]", i)
		dst, err := netip.ParsePrefix(r.Destination)
		if err != nil {
			v.add(p+".destination", "ожидается сеть с маской")
		}
		if r.Gateway == "" && r.Interface == "" {
			v.add(p, "нужен шлюз или интерфейс")
		}
		if r.Gateway != "" {
			gw, err := netip.ParseAddr(r.Gateway)
			if err != nil {
				v.add(p+".gateway", "не IP-адрес")
			} else if dst.IsValid() && gw.Is4() != dst.Addr().Is4() {
				v.add(p+".gateway", "семейство шлюза не совпадает с сетью назначения")
			}
		}
		if r.Interface != "" && ifaces[r.Interface] == nil {
			v.add(p+".interface", "неизвестный интерфейс %q", r.Interface)
		}
		if r.Metric < 0 {
			v.add(p+".metric", "метрика не может быть отрицательной")
		}
	}

	names := map[string]string{}
	claim := func(path, name, kind string) {
		if !nameRe.MatchString(name) {
			v.add(path, "недопустимое имя %q", name)
			return
		}
		if prev, ok := names[name]; ok {
			v.add(path, "имя %q уже занято (%s)", name, prev)
			return
		}
		names[name] = kind
	}
	for i, a := range c.Objects.Addresses {
		p := fmt.Sprintf("objects.addresses[%d]", i)
		claim(p+".name", a.Name, "адрес")
		if len(a.Prefixes) == 0 {
			v.add(p+".prefixes", "пустой объект")
		}
		for j, s := range a.Prefixes {
			if _, err := ParseAddrOrPrefix(s); err != nil {
				v.add(fmt.Sprintf("%s.prefixes[%d]", p, j), "%v", err)
			}
		}
	}
	for i, g := range c.Objects.Groups {
		claim(fmt.Sprintf("objects.groups[%d].name", i), g.Name, "группа")
	}
	for i, s := range c.Objects.Services {
		p := fmt.Sprintf("objects.services[%d]", i)
		claim(p+".name", s.Name, "сервис")
		if _, err := ParseService(s.Proto, s.Ports); err != nil {
			v.add(p, "%v", err)
		}
	}
	res := NewResolver(c)
	for i, g := range c.Objects.Groups {
		if _, err := res.Addresses(g.Members); err != nil {
			v.add(fmt.Sprintf("objects.groups[%d].members", i), "%v", err)
		}
	}

	ruleIDs := map[string]bool{}
	for i, r := range c.Rules {
		p := fmt.Sprintf("rules[%d]", i)
		if r.ID == "" || ruleIDs[r.ID] {
			v.add(p+".id", "пустой или повторяющийся идентификатор")
		}
		ruleIDs[r.ID] = true
		if strings.TrimSpace(r.Name) == "" {
			v.add(p+".name", "нужно имя правила")
		}
		for _, list := range []struct {
			field string
			zs    []string
		}{{"from", r.From}, {"to", r.To}} {
			if len(list.zs) == 0 {
				v.add(p+"."+list.field, "укажите зону или any")
			}
			for _, z := range list.zs {
				if z != "any" && !zones[z] {
					v.add(p+"."+list.field, "неизвестная зона %q", z)
				}
			}
		}
		if _, err := res.Addresses(r.Source); err != nil {
			v.add(p+".source", "%v", err)
		}
		if _, err := res.Addresses(r.Destination); err != nil {
			v.add(p+".destination", "%v", err)
		}
		if _, err := res.Services(r.Services); err != nil {
			v.add(p+".services", "%v", err)
		}
		switch r.Action {
		case ActionAllow, ActionDeny, ActionReject:
		default:
			v.add(p+".action", "действие должно быть allow, deny или reject")
		}
	}

	natIDs := map[string]bool{}
	for i, s := range c.NAT.Source {
		p := fmt.Sprintf("nat.source[%d]", i)
		if s.ID == "" || natIDs[s.ID] {
			v.add(p+".id", "пустой или повторяющийся идентификатор")
		}
		natIDs[s.ID] = true
		if ifaces[s.Interface] == nil {
			v.add(p+".interface", "неизвестный интерфейс %q", s.Interface)
		}
		if _, err := res.Addresses(s.Source); err != nil {
			v.add(p+".source", "%v", err)
		}
		if s.Translate != "" {
			if a, err := netip.ParseAddr(s.Translate); err != nil || !a.Is4() {
				v.add(p+".translate", "ожидается IPv4-адрес")
			}
		}
	}
	type dnatKey struct {
		iface string
		proto Proto
		port  int
	}
	dnatSeen := map[dnatKey]bool{}
	for i, d := range c.NAT.Destination {
		p := fmt.Sprintf("nat.destination[%d]", i)
		if d.ID == "" || natIDs[d.ID] {
			v.add(p+".id", "пустой или повторяющийся идентификатор")
		}
		natIDs[d.ID] = true
		if ifaces[d.Interface] == nil {
			v.add(p+".interface", "неизвестный интерфейс %q", d.Interface)
		}
		if d.Proto != ProtoTCP && d.Proto != ProtoUDP {
			v.add(p+".proto", "проброс возможен только для tcp и udp")
		}
		if d.Port < 1 || d.Port > 65535 {
			v.add(p+".port", "порт вне диапазона")
		}
		if d.ToPort < 0 || d.ToPort > 65535 {
			v.add(p+".to_port", "порт вне диапазона")
		}
		if a, err := netip.ParseAddr(d.ToAddress); err != nil || !a.Is4() {
			v.add(p+".to_address", "ожидается IPv4-адрес")
		}
		k := dnatKey{d.Interface, d.Proto, d.Port}
		if d.Enabled && dnatSeen[k] {
			v.add(p, "порт %s/%d на %s уже пробрасывается", d.Proto, d.Port, d.Interface)
		}
		if d.Enabled {
			dnatSeen[k] = true
		}
	}

	for i, b := range c.Protection.Blocklist {
		if _, err := ParseAddrOrPrefix(b); err != nil {
			v.add(fmt.Sprintf("protection.blocklist[%d]", i), "%v", err)
		}
	}
	if c.Protection.SYNRate < 0 || c.Protection.SYNBurst < 0 {
		v.add("protection", "значения не могут быть отрицательными")
	}

	if c.Management.Interface != "" {
		it := ifaces[c.Management.Interface]
		if it == nil {
			v.add("management.interface", "неизвестный интерфейс %q", c.Management.Interface)
		} else if it.Role != RoleMgmt {
			v.add("management.interface", "у интерфейса управления должна быть роль mgmt")
		}
	}
	if _, err := res.Addresses(c.Management.AllowFrom); err != nil {
		v.add("management.allow_from", "%v", err)
	}
	for i, port := range c.Management.Ports {
		if port < 1 || port > 65535 {
			v.add(fmt.Sprintf("management.ports[%d]", i), "порт вне диапазона")
		}
	}

	if c.Zapret.Enabled {
		if c.Zapret.Strategy == "" {
			v.add("zapret.strategy", "выберите стратегию")
		}
		if it := ifaces[c.Zapret.Interface]; it == nil || it.Role != RoleWAN {
			v.add("zapret.interface", "нужен интерфейс с ролью wan")
		}
	}
	switch c.Zapret.Fooling {
	case "", "badseq", "md5sig":
	default:
		v.add("zapret.fooling", "допустимо: badseq, md5sig или пусто")
	}
	for i, ip := range c.Zapret.IPs {
		if _, err := ParseAddrOrPrefix(ip); err != nil {
			v.add(fmt.Sprintf("zapret.ips[%d]", i), "%v", err)
		}
	}

	if len(v.errs) > 0 {
		return &ValidationError{Errors: v.errs}
	}
	return nil
}

// ValidateCommit добавляет к Validate проверки, которые нужны при применении,
// но не должны мешать агенту поднять уже сохранённый конфиг.
func ValidateCommit(c *Config) error {
	if err := Validate(c); err != nil {
		return err
	}
	v := &validator{}
	for i, it := range c.Interfaces {
		if it.Enabled && it.Role != RoleMgmt && !it.DHCP && len(it.Addresses) == 0 {
			v.add(fmt.Sprintf("interfaces[%d].addresses", i), "нет ни адреса, ни DHCP: интерфейс останется без адреса")
		}
	}
	if len(v.errs) > 0 {
		return &ValidationError{Errors: v.errs}
	}
	return nil
}

// Linux ограничивает имя интерфейса 15 символами.
func ifNameOK(s string) bool {
	if s == "" || len(s) > 15 {
		return false
	}
	for _, r := range s {
		if r == '/' || r == ' ' || r == ':' || r > 0x7e {
			return false
		}
	}
	return true
}

// ParseAddrOrPrefix принимает и одиночный адрес, и сеть.
func ParseAddrOrPrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("неверная сеть %q", s)
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("неверный адрес %q", s)
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

type PortRange struct {
	Lo, Hi uint16
}

type ServiceSpec struct {
	Proto Proto
	Ports []PortRange // пусто — любой порт
}

func ParseService(proto Proto, ports string) (ServiceSpec, error) {
	spec := ServiceSpec{Proto: proto}
	switch proto {
	case ProtoTCP, ProtoUDP:
	case ProtoICMP, ProtoICMPv6, ProtoAny:
		if strings.TrimSpace(ports) != "" {
			return spec, fmt.Errorf("порты имеют смысл только для tcp и udp")
		}
		return spec, nil
	default:
		return spec, fmt.Errorf("неизвестный протокол %q", proto)
	}
	for _, part := range strings.Split(ports, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		l, err := parsePort(lo)
		if err != nil {
			return spec, err
		}
		h := l
		if isRange {
			if h, err = parsePort(hi); err != nil {
				return spec, err
			}
			if h < l {
				return spec, fmt.Errorf("диапазон %q перевёрнут", part)
			}
		}
		spec.Ports = append(spec.Ports, PortRange{l, h})
	}
	return spec, nil
}

func parsePort(s string) (uint16, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("неверный порт %q", s)
	}
	return uint16(n), nil
}
