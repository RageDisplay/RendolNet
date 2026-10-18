//go:build !linux

package dataplane

import (
	"errors"
	"log/slog"
	"net/netip"
)

const DefaultPinDir = ""

var errUnsupported = errors.New("dataplane: eBPF работает только в Linux")

// Заглушка, чтобы остальной код собирался и тестировался в Windows и macOS.
type Dataplane struct{}

func Open(*slog.Logger, string) (*Dataplane, error)           { return nil, errUnsupported }
func (d *Dataplane) Close() error                             { return nil }
func (d *Dataplane) Apply(*Plan) error                        { return errUnsupported }
func (d *Dataplane) RefreshAddresses()                        {}
func (d *Dataplane) Detach()                                  {}
func (d *Dataplane) RuleCounters() map[string]RuleCounter     { return nil }
func (d *Dataplane) ResetRuleCounter(uint32)                  {}
func (d *Dataplane) Stats() Stats                             { return Stats{} }
func (d *Dataplane) Sessions(int) []Session                   { return nil }
func (d *Dataplane) GC(func(Event)) (int, int)                { return 0, 0 }
func (d *Dataplane) FlushSessions()                           {}
func (d *Dataplane) Events(func(Event)) error                 { return errUnsupported }
func (d *Dataplane) XDPMode() string                          { return "" }
func (d *Dataplane) Generation() uint32                       { return 0 }
func (d *Dataplane) SetAddr(uint32, netip.Prefix, bool) error { return errUnsupported }

var _ = decodeEvent
