//go:build linux

package agent

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"github.com/vishvananda/netlink"
)

type dhcpRunner struct {
	log    *slog.Logger
	iface  string
	ctx    context.Context
	cancel context.CancelFunc
}

func newDHCPRunner(log *slog.Logger, iface string) *dhcpRunner {
	ctx, cancel := context.WithCancel(context.Background())
	return &dhcpRunner{log: log, iface: iface, ctx: ctx, cancel: cancel}
}

func (r *dhcpRunner) stop() { r.cancel() }

func (r *dhcpRunner) run() {
	var lease *nclient4.Lease
	var applied *netlink.Addr
	defer func() {
		if applied != nil {
			if l, err := netlink.LinkByName(r.iface); err == nil {
				_ = netlink.AddrDel(l, applied)
			}
		}
	}()

	backoff := 2 * time.Second
	for r.ctx.Err() == nil {
		c, err := nclient4.New(r.iface, nclient4.WithTimeout(5*time.Second), nclient4.WithRetry(3))
		if err != nil {
			r.log.Warn("dhcp: клиент", "err", err)
			r.sleep(backoff)
			continue
		}
		ctx, cancel := context.WithTimeout(r.ctx, 30*time.Second)
		if lease != nil {
			lease, err = c.Renew(ctx, lease)
		} else {
			lease, err = c.Request(ctx)
		}
		cancel()
		_ = c.Close()
		if err != nil {
			r.log.Warn("dhcp: аренда не получена", "err", err)
			lease = nil
			r.sleep(backoff)
			if backoff < time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = 2 * time.Second

		ack := lease.ACK
		lt := ack.IPAddressLeaseTime(time.Hour)
		addr := &netlink.Addr{
			IPNet:       &net.IPNet{IP: ack.YourIPAddr, Mask: ack.SubnetMask()},
			ValidLft:    int(lt.Seconds()),
			PreferedLft: int(lt.Seconds()),
		}
		if l, err := netlink.LinkByName(r.iface); err == nil {
			if applied != nil && !applied.IP.Equal(addr.IP) {
				_ = netlink.AddrDel(l, applied)
			}
			if err := netlink.AddrReplace(l, addr); err != nil {
				r.log.Warn("dhcp: адрес", "err", err)
			}
			applied = addr
			if gws := ack.Router(); len(gws) > 0 {
				_ = netlink.RouteReplace(&netlink.Route{
					LinkIndex: l.Attrs().Index,
					Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
					Gw:        gws[0],
					Protocol:  rtProtoDHCP,
					Priority:  1024,
				})
			}
		}
		r.log.Info("dhcp: аренда", "addr", addr.IPNet.String(), "lease", lt)

		renew := ack.IPAddressRenewalTime(lt / 2)
		if renew < 30*time.Second {
			renew = 30 * time.Second
		}
		r.sleep(renew)
	}
}

func (r *dhcpRunner) sleep(d time.Duration) {
	select {
	case <-r.ctx.Done():
	case <-time.After(d):
	}
}
