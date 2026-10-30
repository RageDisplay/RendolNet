//go:build linux

package agent

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"rendolnet/internal/netcfg"
)

func (n *LinuxNetwork) syncWireGuard(c *netcfg.Config) error {
	want := map[string]*netcfg.Interface{}
	for i := range c.Interfaces {
		if c.Interfaces[i].WireGuard != nil {
			want[c.Interfaces[i].Name] = &c.Interfaces[i]
		}
	}
	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("wireguard: %w", err)
	}
	defer func() { _ = client.Close() }()

	var errs []error
	for name, it := range want {
		link, err := netlink.LinkByName(name)
		if err != nil {
			var notFound netlink.LinkNotFoundError
			if !errors.As(err, &notFound) {
				errs = append(errs, fmt.Errorf("wireguard %s: %w", name, err))
				continue
			}
			link = &netlink.Wireguard{LinkAttrs: netlink.LinkAttrs{Name: name}}
			if err := netlink.LinkAdd(link); err != nil {
				errs = append(errs, fmt.Errorf("wireguard %s: создание: %w", name, err))
				continue
			}
		} else if link.Type() != "wireguard" {
			errs = append(errs, fmt.Errorf("wireguard %s: имя занято интерфейсом %s", name, link.Type()))
			continue
		}
		if err := netlink.LinkSetAlias(link, linkAlias); err != nil {
			errs = append(errs, fmt.Errorf("wireguard %s: метка: %w", name, err))
		}
		if err := configureWireGuard(client, it); err != nil {
			errs = append(errs, fmt.Errorf("wireguard %s: %w", name, err))
		}
	}

	links, err := netlink.LinkList()
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, link := range links {
		if link.Type() == "wireguard" && link.Attrs().Alias == linkAlias {
			if _, ok := want[link.Attrs().Name]; !ok {
				if err := netlink.LinkDel(link); err != nil {
					errs = append(errs, fmt.Errorf("wireguard %s: удаление: %w", link.Attrs().Name, err))
				}
			}
		}
	}
	return errors.Join(errs...)
}

func configureWireGuard(client *wgctrl.Client, it *netcfg.Interface) error {
	wg := it.WireGuard
	privateKey, err := wgtypes.ParseKey(wg.PrivateKey)
	if err != nil {
		return err
	}
	cfg := wgtypes.Config{
		PrivateKey:   &privateKey,
		ListenPort:   &wg.ListenPort,
		ReplacePeers: true,
		Peers:        make([]wgtypes.PeerConfig, 0, len(wg.Peers)),
	}
	for _, peer := range wg.Peers {
		publicKey, err := wgtypes.ParseKey(peer.PublicKey)
		if err != nil {
			return err
		}
		pc := wgtypes.PeerConfig{
			PublicKey:         publicKey,
			ReplaceAllowedIPs: true,
			AllowedIPs:        make([]net.IPNet, 0, len(peer.AllowedIPs)),
		}
		if peer.PresharedKey != "" {
			key, err := wgtypes.ParseKey(peer.PresharedKey)
			if err != nil {
				return err
			}
			pc.PresharedKey = &key
		}
		if peer.Endpoint != "" {
			endpoint, err := net.ResolveUDPAddr("udp", peer.Endpoint)
			if err != nil {
				return fmt.Errorf("endpoint %s: %w", peer.Endpoint, err)
			}
			pc.Endpoint = endpoint
		}
		if peer.PersistentKeepalive > 0 {
			keepalive := time.Duration(peer.PersistentKeepalive) * time.Second
			pc.PersistentKeepaliveInterval = &keepalive
		}
		for _, s := range peer.AllowedIPs {
			pfx, err := netip.ParsePrefix(s)
			if err != nil {
				return err
			}
			pc.AllowedIPs = append(pc.AllowedIPs, net.IPNet{
				IP:   pfx.Addr().AsSlice(),
				Mask: net.CIDRMask(pfx.Bits(), pfx.Addr().BitLen()),
			})
		}
		cfg.Peers = append(cfg.Peers, pc)
	}
	return client.ConfigureDevice(it.Name, cfg)
}

func (n *LinuxNetwork) WireGuard() ([]WireGuardStatus, error) {
	client, err := wgctrl.New()
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()
	devices, err := client.Devices()
	if err != nil {
		return nil, err
	}
	out := make([]WireGuardStatus, 0, len(devices))
	for _, device := range devices {
		status := WireGuardStatus{
			Name: device.Name, PublicKey: device.PublicKey.String(), ListenPort: device.ListenPort,
			Peers: make([]WireGuardPeerState, 0, len(device.Peers)),
		}
		for _, peer := range device.Peers {
			state := WireGuardPeerState{
				PublicKey: peer.PublicKey.String(), LastHandshake: peer.LastHandshakeTime,
				RxBytes: peer.ReceiveBytes, TxBytes: peer.TransmitBytes,
				AllowedIPs: make([]string, 0, len(peer.AllowedIPs)),
			}
			if peer.Endpoint != nil {
				state.Endpoint = peer.Endpoint.String()
			}
			for _, pfx := range peer.AllowedIPs {
				state.AllowedIPs = append(state.AllowedIPs, pfx.String())
			}
			status.Peers = append(status.Peers, state)
		}
		out = append(out, status)
	}
	return out, nil
}
