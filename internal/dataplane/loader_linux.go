//go:build linux

package dataplane

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"golang.org/x/sys/unix"

	bpf "rendolnet/internal/dataplane/ebpf"
)

const DefaultPinDir = "/sys/fs/bpf/rendolnet"

type Dataplane struct {
	mu        sync.Mutex
	log       *slog.Logger
	pinDir    string
	objs      bpf.RendolObjects
	gen       uint32
	plan      *Plan
	ruleIDs   map[uint32]string
	zoneNames map[uint16]string
	ifIndex   map[string]int
	attached  map[string]bool
	dnatKeys  map[bpf.RendolDnatKey]bool
	blockKeys map[bpf.RendolBlockKey]bool
	live      int
	xdpMode   string
}

// Open загружает программы и карты. Карты состояния (conntrack, NAT) берутся
// закреплёнными с прошлого запуска, если формат совпадает: так перезапуск
// агента не рвёт сессии.
func Open(log *slog.Logger, pinDir string) (*Dataplane, error) {
	if pinDir == "" {
		pinDir = DefaultPinDir
	}
	if err := ensureBPFFS(filepath.Dir(pinDir)); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(pinDir, "links"), 0o700); err != nil {
		return nil, fmt.Errorf("dataplane: каталог bpffs: %w", err)
	}
	d := &Dataplane{
		log: log, pinDir: pinDir,
		ruleIDs: map[uint32]string{}, zoneNames: map[uint16]string{},
		ifIndex: map[string]int{}, attached: map[string]bool{},
		dnatKeys: map[bpf.RendolDnatKey]bool{}, blockKeys: map[bpf.RendolBlockKey]bool{},
		xdpMode: "native",
	}
	opts := &ebpf.CollectionOptions{Maps: ebpf.MapOptions{PinPath: pinDir}}
	err := bpf.LoadRendolObjects(&d.objs, opts)
	if errors.Is(err, ebpf.ErrMapIncompatible) {
		log.Warn("формат закреплённых карт изменился, состояние сессий сбрасывается")
		for _, name := range []string{"ct", "snat_rev"} {
			_ = os.Remove(filepath.Join(pinDir, name))
		}
		err = bpf.LoadRendolObjects(&d.objs, opts)
	}
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			return nil, fmt.Errorf("dataplane: верификатор отверг программу: %w", ve)
		}
		return nil, fmt.Errorf("dataplane: загрузка: %w", err)
	}
	var zero uint32
	_ = d.objs.Active.Lookup(&zero, &d.gen)
	return d, nil
}

func (d *Dataplane) Close() error {
	return d.objs.Close()
}

// Apply применяет план. Политика пишется в неактивный банк и включается одной
// записью в карту active.
func (d *Dataplane) Apply(p *Plan) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	links, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("dataplane: список интерфейсов: %w", err)
	}
	present := map[string]int{}
	for _, l := range links {
		present[l.Name] = l.Index
	}
	d.ifIndex = map[string]int{}
	for name := range p.Ifaces {
		if idx, ok := present[name]; ok {
			d.ifIndex[name] = idx
		} else {
			d.log.Warn("интерфейс из конфига отсутствует в системе", "iface", name)
		}
	}

	next := d.gen + 1
	bank := next & 1
	if err := d.writeBank(p, bank, next); err != nil {
		return err
	}
	if err := d.syncIfaces(p); err != nil {
		return err
	}
	if err := d.syncDNAT(p); err != nil {
		return err
	}
	if err := d.syncBlocklist(p); err != nil {
		return err
	}
	var zero uint32
	if err := d.objs.Active.Put(&zero, &next); err != nil {
		return fmt.Errorf("dataplane: переключение банка: %w", err)
	}
	d.gen = next
	d.plan = p

	newIDs := map[uint32]string{}
	for id, slot := range p.RuleSlots {
		newIDs[slot] = id
		if d.ruleIDs[slot] != id {
			d.ResetRuleCounter(slot)
		}
	}
	d.ruleIDs = newIDs
	d.zoneNames = map[uint16]string{}
	for name, id := range p.Zones {
		d.zoneNames[id] = name
	}

	if err := d.attach(); err != nil {
		return err
	}
	d.clearBank(bank ^ 1)
	return nil
}

func (d *Dataplane) writeBank(p *Plan, bank, gen uint32) error {
	d.clearBank(bank)

	setID := func(s uint32) uint32 {
		if s == 0 {
			return 0
		}
		return s | bank<<31
	}
	for id, prefixes := range p.Sets {
		one := uint8(1)
		for _, pfx := range prefixes {
			k := bpf.RendolSetKey{Prefixlen: 32 + prefixBits(pfx), Set: setID(id), A: toAddr(pfx.Addr())}
			if err := d.objs.AddrSets.Put(&k, &one); err != nil {
				return fmt.Errorf("dataplane: набор адресов: %w", err)
			}
		}
	}

	base := bank * MaxRules
	for i, r := range p.Rules {
		idx := base + uint32(i)
		v := bpf.RendolRule{
			SrcSet: setID(r.SrcSet), DstSet: setID(r.DstSet), RuleId: r.Slot,
			DportLo: r.PortLo, DportHi: r.PortHi, Proto: r.Proto, Action: r.Action, Flags: r.Flags,
		}
		if err := d.objs.Rules.Put(&idx, &v); err != nil {
			return fmt.Errorf("dataplane: правило %d: %w", i, err)
		}
	}
	for k, span := range p.Pairs {
		pk := bpf.RendolPairKey{Bank: bank, From: k.From, To: k.To}
		pv := bpf.RendolPairVal{Start: base + span.Start, Count: span.Count}
		if err := d.objs.Pairs.Put(&pk, &pv); err != nil {
			return fmt.Errorf("dataplane: пара зон: %w", err)
		}
	}

	cfg := bpf.RendolBankCfg{Gen: gen, SynRate: p.SYNRate, SynBurst: p.SYNBurst}
	if p.LogDefault {
		cfg.LogDefault = 1
	}
	n := uint32(0)
	for _, s := range p.SNAT {
		idx, ok := d.ifIndex[s.Interface]
		if !ok {
			continue
		}
		r := bpf.RendolSnatRule{OutIfindex: uint32(idx), SrcSet: setID(s.SrcSet)}
		if s.ToAddr.IsValid() {
			r.ToAddr = be32(s.ToAddr)
		}
		key := bank*MaxSNATRules + n
		if err := d.objs.SnatRules.Put(&key, &r); err != nil {
			return fmt.Errorf("dataplane: SNAT: %w", err)
		}
		n++
	}
	cfg.SnatCount = n
	if idx, ok := d.ifIndex[p.Mgmt.Interface]; ok {
		cfg.MgmtIfindex = uint32(idx)
		cfg.MgmtSet = setID(p.Mgmt.Set)
		for i, port := range p.Mgmt.Ports {
			if i < MaxMgmtPorts {
				cfg.MgmtPorts[i] = port
			}
		}
	}
	if err := d.objs.Bankcfg.Put(&bank, &cfg); err != nil {
		return fmt.Errorf("dataplane: настройки банка: %w", err)
	}
	return nil
}

// clearBank убирает записи банка из хеш-карт; массивы просто перезаписываются.
func (d *Dataplane) clearBank(bank uint32) {
	var pk bpf.RendolPairKey
	var pv bpf.RendolPairVal
	var stale []bpf.RendolPairKey
	it := d.objs.Pairs.Iterate()
	for it.Next(&pk, &pv) {
		if pk.Bank == bank {
			stale = append(stale, pk)
		}
	}
	for _, k := range stale {
		_ = d.objs.Pairs.Delete(&k)
	}

	var sk bpf.RendolSetKey
	var one uint8
	var staleSets []bpf.RendolSetKey
	it = d.objs.AddrSets.Iterate()
	for it.Next(&sk, &one) {
		if sk.Set>>31 == bank {
			staleSets = append(staleSets, sk)
		}
	}
	for _, k := range staleSets {
		_ = d.objs.AddrSets.Delete(&k)
	}
}

func (d *Dataplane) syncIfaces(p *Plan) error {
	want := map[uint32]bpf.RendolIface{}
	for name, ip := range p.Ifaces {
		idx, ok := d.ifIndex[name]
		if !ok {
			continue
		}
		v := bpf.RendolIface{Zone: ip.Zone, Role: ip.Role}
		if a, ok := primaryV4(name); ok {
			v.Addr4 = be32(a)
		}
		want[uint32(idx)] = v
	}
	var k uint32
	var v bpf.RendolIface
	var stale []uint32
	it := d.objs.Ifaces.Iterate()
	for it.Next(&k, &v) {
		if _, ok := want[k]; !ok {
			stale = append(stale, k)
		}
	}
	for _, k := range stale {
		_ = d.objs.Ifaces.Delete(&k)
	}
	for k, v := range want {
		if err := d.objs.Ifaces.Put(&k, &v); err != nil {
			return fmt.Errorf("dataplane: интерфейс %d: %w", k, err)
		}
	}
	return nil
}

// RefreshAddresses обновляет адреса интерфейсов в карте: DHCP на WAN может
// сменить адрес в любой момент, а маскарад должен знать текущий.
func (d *Dataplane) RefreshAddresses() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.plan != nil {
		if err := d.syncIfaces(d.plan); err != nil {
			d.log.Warn("обновление адресов интерфейсов", "err", err)
		}
	}
}

func primaryV4(name string) (netip.Addr, bool) {
	it, err := net.InterfaceByName(name)
	if err != nil {
		return netip.Addr{}, false
	}
	addrs, err := it.Addrs()
	if err != nil {
		return netip.Addr{}, false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			if v4 := n.IP.To4(); v4 != nil {
				addr, _ := netip.AddrFromSlice(v4)
				return addr, true
			}
		}
	}
	return netip.Addr{}, false
}

func (d *Dataplane) syncDNAT(p *Plan) error {
	want := map[bpf.RendolDnatKey]bpf.RendolDnatVal{}
	for _, r := range p.DNAT {
		idx, ok := d.ifIndex[r.Interface]
		if !ok {
			continue
		}
		k := bpf.RendolDnatKey{Ifindex: uint32(idx), Port: htons(r.Port), Proto: r.Proto}
		v := bpf.RendolDnatVal{ToAddr: be32(r.ToAddr), RuleId: RuleIDDNAT}
		if r.ToPort != 0 {
			v.ToPort = htons(r.ToPort)
		}
		if r.Allow {
			v.Allow = 1
		}
		want[k] = v
	}
	for k := range d.dnatKeys {
		if _, ok := want[k]; !ok {
			_ = d.objs.Dnat.Delete(&k)
		}
	}
	d.dnatKeys = map[bpf.RendolDnatKey]bool{}
	for k, v := range want {
		if err := d.objs.Dnat.Put(&k, &v); err != nil {
			return fmt.Errorf("dataplane: DNAT: %w", err)
		}
		d.dnatKeys[k] = true
	}
	return nil
}

func (d *Dataplane) syncBlocklist(p *Plan) error {
	want := map[bpf.RendolBlockKey]bool{}
	for _, pfx := range p.Blocklist {
		want[bpf.RendolBlockKey{Prefixlen: prefixBits(pfx), A: toAddr(pfx.Addr())}] = true
	}
	var k bpf.RendolBlockKey
	var v uint8
	var stale []bpf.RendolBlockKey
	it := d.objs.Blocklist.Iterate()
	for it.Next(&k, &v) {
		if !want[k] {
			stale = append(stale, k)
		}
	}
	for _, k := range stale {
		_ = d.objs.Blocklist.Delete(&k)
	}
	one := uint8(1)
	for k := range want {
		if err := d.objs.Blocklist.Put(&k, &one); err != nil {
			return fmt.Errorf("dataplane: блок-лист: %w", err)
		}
	}
	d.blockKeys = want
	return nil
}

func (d *Dataplane) linkPin(iface, kind string) string {
	return filepath.Join(d.pinDir, "links", iface+"_"+kind)
}

// attach подключает программы к интерфейсам плана и снимает с тех, что из
// плана ушли. Закреплённая связь переживает перезапуск агента; при новом
// запуске программа в ней подменяется без момента «без защиты».
func (d *Dataplane) attach() error {
	want := map[string]bool{}
	for name := range d.ifIndex {
		want[name] = true
	}
	entries, _ := os.ReadDir(filepath.Join(d.pinDir, "links"))
	for _, e := range entries {
		name := e.Name()
		for _, kind := range []string{"_xdp", "_in", "_out"} {
			if len(name) > len(kind) && name[len(name)-len(kind):] == kind {
				iface := name[:len(name)-len(kind)]
				if !want[iface] {
					d.detachPin(filepath.Join(d.pinDir, "links", name))
				}
			}
		}
	}

	var errs []error
	for name, idx := range d.ifIndex {
		if err := d.attachTC(name, idx, "in", d.objs.TcIngress, ebpf.AttachTCXIngress); err != nil {
			errs = append(errs, err)
		}
		if err := d.attachTC(name, idx, "out", d.objs.TcEgress, ebpf.AttachTCXEgress); err != nil {
			errs = append(errs, err)
		}
		if err := d.attachXDP(name, idx); err != nil {
			d.log.Warn("XDP не подключён, работает только TC", "iface", name, "err", err)
		}
		d.attached[name] = true
	}
	return errors.Join(errs...)
}

func (d *Dataplane) attachTC(name string, idx int, kind string, prog *ebpf.Program, at ebpf.AttachType) error {
	pin := d.linkPin(name, kind)
	if l, err := link.LoadPinnedLink(pin, nil); err == nil {
		defer func() { _ = l.Close() }()
		if err := l.Update(prog); err == nil {
			return nil
		}
		_ = l.Unpin()
	}
	l, err := link.AttachTCX(link.TCXOptions{Interface: idx, Program: prog, Attach: at})
	if err != nil {
		return fmt.Errorf("dataplane: tcx %s %s: %w", kind, name, err)
	}
	defer func() { _ = l.Close() }()
	if err := l.Pin(pin); err != nil {
		return fmt.Errorf("dataplane: закрепление связи %s: %w", pin, err)
	}
	return nil
}

func (d *Dataplane) attachXDP(name string, idx int) error {
	pin := d.linkPin(name, "xdp")
	if l, err := link.LoadPinnedLink(pin, nil); err == nil {
		defer func() { _ = l.Close() }()
		if err := l.Update(d.objs.XdpGuard); err == nil {
			return nil
		}
		_ = l.Unpin()
	}
	l, err := link.AttachXDP(link.XDPOptions{Program: d.objs.XdpGuard, Interface: idx, Flags: link.XDPDriverMode})
	if err != nil {
		// драйвер без нативного XDP (часть виртуальных карт, VLAN) — общий режим
		l, err = link.AttachXDP(link.XDPOptions{Program: d.objs.XdpGuard, Interface: idx, Flags: link.XDPGenericMode})
		if err != nil {
			return err
		}
		d.xdpMode = "generic"
	}
	defer func() { _ = l.Close() }()
	return l.Pin(pin)
}

func (d *Dataplane) detachPin(pin string) {
	l, err := link.LoadPinnedLink(pin, nil)
	if err != nil {
		_ = os.Remove(pin)
		return
	}
	_ = l.Unpin()
	_ = l.Close()
}

// Detach снимает все программы. Нужен для удаления продукта, не для перезапуска.
func (d *Dataplane) Detach() {
	entries, _ := os.ReadDir(filepath.Join(d.pinDir, "links"))
	for _, e := range entries {
		d.detachPin(filepath.Join(d.pinDir, "links", e.Name()))
	}
}

func (d *Dataplane) RuleCounters() map[string]RuleCounter {
	d.mu.Lock()
	ids := d.ruleIDs
	d.mu.Unlock()

	out := map[string]RuleCounter{}
	var per []bpf.RendolCounter
	for slot, id := range ids {
		if err := d.objs.RuleStats.Lookup(&slot, &per); err != nil {
			continue
		}
		var c RuleCounter
		for _, v := range per {
			c.Packets += v.Pkts
			c.Bytes += v.Bytes
		}
		out[id] = c
	}
	return out
}

// ResetRuleCounter обнуляет счётчик слота; вызывается, когда слот достаётся
// новому правилу.
func (d *Dataplane) ResetRuleCounter(slot uint32) {
	n, err := ebpf.PossibleCPU()
	if err != nil {
		return
	}
	zero := make([]bpf.RendolCounter, n)
	_ = d.objs.RuleStats.Put(&slot, zero)
}

func (d *Dataplane) Stats() Stats {
	d.mu.Lock()
	s := Stats{Sessions: d.live}
	d.mu.Unlock()
	var per []uint64
	vals := []*uint64{&s.XDPBlocklist, &s.XDPSynFlood, &s.PolicyDeny, &s.PolicyAllow, &s.CTHit,
		&s.CTNew, &s.NATFail, &s.FragPass, &s.RingbufFull, &s.RejectSent}
	for i, dst := range vals {
		idx := uint32(i)
		if err := d.objs.Stats.Lookup(&idx, &per); err != nil {
			continue
		}
		for _, v := range per {
			*dst += v
		}
	}
	return s
}

func monoNow() uint64 {
	var ts unix.Timespec
	_ = unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts)
	return uint64(ts.Sec)*1e9 + uint64(ts.Nsec)
}

func (d *Dataplane) natString(v *bpf.RendolCtVal) string {
	switch {
	case v.Flags&ctFlagSNAT != 0:
		return fmt.Sprintf("snat %s:%d", fromBE32(v.NatAddr), ntohs(v.NatPort))
	case v.Flags&ctFlagDNAT != 0:
		return fmt.Sprintf("dnat %s:%d", fromBE32(v.NatAddr), ntohs(v.NatPort))
	}
	return ""
}

func (d *Dataplane) ruleName(id uint32) string {
	switch id {
	case RuleIDDNAT:
		return "dnat"
	case RuleIDMgmt:
		return "management"
	case RuleIDDefault:
		return ""
	}
	return d.ruleIDs[id]
}

func (d *Dataplane) session(k *bpf.RendolCtKey, v *bpf.RendolCtVal, now uint64) Session {
	return Session{
		Proto: k.Proto, Src: fromAddr(k.Src), Dst: fromAddr(k.Dst),
		SrcPort: ntohs(k.Sport), DstPort: ntohs(k.Dport),
		State: stateNames[v.State], FromZone: d.zoneNames[v.FromZone], ToZone: d.zoneNames[v.ToZone],
		RuleID: d.ruleName(v.RuleId), NAT: d.natString(v),
		PktsOrig: v.PktsOrig, BytesOrig: v.BytesOrig, PktsReply: v.PktsReply, BytesReply: v.BytesReply,
		Age:  float64(now-v.Created) / 1e9,
		Idle: float64(now-v.LastSeen) / 1e9,
	}
}

// Sessions возвращает до limit активных сессий.
func (d *Dataplane) Sessions(limit int) []Session {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := monoNow()
	var k bpf.RendolCtKey
	var v bpf.RendolCtVal
	var out []Session
	it := d.objs.Ct.Iterate()
	for it.Next(&k, &v) {
		if now-v.LastSeen > uint64(stateTimeout[v.State]) {
			continue
		}
		out = append(out, d.session(&k, &v, now))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// GC удаляет просроченные сессии и освобождает их порты NAT. LRU-карта и
// сама вытеснит старое, но порты SNAT без этой уборки кончились бы раньше.
func (d *Dataplane) GC(onEnd func(Event)) (removed, total int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := monoNow()
	var k bpf.RendolCtKey
	var v bpf.RendolCtVal
	type item struct {
		k bpf.RendolCtKey
		v bpf.RendolCtVal
	}
	var dead []item
	it := d.objs.Ct.Iterate()
	for it.Next(&k, &v) {
		total++
		if now > v.LastSeen && now-v.LastSeen > uint64(stateTimeout[v.State]) {
			dead = append(dead, item{k, v})
		}
	}
	for _, x := range dead {
		if err := d.objs.Ct.Delete(&x.k); err != nil {
			continue
		}
		removed++
		if x.v.Flags&ctFlagSNAT != 0 {
			sk := bpf.RendolSnatKey{
				NatAddr: x.v.NatAddr, RemoteAddr: x.k.Dst.W[3],
				NatPort: x.v.NatPort, RemotePort: x.k.Dport, Proto: x.k.Proto,
			}
			_ = d.objs.SnatRev.Delete(&sk)
		}
		if onEnd != nil && x.v.Flags&ctFlagLog != 0 {
			s := d.session(&x.k, &x.v, now)
			onEnd(Event{
				Time: time.Now(), Type: EventSessionEnd, Action: "allow", Proto: s.Proto,
				Src: s.Src, Dst: s.Dst, SrcPort: s.SrcPort, DstPort: s.DstPort,
				FromZone: s.FromZone, ToZone: s.ToZone, RuleID: s.RuleID,
				Packets: x.v.PktsOrig + x.v.PktsReply, Bytes: x.v.BytesOrig + x.v.BytesReply, Count: 1,
			})
		}
	}
	d.live = total - removed
	return removed, total - removed
}

// FlushSessions сбрасывает все сессии, например по кнопке в интерфейсе.
func (d *Dataplane) FlushSessions() {
	d.mu.Lock()
	defer d.mu.Unlock()
	var k bpf.RendolCtKey
	var v bpf.RendolCtVal
	var keys []bpf.RendolCtKey
	it := d.objs.Ct.Iterate()
	for it.Next(&k, &v) {
		keys = append(keys, k)
	}
	for _, k := range keys {
		_ = d.objs.Ct.Delete(&k)
	}
	var sk bpf.RendolSnatKey
	var sv bpf.RendolSnatVal
	var skeys []bpf.RendolSnatKey
	it = d.objs.SnatRev.Iterate()
	for it.Next(&sk, &sv) {
		skeys = append(skeys, sk)
	}
	for _, k := range skeys {
		_ = d.objs.SnatRev.Delete(&k)
	}
}

// Events читает кольцевой буфер до закрытия dataplane.
func (d *Dataplane) Events(fn func(Event)) error {
	rd, err := ringbuf.NewReader(d.objs.Events)
	if err != nil {
		return fmt.Errorf("dataplane: ringbuf: %w", err)
	}
	defer func() { _ = rd.Close() }()
	ifNames := map[uint32]string{}
	for {
		rec, err := rd.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return nil
			}
			return err
		}
		var e bpf.RendolEvent
		if err := decodeEvent(rec.RawSample, &e); err != nil {
			continue
		}
		name, ok := ifNames[e.Ifindex]
		if !ok {
			if it, err := net.InterfaceByIndex(int(e.Ifindex)); err == nil {
				name = it.Name
			}
			ifNames[e.Ifindex] = name
		}
		d.mu.Lock()
		ev := Event{
			Time: time.Now(), Proto: e.Proto, Src: fromAddr(e.Src), Dst: fromAddr(e.Dst),
			SrcPort: ntohs(e.Sport), DstPort: ntohs(e.Dport),
			FromZone: d.zoneNames[e.FromZone], ToZone: d.zoneNames[e.ToZone],
			RuleID: d.ruleName(e.RuleId), Iface: name, Packets: e.Pkts, Bytes: e.Bytes, Count: 1,
		}
		d.mu.Unlock()
		switch e.Type {
		case 1:
			ev.Type = EventDeny
			ev.Action = "deny"
			if e.Action == ActReject {
				ev.Action = "reject"
			}
		case 2:
			ev.Type = EventAllow
			ev.Action = "allow"
		default:
			continue
		}
		fn(ev)
	}
}

func (d *Dataplane) XDPMode() string { return d.xdpMode }

func (d *Dataplane) Generation() uint32 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.gen
}

// ensureBPFFS монтирует bpffs, если её нет: systemd в Debian делает это сам,
// а в контейнерах и минимальных системах — не всегда.
func ensureBPFFS(dir string) error {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err == nil && st.Type == unix.BPF_FS_MAGIC {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("dataplane: каталог bpffs: %w", err)
	}
	if err := unix.Mount("bpf", dir, "bpf", 0, "mode=0700"); err != nil {
		return fmt.Errorf("dataplane: монтирование bpffs в %s: %w", dir, err)
	}
	return nil
}
