//go:build !linux

package agent

import (
	"context"
	"errors"
	"log/slog"

	"rendolnet/internal/netcfg"
)

type LinuxNetwork struct{}

func NewLinuxNetwork(*slog.Logger) *LinuxNetwork { return &LinuxNetwork{} }

var errNotLinux = errors.New("agent: сеть настраивается только в Linux")

func (n *LinuxNetwork) Apply(context.Context, *netcfg.Config) error { return errNotLinux }
func (n *LinuxNetwork) Links() ([]Link, error)                      { return nil, errNotLinux }
func (n *LinuxNetwork) Watch(context.Context, func())               {}
func (n *LinuxNetwork) DisableBootGuard() error                     { return nil }
