package agent

import (
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type WireGuardStatus struct {
	Name       string               `json:"name"`
	PublicKey  string               `json:"public_key"`
	ListenPort int                  `json:"listen_port"`
	Peers      []WireGuardPeerState `json:"peers"`
}

type WireGuardPeerState struct {
	PublicKey     string    `json:"public_key"`
	Endpoint      string    `json:"endpoint,omitempty"`
	AllowedIPs    []string  `json:"allowed_ips"`
	LastHandshake time.Time `json:"last_handshake"`
	RxBytes       int64     `json:"rx_bytes"`
	TxBytes       int64     `json:"tx_bytes"`
}

type WireGuardKeypair struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

func generateWireGuardKeypair() (WireGuardKeypair, error) {
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return WireGuardKeypair{}, err
	}
	return WireGuardKeypair{PrivateKey: key.String(), PublicKey: key.PublicKey().String()}, nil
}
