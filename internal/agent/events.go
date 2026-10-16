package agent

import (
	"sync"
	"time"

	"rendolnet/internal/dataplane"
)

type SeqEvent struct {
	Seq uint64 `json:"seq"`
	dataplane.Event
}

// eventRing держит последние события в памяти, пока их не заберёт rendol-api.
// Одинаковые отбои в пределах секунды склеиваются: сканирование портов
// иначе вытесняет из буфера всё остальное за доли секунды.
type eventRing struct {
	mu   sync.Mutex
	buf  []SeqEvent
	size int
	next uint64
	idx  map[aggKey]int
}

type aggKey struct {
	typ      dataplane.EventType
	src, dst string
	dport    uint16
	proto    uint8
	rule     string
	sec      int64
}

func newEventRing(size int) *eventRing {
	return &eventRing{size: size, next: 1, idx: map[aggKey]int{}}
}

func (r *eventRing) add(e dataplane.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.Type != dataplane.EventSessionEnd {
		k := aggKey{e.Type, e.Src.String(), e.Dst.String(), e.DstPort, e.Proto, e.RuleID, e.Time.Unix()}
		if i, ok := r.idx[k]; ok && i < len(r.buf) {
			r.buf[i].Count++
			r.buf[i].Packets += e.Packets
			r.buf[i].Bytes += e.Bytes
			return
		}
		if len(r.idx) > 4096 {
			r.idx = map[aggKey]int{}
		}
		defer func() { r.idx[k] = len(r.buf) - 1 }()
	}
	if len(r.buf) >= r.size {
		drop := r.size / 4
		r.buf = append(r.buf[:0], r.buf[drop:]...)
		r.idx = map[aggKey]int{}
	}
	r.buf = append(r.buf, SeqEvent{Seq: r.next, Event: e})
	r.next++
}

// Since возвращает события с номером больше seq. Склеенное событие может
// ещё дорасти, но через секунду оно закрыто: его ключ больше не совпадёт.
func (r *eventRing) Since(seq uint64, limit int) []SeqEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	// курсор из будущего — значит, агент перезапустился и считает заново
	if seq >= r.next {
		seq = 0
	}
	cutoff := time.Now().Add(-time.Second)
	var out []SeqEvent
	for _, e := range r.buf {
		if e.Seq <= seq {
			continue
		}
		if e.Type != dataplane.EventSessionEnd && e.Time.After(cutoff) {
			break
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
