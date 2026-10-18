//go:build !linux

package agent

import (
	"context"
	"errors"
	"log/slog"

	"rendolnet/internal/dhcpd"
	"rendolnet/internal/netcfg"
)

type LinuxNetwork struct{}

func NewLinuxNetwork(*slog.Logger, string) *LinuxNetwork { return &LinuxNetwork{} }

var errNotLinux = errors.New("agent: сеть настраивается только в Linux")

func (n *LinuxNetwork) Apply(context.Context, *netcfg.Config) error { return errNotLinux }
func (n *LinuxNetwork) Links() ([]Link, error)                      { return nil, errNotLinux }
func (n *LinuxNetwork) Watch(context.Context, func())               {}
func (n *LinuxNetwork) DisableBootGuard() error                     { return nil }
func (n *LinuxNetwork) Gateways() []Gateway                         { return nil }
func (n *LinuxNetwork) Leases() []dhcpd.Lease                       { return nil }
func (n *LinuxNetwork) SetDNSObserver(func([]byte))                 {}
