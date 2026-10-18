// Package dnsfwd — DNS-прокси для клиентов LAN: принимает запросы на адресах
// узла и пересылает их вышестоящим серверам как есть, без кэша и без разбора.
package dnsfwd

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

const timeout = 2 * time.Second

type Forwarder struct {
	log       *slog.Logger
	upstreams []string
	observe   func([]byte)
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// Start поднимает UDP и TCP на порту port каждого адреса из listen.
// Адрес, которого на узле ещё нет, тоже принимается (IP_FREEBIND).
// observe, если задан, видит каждый ответ до того, как его получит клиент:
// так адреса доменов попадают в правила раньше, чем клиент к ним пойдёт.
func Start(log *slog.Logger, listen []netip.Addr, port int, upstreams []netip.AddrPort, observe func([]byte)) (*Forwarder, error) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &Forwarder{log: log, ctx: ctx, cancel: cancel, observe: observe}
	for _, u := range upstreams {
		f.upstreams = append(f.upstreams, u.String())
	}
	lc := listenConfig()
	var errs []error
	for _, a := range listen {
		addr := net.JoinHostPort(a.String(), strconv.Itoa(port))
		pc, err := lc.ListenPacket(ctx, "udp", addr)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ln, err := lc.Listen(ctx, "tcp", addr)
		if err != nil {
			_ = pc.Close()
			errs = append(errs, err)
			continue
		}
		f.wg.Add(2)
		go f.serveUDP(ctx, pc)
		go f.serveTCP(ctx, ln)
	}
	if len(errs) > 0 && len(errs) == len(listen) {
		cancel()
		return nil, errors.Join(errs...)
	}
	return f, errors.Join(errs...)
}

func (f *Forwarder) Close() {
	f.cancel()
	f.wg.Wait()
}

func (f *Forwarder) serveUDP(ctx context.Context, pc net.PacketConn) {
	defer f.wg.Done()
	go func() {
		<-ctx.Done()
		_ = pc.Close()
	}()
	for {
		buf := make([]byte, 4096)
		n, peer, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		go func() {
			if resp := f.exchangeUDP(buf[:n]); resp != nil {
				f.seen(resp)
				_, _ = pc.WriteTo(resp, peer)
			}
		}()
	}
}

func (f *Forwarder) exchangeUDP(q []byte) []byte {
	for _, up := range f.upstreams {
		c, err := f.dial("udp", up)
		if err != nil {
			continue
		}
		_ = c.SetDeadline(time.Now().Add(timeout))
		resp := make([]byte, 65535)
		n := 0
		if _, err = c.Write(q); err == nil {
			n, err = c.Read(resp)
		}
		_ = c.Close()
		// ответ должен быть на этот же запрос: совпадает ID
		if err == nil && n >= 2 && len(q) >= 2 && resp[0] == q[0] && resp[1] == q[1] {
			return resp[:n]
		}
	}
	return nil
}

func (f *Forwarder) serveTCP(ctx context.Context, ln net.Listener) {
	defer f.wg.Done()
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go f.handleTCP(c)
	}
}

func (f *Forwarder) dial(network, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	return d.DialContext(f.ctx, network, addr)
}

func (f *Forwarder) handleTCP(c net.Conn) {
	defer func() { _ = c.Close() }()
	for {
		_ = c.SetDeadline(time.Now().Add(10 * time.Second))
		q, err := readMsg(c)
		if err != nil {
			return
		}
		resp := f.exchangeTCP(q)
		if resp == nil {
			return
		}
		f.seen(resp)
		if err := writeMsg(c, resp); err != nil {
			return
		}
	}
}

func (f *Forwarder) exchangeTCP(q []byte) []byte {
	for _, up := range f.upstreams {
		c, err := f.dial("tcp", up)
		if err != nil {
			continue
		}
		_ = c.SetDeadline(time.Now().Add(2 * timeout))
		var resp []byte
		if err = writeMsg(c, q); err == nil {
			resp, err = readMsg(c)
		}
		_ = c.Close()
		if err == nil {
			return resp
		}
	}
	return nil
}

func (f *Forwarder) seen(resp []byte) {
	if f.observe != nil {
		f.observe(resp)
	}
}

func readMsg(r io.Reader) ([]byte, error) {
	var l [2]byte
	if _, err := io.ReadFull(r, l[:]); err != nil {
		return nil, err
	}
	b := make([]byte, binary.BigEndian.Uint16(l[:]))
	_, err := io.ReadFull(r, b)
	return b, err
}

func writeMsg(w io.Writer, b []byte) error {
	out := make([]byte, 2+len(b))
	binary.BigEndian.PutUint16(out, uint16(len(b)))
	copy(out[2:], b)
	_, err := w.Write(out)
	return err
}
