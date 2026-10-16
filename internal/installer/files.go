package installer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"rendolnet/internal/netcfg"
)

// Маршрут по умолчанию через сеть управления нужен, только когда нет WAN:
// иначе трафик LAN ушёл бы в интернет через управление.
const mgmtRouteMetric = 2048

func MgmtNetworkUnit(iface string, a Addr) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Match]\nName=%s\n\n[Network]\n", iface)
	if a.DHCP {
		b.WriteString("DHCP=ipv4\nIPv6AcceptRA=no\nLLMNR=no\nMulticastDNS=no\n\n")
		fmt.Fprintf(&b, "[DHCPv4]\nRouteMetric=%d\nUseHostname=no\n", mgmtRouteMetric)
		return b.String()
	}
	fmt.Fprintf(&b, "Address=%s\n", a.Address)
	for _, d := range a.DNS {
		fmt.Fprintf(&b, "DNS=%s\n", d)
	}
	b.WriteString("IPv6AcceptRA=no\nLLMNR=no\nMulticastDNS=no\n")
	if a.Gateway != "" {
		fmt.Fprintf(&b, "\n[Route]\nGateway=%s\nMetric=%d\n", a.Gateway, mgmtRouteMetric)
	}
	return b.String()
}

// BootGuard — таблица nftables, которая с загрузки и до запуска агента
// держит закрытыми транзит и доступ к узлу через WAN и LAN. Агент удаляет её,
// когда eBPF-политика уже на месте. Файл не делает flush ruleset: перезапуск
// nftables.service не должен сносить правила k3s.
func BootGuard(dataIfaces []string) string {
	if len(dataIfaces) == 0 {
		return "table inet rendolnet_boot\ndelete table inet rendolnet_boot\n"
	}
	set := `"` + strings.Join(dataIfaces, `", "`) + `"`
	return fmt.Sprintf(`table inet rendolnet_boot
delete table inet rendolnet_boot
table inet rendolnet_boot {
	chain input {
		type filter hook input priority -10; policy accept;
		iifname { %[1]s } ct state established,related accept
		iifname { %[1]s } udp sport 67 udp dport 68 accept
		iifname { %[1]s } drop
	}
	chain forward {
		type filter hook forward priority -10; policy accept;
		iifname { %[1]s } drop
		oifname { %[1]s } drop
	}
}
`, set)
}

// SeedConfig — стартовый конфиг агента, тот же формат, что пишет агент.
func SeedConfig(a Answers) ([]byte, error) {
	seed := netcfg.Seed{
		Hostname: a.Hostname, Mgmt: a.Mgmt, WAN: a.WAN, LAN: a.LAN, LANAddr: a.LANAddr,
		WANDHCP: a.WANAddr.DHCP, WANAddr: a.WANAddr.Address, Gateway: a.WANAddr.Gateway,
	}
	c := netcfg.Default(seed)
	if len(a.WANAddr.DNS) > 0 {
		c.System.DNS = a.WANAddr.DNS
	}
	if err := netcfg.Validate(&c); err != nil {
		return nil, err
	}
	return json.MarshalIndent(map[string]any{"revision": 0, "config": c, "applied": time.Now()}, "", "  ")
}

func DBSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: rendolnet
---
apiVersion: v1
kind: Secret
metadata:
  name: rendolnet-db
  namespace: rendolnet
stringData:
  password: %s
`, hex.EncodeToString(b))
}

func FstabText(rootUUID, espUUID string) string {
	return fmt.Sprintf("UUID=%s\t/\text4\terrors=remount-ro,noatime\t0\t1\n", rootUUID) +
		fmt.Sprintf("UUID=%s\t/boot/efi\tvfat\tumask=0077,noatime\t0\t2\n", espUUID) +
		"tmpfs\t/tmp\ttmpfs\tnosuid,nodev,mode=1777,size=25%\t0\t0\n"
}
