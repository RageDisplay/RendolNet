package dnsfwd

import (
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func listenConfig() net.ListenConfig {
	return net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_FREEBIND, 1)
		})
		if err != nil {
			return err
		}
		return serr
	}}
}
