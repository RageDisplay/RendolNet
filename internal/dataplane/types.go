package dataplane

import (
	"net/netip"
	"time"
)

type Session struct {
	Proto      uint8      `json:"proto"`
	Src        netip.Addr `json:"src"`
	Dst        netip.Addr `json:"dst"`
	SrcPort    uint16     `json:"sport"`
	DstPort    uint16     `json:"dport"`
	State      string     `json:"state"`
	FromZone   string     `json:"from_zone"`
	ToZone     string     `json:"to_zone"`
	RuleID     string     `json:"rule_id,omitempty"`
	NAT        string     `json:"nat,omitempty"`
	PktsOrig   uint64     `json:"pkts_orig"`
	BytesOrig  uint64     `json:"bytes_orig"`
	PktsReply  uint64     `json:"pkts_reply"`
	BytesReply uint64     `json:"bytes_reply"`
	Age        float64    `json:"age_s"`
	Idle       float64    `json:"idle_s"`
}

type EventType string

const (
	EventDeny       EventType = "deny"
	EventAllow      EventType = "allow"
	EventSessionEnd EventType = "session_end"
)

type Event struct {
	Time     time.Time  `json:"time"`
	Type     EventType  `json:"type"`
	Action   string     `json:"action"`
	Proto    uint8      `json:"proto"`
	Src      netip.Addr `json:"src"`
	Dst      netip.Addr `json:"dst"`
	SrcPort  uint16     `json:"sport"`
	DstPort  uint16     `json:"dport"`
	FromZone string     `json:"from_zone"`
	ToZone   string     `json:"to_zone"`
	RuleID   string     `json:"rule_id,omitempty"`
	Iface    string     `json:"iface,omitempty"`
	Packets  uint64     `json:"packets"`
	Bytes    uint64     `json:"bytes"`
	Count    int        `json:"count"`
}

type RuleCounter struct {
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

type Stats struct {
	XDPBlocklist uint64 `json:"xdp_blocklist"`
	XDPSynFlood  uint64 `json:"xdp_synflood"`
	PolicyDeny   uint64 `json:"policy_deny"`
	PolicyAllow  uint64 `json:"policy_allow"`
	CTHit        uint64 `json:"ct_hit"`
	CTNew        uint64 `json:"ct_new"`
	NATFail      uint64 `json:"nat_fail"`
	FragPass     uint64 `json:"frag_pass"`
	RingbufFull  uint64 `json:"ringbuf_full"`
	RejectSent   uint64 `json:"reject_sent"`
	Sessions     int    `json:"sessions"`
}

var stateNames = map[uint8]string{
	1: "syn_sent", 2: "established", 3: "fin_wait", 4: "close", 5: "udp", 6: "icmp", 7: "other",
}

var stateTimeout = map[uint8]time.Duration{
	1: 60 * time.Second, 2: time.Hour, 3: 120 * time.Second, 4: 10 * time.Second,
	5: 120 * time.Second, 6: 30 * time.Second, 7: 60 * time.Second,
}

const (
	ctFlagDNAT     = 0x01
	ctFlagSNAT     = 0x02
	ctFlagLocalOut = 0x04
	ctFlagLog      = 0x10
)
