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
	LAN      string `yaml:"lan"`
	LANAddr  string `yaml:"lan_addr"`

	SSHKey string `yaml:"ssh_key"`
	Reboot bool   `yaml:"reboot"`
}

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
	seen := map[string]string{}
	for role, name := range map[string]string{"управления": a.Mgmt, "WAN": a.WAN, "LAN": a.LAN} {
		if name == "" {
			continue
		}
		if !ifaceRe.MatchString(name) {
			return fmt.Errorf("недопустимое имя интерфейса %q", name)
		}
		if other, ok := seen[name]; ok {
			return fmt.Errorf("%s назначен и интерфейсом %s, и %s", name, other, role)
		}
		seen[name] = role
	}
	if err := a.MgmtAddr.validate("управления", true); err != nil {
		return err
	}
	if a.WAN != "" {
		if err := a.WANAddr.validate("WAN", true); err != nil {
			return err
		}
	}
	if a.LAN != "" {
		if a.LANAddr == "" {
			a.LANAddr = "192.168.1.1/24"
		}
		if _, err := netip.ParsePrefix(a.LANAddr); err != nil {
			return errors.New("адрес LAN указывается с маской, например 192.168.1.1/24")
		}
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
