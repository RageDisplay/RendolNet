//go:build linux

package agent

import (
	"io"
	"log/slog"
	"os"
	"runtime"
	"testing"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"rendolnet/internal/netcfg"
)

func TestWireGuardSync(t *testing.T) {
	if os.Getenv("RN_BPF_TEST") == "" {
		t.Skip("нужен RN_BPF_TEST=1 и права root")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	host, err := netns.Get()
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	defer func() { _ = netns.Set(host) }()
	ns, err := netns.New()
	if err != nil {
		t.Fatal(err)
	}
	defer ns.Close()

	foreign := &netlink.Wireguard{LinkAttrs: netlink.LinkAttrs{Name: "wg-foreign"}}
	if err := netlink.LinkAdd(foreign); err != nil {
		t.Fatal(err)
	}
	serverKey, _ := wgtypes.GeneratePrivateKey()
	peerKey, _ := wgtypes.GeneratePrivateKey()
	secondKey, _ := wgtypes.GeneratePrivateKey()
	config := &netcfg.Config{Interfaces: []netcfg.Interface{{
		Name: "wg-test", Role: netcfg.RoleLAN, Zone: "lan", Enabled: true, Addresses: []string{"10.200.0.1/24"},
		WireGuard: &netcfg.WireGuardConfig{ListenPort: 51820, PrivateKey: serverKey.String(), Peers: []netcfg.WireGuardPeer{
			{Name: "first", PublicKey: peerKey.PublicKey().String(), AllowedIPs: []string{"10.200.0.2/32"}, PersistentKeepalive: 25},
			{Name: "second", PublicKey: secondKey.PublicKey().String(), AllowedIPs: []string{"10.200.0.3/32"}},
		}},
	}}}
	n := NewLinuxNetwork(slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	if err := n.syncWireGuard(config); err != nil {
		t.Fatal(err)
	}
	client, err := wgctrl.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	device, err := client.Device("wg-test")
	if err != nil {
		t.Fatal(err)
	}
	if device.PrivateKey != serverKey || device.ListenPort != 51820 || len(device.Peers) != 2 {
		t.Fatalf("неожиданная конфигурация: %+v", device)
	}
	link, err := netlink.LinkByName("wg-test")
	if err != nil || link.Attrs().Alias != linkAlias {
		t.Fatalf("управляемая метка: link=%v err=%v", link, err)
	}

	config.Interfaces[0].WireGuard.ListenPort = 51821
	config.Interfaces[0].WireGuard.Peers = config.Interfaces[0].WireGuard.Peers[:1]
	config.Interfaces[0].WireGuard.Peers[0].AllowedIPs = []string{"10.200.0.20/32"}
	if err := n.syncWireGuard(config); err != nil {
		t.Fatal(err)
	}
	device, err = client.Device("wg-test")
	if err != nil {
		t.Fatal(err)
	}
	if device.ListenPort != 51821 || len(device.Peers) != 1 || len(device.Peers[0].AllowedIPs) != 1 || device.Peers[0].AllowedIPs[0].String() != "10.200.0.20/32" {
		t.Fatalf("обновление не применено: %+v", device)
	}
	if err := n.syncWireGuard(&netcfg.Config{}); err != nil {
		t.Fatal(err)
	}
	if _, err := netlink.LinkByName("wg-test"); err == nil {
		t.Fatal("управляемый туннель не удалён")
	}
	if _, err := netlink.LinkByName("wg-foreign"); err != nil {
		t.Fatalf("чужой туннель затронут: %v", err)
	}
}
