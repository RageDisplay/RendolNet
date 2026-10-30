package netcfg

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func testWireGuardConfig() Config {
	c := seedConfig()
	key := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	c.Interfaces = append(c.Interfaces, Interface{
		Name: "wg0", Role: RoleLAN, Zone: "lan", Enabled: true, Addresses: []string{"10.200.0.1/24"}, MTU: 1420,
		WireGuard: &WireGuardConfig{
			ListenPort: 51820, PrivateKey: key(1),
			Peers: []WireGuardPeer{
				{Name: "laptop", PublicKey: key(2), PresharedKey: key(3), AllowedIPs: []string{"10.200.0.2/32"}, Endpoint: "vpn.example.com:51820", PersistentKeepalive: 25},
				{Name: "phone", PublicKey: key(4), AllowedIPs: []string{"10.200.0.3/32"}},
			},
		},
	})
	return c
}

func TestWireGuardValidation(t *testing.T) {
	c := testWireGuardConfig()
	if err := ValidateCommit(&c); err != nil {
		t.Fatalf("корректный WireGuard не прошёл проверку: %v", err)
	}
	for _, tc := range []struct {
		name string
		edit func(*Interface)
		want string
	}{
		{"ключ", func(it *Interface) { it.WireGuard.PrivateKey = "secret" }, "private_key"},
		{"порт", func(it *Interface) { it.WireGuard.ListenPort = 0 }, "listen_port"},
		{"без allowed", func(it *Interface) { it.WireGuard.Peers[0].AllowedIPs = nil }, "allowed_ips"},
		{"пересечение", func(it *Interface) { it.WireGuard.Peers[1].AllowedIPs = []string{"10.200.0.0/24"} }, "пересекается"},
		{"повтор ключа", func(it *Interface) { it.WireGuard.Peers[1].PublicKey = it.WireGuard.Peers[0].PublicKey }, "public_key"},
		{"повтор имени", func(it *Interface) { it.WireGuard.Peers[1].Name = it.WireGuard.Peers[0].Name }, "peers[1].name"},
		{"endpoint", func(it *Interface) { it.WireGuard.Peers[0].Endpoint = "example.com" }, "endpoint"},
		{"keepalive", func(it *Interface) { it.WireGuard.Peers[0].PersistentKeepalive = 65536 }, "persistent_keepalive"},
		{"dhcp", func(it *Interface) { it.DHCP = true; it.Addresses = nil }, "несовместим"},
		{"mgmt", func(it *Interface) { it.Role = RoleMgmt }, "несовместим"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testWireGuardConfig()
			tc.edit(&c.Interfaces[len(c.Interfaces)-1])
			if err := Validate(&c); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ожидалась ошибка %q, получено %v", tc.want, err)
			}
		})
	}
}
