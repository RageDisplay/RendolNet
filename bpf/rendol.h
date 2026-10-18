#pragma once

#include <linux/types.h>

#define MAX_RULES      4096
#define MAX_SNAT_RULES 64
#define MAX_MGMT_PORTS 8

#define ZONE_SELF 0

#define ROLE_WAN  1
#define ROLE_LAN  2
#define ROLE_MGMT 3

#define ACT_ALLOW  1
#define ACT_DENY   2
#define ACT_REJECT 3

#define RULE_F_LOG 1

#define CT_SYN_SENT 1
#define CT_EST      2
#define CT_FIN      3
#define CT_CLOSE    4
#define CT_UDP      5
#define CT_ICMP     6
#define CT_OTHER    7

#define CT_F_DNAT      0x01
#define CT_F_SNAT      0x02
#define CT_F_LOCAL_OUT 0x04
#define CT_F_REPLIED   0x08
#define CT_F_LOG       0x10

/* бит отметки пакета, прошедшего ingress; остальные биты mark не трогаем —
 * ими пользуются kube-proxy и flannel */
#define RN_MARK 0x10000000
#define RN_PACKET_OTHERHOST 3 /* PACKET_OTHERHOST из linux/if_packet.h */

#define EV_DENY        1
#define EV_ALLOW       2
#define EV_SESSION_END 3

/* правило из DNAT с автоматическим разрешением и правило доступа управления */
#define RULE_ID_DNAT 0xfffffffe
#define RULE_ID_MGMT 0xfffffffd
#define RULE_ID_DEFAULT 0xffffffff

enum stat_idx {
	ST_XDP_BLOCKLIST,
	ST_XDP_SYNFLOOD,
	ST_POLICY_DENY,
	ST_POLICY_ALLOW,
	ST_CT_HIT,
	ST_CT_NEW,
	ST_NAT_FAIL,
	ST_FRAG_PASS,
	ST_RINGBUF_FULL,
	ST_REJECT_SENT,
	ST_MAX,
};

/* адреса IPv4 хранятся как ::ffff:a.b.c.d, всё в сетевом порядке байт */
struct addr {
	__u32 w[4];
};

struct ct_key {
	struct addr src;
	struct addr dst;
	__u16 sport;
	__u16 dport;
	__u8 proto;
	__u8 pad[3];
};

struct ct_val {
	__u64 created;
	__u64 last_seen;
	__u64 pkts_orig;
	__u64 bytes_orig;
	__u64 pkts_reply;
	__u64 bytes_reply;
	__u32 rule_id;
	__u32 gen;
	__u32 in_ifindex;
	__u32 nat_addr;
	__u16 nat_port;
	__u16 from_zone;
	__u16 to_zone;
	__u8 state;
	__u8 flags;
};

struct snat_key {
	__u32 nat_addr;
	__u32 remote_addr;
	__u16 nat_port;
	__u16 remote_port;
	__u8 proto;
	__u8 pad[3];
};

struct snat_val {
	__u32 orig_addr;
	__u16 orig_port;
	__u16 pad;
};

struct iface {
	__u32 addr4;
	__u16 zone;
	__u8 role;
	__u8 flags;
};

struct rule {
	__u32 src_set;
	__u32 dst_set;
	__u32 rule_id;
	__u16 dport_lo;
	__u16 dport_hi;
	__u8 proto;
	__u8 action;
	__u8 flags;
	__u8 pad;
};

struct pair_key {
	__u32 bank;
	__u16 from;
	__u16 to;
};

struct pair_val {
	__u32 start;
	__u32 count;
};

struct set_key {
	__u32 prefixlen;
	__u32 set;
	struct addr a;
};

struct block_key {
	__u32 prefixlen;
	struct addr a;
};

struct dnat_key {
	__u32 ifindex;
	__u16 port;
	__u8 proto;
	__u8 pad;
};

struct dnat_val {
	__u32 to_addr;
	__u16 to_port;
	__u8 allow;
	__u8 pad;
	__u32 rule_id;
};

struct snat_rule {
	__u32 out_ifindex;
	__u32 src_set;
	__u32 to_addr;
	__u32 rule_id;
};

struct bank_cfg {
	__u32 gen;
	__u32 snat_count;
	__u32 mgmt_ifindex;
	__u32 mgmt_set;
	__u16 mgmt_ports[MAX_MGMT_PORTS];
	__u32 syn_rate;
	__u32 syn_burst;
	__u32 log_default;
};

struct syn_bucket {
	__u64 tokens;
	__u64 last;
};

struct counter {
	__u64 pkts;
	__u64 bytes;
};

struct event {
	__u64 ts;
	struct addr src;
	struct addr dst;
	__u64 pkts;
	__u64 bytes;
	__u32 rule_id;
	__u32 ifindex;
	__u16 sport;
	__u16 dport;
	__u16 from_zone;
	__u16 to_zone;
	__u8 type;
	__u8 proto;
	__u8 action;
	__u8 pad;
};
