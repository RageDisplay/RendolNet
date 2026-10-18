package installer

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

type Addr struct {
	DHCP    bool     `yaml:"dhcp"`
	Address string   `yaml:"address"` // CIDR
	Gateway string   `yaml:"gateway"`
	DNS     []string `yaml:"dns"`
}

// Answers — всё, что спрашивает установщик или читает из autoinstall.yaml.
type Answers struct {
	Language string `yaml:"language"`
	Disk     string `yaml:"disk"`
	Confirm  string `yaml:"confirm"`
	Hostname string `yaml:"hostname"`
	Timezone string `yaml:"timezone"`

	Mgmt     string `yaml:"mgmt"`
	MgmtAddr Addr   `yaml:"mgmt_addr"`
	WAN      string `yaml:"wan"`
	WANAddr  Addr   `yaml:"wan_addr"`
	LANs     []LAN  `yaml:"lans"`
	// lan и lan_addr — короткая запись для одной LAN, Validate переносит её в LANs
	LAN     string `yaml:"lan"`
	LANAddr string `yaml:"lan_addr"`

	SSHKey string `yaml:"ssh_key"`
	Reboot bool   `yaml:"reboot"`
}

type LAN struct {
	Iface string `yaml:"iface"`
	Addr  string `yaml:"addr"` // CIDR, по умолчанию 192.168.N.1/24
}

// DefaultLANAddr: 192.168.1.1/24 для первой LAN, 192.168.2.1/24 для второй…
func DefaultLANAddr(i int) string { return fmt.Sprintf("192.168.%d.1/24", i+1) }

var (
	hostnameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	ifaceRe    = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`)
	tzRe       = regexp.MustCompile(`^[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+){0,2}$`)
	sshKeyRe   = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)) [A-Za-z0-9+/]{40,}={0,3}( [ -~]{0,100})?$`)
)

func (a *Answers) Validate() error {
	a.Hostname = strings.ToLower(strings.TrimSpace(a.Hostname))
	if a.Language != "ru" && a.Language != "en" {
		a.Language = "ru"
	}
	if !hostnameRe.MatchString(a.Hostname) {
		return errors.New("недопустимое имя узла")
	}
	if a.Timezone == "" {
		a.Timezone = "UTC"
	}
	if !tzRe.MatchString(a.Timezone) || strings.Contains(a.Timezone, "..") {
		return errors.New("недопустимый часовой пояс")
	}
	if a.Mgmt == "" {
		return errors.New("нужен интерфейс управления")
	}
	if a.LAN != "" {
		a.LANs = append([]LAN{{Iface: a.LAN, Addr: a.LANAddr}}, a.LANs...)
		a.LAN, a.LANAddr = "", ""
	}
	type port struct{ role, name string }
	ports := []port{{"управления", a.Mgmt}, {"WAN", a.WAN}}
	for i, l := range a.LANs {
		ports = append(ports, port{fmt.Sprintf("LAN %d", i+1), l.Iface})
	}
	seen := map[string]string{}
	for _, p := range ports {
		if p.name == "" {
			if p.role != "WAN" {
				return fmt.Errorf("не указан интерфейс %s", p.role)
			}
			continue
		}
		if !ifaceRe.MatchString(p.name) {
			return fmt.Errorf("недопустимое имя интерфейса %q", p.name)
		}
		if other, ok := seen[p.name]; ok {
			return fmt.Errorf("%s назначен и интерфейсом %s, и %s", p.name, other, p.role)
		}
		seen[p.name] = p.role
	}
	if err := a.MgmtAddr.validate("управления", true); err != nil {
		return err
	}
	if a.WAN != "" {
		if err := a.WANAddr.validate("WAN", true); err != nil {
			return err
		}
	}
	var nets []netip.Prefix
	for i := range a.LANs {
		l := &a.LANs[i]
		if l.Addr == "" {
			l.Addr = DefaultLANAddr(i)
		}
		p, err := netip.ParsePrefix(l.Addr)
		if err != nil || !p.Addr().Is4() {
			return fmt.Errorf("адрес LAN %s указывается с маской, например %s", l.Iface, DefaultLANAddr(i))
		}
		for j, o := range nets {
			if o.Overlaps(p.Masked()) {
				return fmt.Errorf("сети LAN %s и %s пересекаются", a.LANs[j].Iface, l.Iface)
			}
		}
		nets = append(nets, p.Masked())
	}
	a.SSHKey = strings.TrimSpace(a.SSHKey)
	if a.SSHKey != "" && !sshKeyRe.MatchString(a.SSHKey) {
		return errors.New("ключ SSH должен быть открытым ключом в одну строку (ssh-ed25519 …)")
	}
	return nil
}

func (n *Addr) validate(role string, allowDHCP bool) error {
	if n.DHCP && allowDHCP {
		return nil
	}
	p, err := netip.ParsePrefix(n.Address)
	if err != nil {
		return fmt.Errorf("адрес %s указывается с маской, например 10.0.0.10/24", role)
	}
	if n.Gateway != "" {
		gw, err := netip.ParseAddr(n.Gateway)
		if err != nil || gw.Is4() != p.Addr().Is4() {
			return fmt.Errorf("неверный шлюз %s", role)
		}
	}
	for _, d := range n.DNS {
		if _, err := netip.ParseAddr(d); err != nil {
			return fmt.Errorf("неверный DNS-сервер %q", d)
		}
	}
	return nil
}
