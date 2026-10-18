package dhcpd

import (
	"context"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
)

// Serve слушает порт 67 на интерфейсе до отмены ctx. changed вызывается
// после каждого подтверждения или освобождения аренды.
func (s *Server) Serve(ctx context.Context, changed func()) error {
	srv, err := server4.NewServer(s.cfg.Iface, &net.UDPAddr{Port: dhcpv4.ServerPort}, func(conn net.PacketConn, peer net.Addr, req *dhcpv4.DHCPv4) {
		resp := s.Handle(req, time.Now())
		if resp == nil {
			if t := req.MessageType(); t == dhcpv4.MessageTypeRelease || t == dhcpv4.MessageTypeDecline {
				changed()
			}
			return
		}
		to := &net.UDPAddr{IP: net.IPv4bcast, Port: dhcpv4.ClientPort}
		// у клиента уже есть адрес (продление, INFORM) — отвечаем ему напрямую
		if !req.ClientIPAddr.IsUnspecified() && req.ClientIPAddr != nil && resp.MessageType() != dhcpv4.MessageTypeNak {
			to.IP = req.ClientIPAddr
		}
		_, _ = conn.WriteTo(resp.ToBytes(), to)
		if resp.MessageType() == dhcpv4.MessageTypeAck {
			changed()
		}
	})
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	serveErr := srv.Serve()
	select {
	case <-ctx.Done():
		return nil // сокет закрыт нами
	default:
		return serveErr
	}
}
