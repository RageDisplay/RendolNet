//go:build !linux

package dnsfwd

import "net"

func listenConfig() net.ListenConfig { return net.ListenConfig{} }
