// SPDX-License-Identifier: GPL-2.0
#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/in.h>
#include <linux/ip.h>
#include <linux/ipv6.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <linux/pkt_cls.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

#include "rendol.h"

/* linux/icmp.h тянет за собой libc-заголовки, поэтому константы свои */
#define ICMP_ECHOREPLY      0
#define ICMP_DEST_UNREACH   3
#define ICMP_ECHO           8
#define ICMP_TIME_EXCEEDED  11
#define ICMP_PARAMETERPROB  12
#define ICMPV6_ECHO_REQUEST 128
#define ICMPV6_ECHO_REPLY   129

char LICENSE[] SEC("license") = "GPL";

/* ---- карты состояния: закреплены, переживают перезапуск агента ---- */

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 262144);
	__type(key, struct ct_key);
	__type(value, struct ct_val);
	__uint(pinning, LIBBPF_PIN_BY_NAME);
} ct SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 131072);
	__type(key, struct snat_key);
	__type(value, struct snat_val);
	__uint(pinning, LIBBPF_PIN_BY_NAME);
} snat_rev SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 65536);
	__type(key, struct addr);
	__type(value, struct syn_bucket);
} syn_rl SEC(".maps");

/* ---- конфигурация: пишет агент ---- */

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, __u32);
} active SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 2);
	__type(key, __u32);
	__type(value, struct bank_cfg);
} bankcfg SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 256);
	__type(key, __u32);
	__type(value, struct iface);
} ifaces SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 2 * MAX_RULES);
	__type(key, __u32);
	__type(value, struct rule);
} rules SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 2 * 1024);
	__type(key, struct pair_key);
	__type(value, struct pair_val);
} pairs SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LPM_TRIE);
	__uint(max_entries, 131072);
	__uint(map_flags, BPF_F_NO_PREALLOC);
	__type(key, struct set_key);
	__type(value, __u8);
} addr_sets SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LPM_TRIE);
	__uint(max_entries, 262144);
	__uint(map_flags, BPF_F_NO_PREALLOC);
	__type(key, struct block_key);
	__type(value, __u8);
} blocklist SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 2 * 1024);
	__type(key, struct dnat_key);
	__type(value, struct dnat_val);
} dnat SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 2 * MAX_SNAT_RULES);
	__type(key, __u32);
	__type(value, struct snat_rule);
} snat_rules SEC(".maps");

/* ---- телеметрия ---- */

struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, MAX_RULES);
	__type(key, __u32);
	__type(value, struct counter);
} rule_stats SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__uint(max_entries, ST_MAX);
	__type(key, __u32);
	__type(value, __u64);
} stats SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 22);
} events SEC(".maps");

/* чтобы bpf2go вывел типы, которые читает только агент */
const struct event *unused_event __attribute__((unused));
const struct ct_val *unused_ct_val __attribute__((unused));

static __always_inline void stat_inc(__u32 idx)
{
	__u64 *v = bpf_map_lookup_elem(&stats, &idx);
	if (v)
		*v += 1;
}

static __always_inline void set_v4(struct addr *a, __u32 ip)
{
	a->w[0] = 0;
	a->w[1] = 0;
	a->w[2] = bpf_htonl(0xffff);
	a->w[3] = ip;
}

/* ================================ XDP ================================ */

SEC("xdp")
int xdp_guard(struct xdp_md *ctx)
{
	void *data = (void *)(long)ctx->data;
	void *end = (void *)(long)ctx->data_end;
	struct ethhdr *eth = data;
	struct addr src = {};
	__u8 proto = 0;
	void *l4;

	if ((void *)(eth + 1) > end)
		return XDP_PASS;

	__u16 h_proto = eth->h_proto;
	void *l3 = eth + 1;
	if (h_proto == bpf_htons(ETH_P_8021Q) || h_proto == bpf_htons(ETH_P_8021AD)) {
		struct { __u16 tci; __u16 proto; } *vh = l3;
		if ((void *)(vh + 1) > end)
			return XDP_PASS;
		h_proto = vh->proto;
		l3 = vh + 1;
	}

	if (h_proto == bpf_htons(ETH_P_IP)) {
		struct iphdr *ip = l3;
		if ((void *)(ip + 1) > end)
			return XDP_PASS;
		set_v4(&src, ip->saddr);
		proto = ip->protocol;
		if (ip->frag_off & bpf_htons(0x1fff))
			proto = 0;
		l4 = (void *)ip + ip->ihl * 4;
	} else if (h_proto == bpf_htons(ETH_P_IPV6)) {
		struct ipv6hdr *ip6 = l3;
		if ((void *)(ip6 + 1) > end)
			return XDP_PASS;
		__builtin_memcpy(&src, &ip6->saddr, sizeof(src));
		proto = ip6->nexthdr;
		l4 = ip6 + 1;
	} else {
		return XDP_PASS;
	}

	struct block_key bk = { .prefixlen = 128, .a = src };
	if (bpf_map_lookup_elem(&blocklist, &bk)) {
		stat_inc(ST_XDP_BLOCKLIST);
		return XDP_DROP;
	}

	if (proto != IPPROTO_TCP)
		return XDP_PASS;
	struct tcphdr *th = l4;
	if ((void *)(th + 1) > end)
		return XDP_PASS;
	if (!th->syn || th->ack)
		return XDP_PASS;

	__u32 zero = 0;
	__u32 *gen = bpf_map_lookup_elem(&active, &zero);
	if (!gen)
		return XDP_PASS;
	__u32 bank = *gen & 1;
	struct bank_cfg *cfg = bpf_map_lookup_elem(&bankcfg, &bank);
	if (!cfg || !cfg->syn_rate)
		return XDP_PASS;

	const __u64 one = 1000000000ULL;
	__u64 now = bpf_ktime_get_ns();
	__u64 cap = (__u64)(cfg->syn_burst ? cfg->syn_burst : cfg->syn_rate) * one;
	struct syn_bucket *b = bpf_map_lookup_elem(&syn_rl, &src);
	if (!b) {
		struct syn_bucket nb = { .tokens = cap - one, .last = now };
		bpf_map_update_elem(&syn_rl, &src, &nb, BPF_ANY);
		return XDP_PASS;
	}
	__u64 elapsed = now - b->last;
	if (elapsed > 10 * one)
		elapsed = 10 * one;
	__u64 tokens = b->tokens + elapsed * cfg->syn_rate;
	if (tokens > cap)
		tokens = cap;
	b->last = now;
	if (tokens < one) {
		b->tokens = tokens;
		stat_inc(ST_XDP_SYNFLOOD);
		return XDP_DROP;
	}
	b->tokens = tokens - one;
	return XDP_PASS;
}

/* ================================ TC ================================ */

struct pkt {
	struct addr src;
	struct addr dst;
	__u32 l3_off;
	__u32 l4_off;
	__u32 len;
	__u16 sport;
	__u16 dport;
	__u8 v4;
	__u8 proto;
	__u8 tcp_flags;
	__u8 icmp_type;
	__u8 ok;
	__u8 frag;
	__u8 ihl_ok;
};

#define TH_FIN 0x01
#define TH_SYN 0x02
#define TH_RST 0x04
#define TH_ACK 0x10

static __always_inline int parse(struct __sk_buff *skb, struct pkt *p)
{
	__builtin_memset(p, 0, sizeof(*p));
	p->l3_off = ETH_HLEN;
	p->len = skb->len;
	__u8 proto;

	if (skb->protocol == bpf_htons(ETH_P_IP)) {
		struct iphdr ip;
		if (bpf_skb_load_bytes(skb, ETH_HLEN, &ip, sizeof(ip)) < 0)
			return -1;
		if (ip.ihl < 5)
			return -1;
		p->v4 = 1;
		p->ihl_ok = ip.ihl == 5;
		set_v4(&p->src, ip.saddr);
		set_v4(&p->dst, ip.daddr);
		proto = ip.protocol;
		p->l4_off = ETH_HLEN + ip.ihl * 4;
		if (ip.frag_off & bpf_htons(0x1fff))
			p->frag = 1;
	} else if (skb->protocol == bpf_htons(ETH_P_IPV6)) {
		struct ipv6hdr ip6;
		if (bpf_skb_load_bytes(skb, ETH_HLEN, &ip6, sizeof(ip6)) < 0)
			return -1;
		__builtin_memcpy(&p->src, &ip6.saddr, sizeof(p->src));
		__builtin_memcpy(&p->dst, &ip6.daddr, sizeof(p->dst));
		proto = ip6.nexthdr;
		p->l4_off = ETH_HLEN + sizeof(ip6);
		p->ihl_ok = 1;
#pragma unroll
		for (int i = 0; i < 4; i++) {
			if (proto != 0 && proto != 43 && proto != 44 && proto != 60)
				break;
			struct { __u8 nh; __u8 len; __u16 frag; } ext;
			if (bpf_skb_load_bytes(skb, p->l4_off, &ext, sizeof(ext)) < 0)
				return -1;
			p->ihl_ok = 0;
			if (proto == 44) {
				if (ext.frag & bpf_htons(0xfff8))
					p->frag = 1;
				p->l4_off += 8;
			} else {
				p->l4_off += (ext.len + 1) * 8;
			}
			proto = ext.nh;
		}
	} else {
		return -1;
	}
	p->proto = proto;
	if (p->frag)
		return 0;

	if (proto == IPPROTO_TCP) {
		struct tcphdr th;
		if (bpf_skb_load_bytes(skb, p->l4_off, &th, sizeof(th)) < 0)
			return -1;
		p->sport = th.source;
		p->dport = th.dest;
		p->tcp_flags = ((__u8 *)&th)[13];
	} else if (proto == IPPROTO_UDP) {
		struct udphdr uh;
		if (bpf_skb_load_bytes(skb, p->l4_off, &uh, sizeof(uh)) < 0)
			return -1;
		p->sport = uh.source;
		p->dport = uh.dest;
	} else if (proto == IPPROTO_ICMP || proto == IPPROTO_ICMPV6) {
		struct { __u8 type; __u8 code; __u16 csum; __u16 id; __u16 seq; } ih;
		if (bpf_skb_load_bytes(skb, p->l4_off, &ih, sizeof(ih)) < 0)
			return -1;
		p->icmp_type = ih.type;
		/* эхо-запрос и ответ сводим к одному кортежу: id играет роль порта */
		if ((proto == IPPROTO_ICMP && ih.type == ICMP_ECHO) ||
		    (proto == IPPROTO_ICMPV6 && ih.type == ICMPV6_ECHO_REQUEST))
			p->sport = ih.id;
		else if ((proto == IPPROTO_ICMP && ih.type == ICMP_ECHOREPLY) ||
			 (proto == IPPROTO_ICMPV6 && ih.type == ICMPV6_ECHO_REPLY))
			p->dport = ih.id;
	}
	p->ok = 1;
	return 0;
}

static __always_inline void make_key(const struct pkt *p, struct ct_key *k, int reverse)
{
	__builtin_memset(k, 0, sizeof(*k));
	if (!reverse) {
		k->src = p->src;
		k->dst = p->dst;
		k->sport = p->sport;
		k->dport = p->dport;
	} else {
		k->src = p->dst;
		k->dst = p->src;
		k->sport = p->dport;
		k->dport = p->sport;
	}
	k->proto = p->proto;
}

static __always_inline __u64 ct_timeout(__u8 state)
{
	const __u64 s = 1000000000ULL;
	switch (state) {
	case CT_SYN_SENT: return 60 * s;
	case CT_EST:      return 3600 * s;
	case CT_FIN:      return 120 * s;
	case CT_CLOSE:    return 10 * s;
	case CT_UDP:      return 120 * s;
	case CT_ICMP:     return 30 * s;
	default:          return 60 * s;
	}
}

static __always_inline int csum_off(__u8 proto)
{
	switch (proto) {
	case IPPROTO_TCP:  return 16;
	case IPPROTO_UDP:  return 6;
	case IPPROTO_ICMP: return 2;
	}
	return -1;
}

/* Подмена адреса IPv4: is_src — источник или назначение. */
static __always_inline int rw_addr4(struct __sk_buff *skb, const struct pkt *p, int is_src, __u32 old, __u32 new)
{
	int co = csum_off(p->proto);
	if (co >= 0 && p->proto != IPPROTO_ICMP) {
		__u64 fl = BPF_F_PSEUDO_HDR | sizeof(new);
		if (p->proto == IPPROTO_UDP)
			fl |= BPF_F_MARK_MANGLED_0;
		if (bpf_l4_csum_replace(skb, p->l4_off + co, old, new, fl) < 0)
			return -1;
	}
	if (bpf_l3_csum_replace(skb, p->l3_off + 10, old, new, sizeof(new)) < 0)
		return -1;
	return bpf_skb_store_bytes(skb, p->l3_off + (is_src ? 12 : 16), &new, sizeof(new), 0);
}

/* Подмена порта; для ICMP-эха — идентификатора. */
static __always_inline int rw_port(struct __sk_buff *skb, const struct pkt *p, int is_src, __u16 old, __u16 new)
{
	if (old == new)
		return 0;
	int co = csum_off(p->proto);
	if (co < 0)
		return -1;
	__u32 off;
	__u64 fl = sizeof(new);
	if (p->proto == IPPROTO_ICMP) {
		off = p->l4_off + 4;
	} else {
		off = p->l4_off + (is_src ? 0 : 2);
		if (p->proto == IPPROTO_UDP)
			fl |= BPF_F_MARK_MANGLED_0;
	}
	if (bpf_l4_csum_replace(skb, p->l4_off + co, old, new, fl) < 0)
		return -1;
	return bpf_skb_store_bytes(skb, off, &new, sizeof(new), 0);
}

static __always_inline int in_set(__u32 set, const struct addr *a)
{
	struct set_key k = { .prefixlen = 32 + 128, .set = set, .a = *a };
	return bpf_map_lookup_elem(&addr_sets, &k) != 0;
}

struct eval_ctx {
	struct addr src;
	struct addr dst;
	__u32 base;
	__u32 match;
	__u16 dport;
	__u8 proto;
	__u8 found;
};

static long eval_rule(__u32 i, void *data)
{
	struct eval_ctx *c = data;
	__u32 idx = c->base + i;
	struct rule *r = bpf_map_lookup_elem(&rules, &idx);
	if (!r)
		return 1;
	if (r->proto && r->proto != c->proto)
		return 0;
	if (r->dport_hi) {
		__u16 port = bpf_ntohs(c->dport);
		if (port < r->dport_lo || port > r->dport_hi)
			return 0;
	}
	if (r->src_set && !in_set(r->src_set, &c->src))
		return 0;
	if (r->dst_set && !in_set(r->dst_set, &c->dst))
		return 0;
	c->match = idx;
	c->found = 1;
	return 1;
}

struct verdict {
	__u32 rule_id;
	__u8 action;
	__u8 flags;
};

static __always_inline void policy(__u32 bank, __u16 from, __u16 to, const struct pkt *p, struct verdict *v)
{
	v->rule_id = RULE_ID_DEFAULT;
	v->action = ACT_DENY;
	v->flags = 0;

	struct pair_key pk = { .bank = bank, .from = from, .to = to };
	struct pair_val *pv = bpf_map_lookup_elem(&pairs, &pk);
	if (!pv || !pv->count)
		return;
	struct eval_ctx c = {
		.src = p->src, .dst = p->dst, .base = pv->start,
		.dport = p->dport, .proto = p->proto,
	};
	__u32 n = pv->count;
	if (n > MAX_RULES)
		n = MAX_RULES;
	bpf_loop(n, eval_rule, &c, 0);
	if (!c.found)
		return;
	__u32 idx = c.match;
	struct rule *r = bpf_map_lookup_elem(&rules, &idx);
	if (!r)
		return;
	v->rule_id = r->rule_id;
	v->action = r->action;
	v->flags = r->flags;

	__u32 sid = r->rule_id;
	struct counter *cnt = bpf_map_lookup_elem(&rule_stats, &sid);
	if (cnt) {
		cnt->pkts += 1;
		cnt->bytes += p->len;
	}
}

static __always_inline int mgmt_allowed(struct bank_cfg *cfg, struct __sk_buff *skb, const struct pkt *p)
{
	if (!cfg->mgmt_ifindex || skb->ifindex != cfg->mgmt_ifindex || p->proto != IPPROTO_TCP)
		return 0;
	if (cfg->mgmt_set && !in_set(cfg->mgmt_set, &p->src))
		return 0;
	__u16 port = bpf_ntohs(p->dport);
#pragma unroll
	for (int i = 0; i < MAX_MGMT_PORTS; i++)
		if (cfg->mgmt_ports[i] && cfg->mgmt_ports[i] == port)
			return 1;
	return 0;
}

static __always_inline __u16 egress_zone(struct __sk_buff *skb, const struct pkt *p, int *local)
{
	struct bpf_fib_lookup f = {};
	f.l4_protocol = p->proto;
	f.sport = p->sport;
	f.dport = p->dport;
	f.tot_len = p->len - ETH_HLEN;
	f.ifindex = skb->ifindex;
	if (p->v4) {
		f.family = 2; /* AF_INET */
		f.ipv4_src = p->src.w[3];
		f.ipv4_dst = p->dst.w[3];
	} else {
		f.family = 10; /* AF_INET6 */
		__builtin_memcpy(f.ipv6_src, &p->src, 16);
		__builtin_memcpy(f.ipv6_dst, &p->dst, 16);
	}
	long rc = bpf_fib_lookup(skb, &f, sizeof(f), 0);
	*local = 0;
	if (rc == BPF_FIB_LKUP_RET_NOT_FWDED) {
		*local = 1;
		return ZONE_SELF;
	}
	if (rc != BPF_FIB_LKUP_RET_SUCCESS && rc != BPF_FIB_LKUP_RET_NO_NEIGH && rc != BPF_FIB_LKUP_RET_FRAG_NEEDED)
		return ZONE_SELF;
	__u32 oif = f.ifindex;
	struct iface *ifc = bpf_map_lookup_elem(&ifaces, &oif);
	/* интерфейсы, к которым мы не подключены (cni0, flannel), — это сам узел */
	if (!ifc)
		return ZONE_SELF;
	return ifc->zone;
}

static __always_inline void emit(__u8 type, const struct pkt *p, struct __sk_buff *skb, __u16 from, __u16 to, __u32 rule_id, __u8 action)
{
	struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
	if (!e) {
		stat_inc(ST_RINGBUF_FULL);
		return;
	}
	e->ts = bpf_ktime_get_ns();
	e->src = p->src;
	e->dst = p->dst;
	e->pkts = 1;
	e->bytes = p->len;
	e->rule_id = rule_id;
	e->ifindex = skb->ifindex;
	e->sport = p->sport;
	e->dport = p->dport;
	e->from_zone = from;
	e->to_zone = to;
	e->type = type;
	e->proto = p->proto;
	e->action = action;
	e->pad = 0;
	bpf_ringbuf_submit(e, 0);
}

static __always_inline __u16 csum_fold(__u64 s)
{
#pragma unroll
	for (int i = 0; i < 4; i++)
		s = (s & 0xffff) + (s >> 16);
	return ~s;
}

/* Отказ с TCP RST: пакет разворачивается на месте и уходит обратно в тот же
 * интерфейс. Только для заголовков без опций IP — иначе просто сброс. */
static __always_inline int send_rst(struct __sk_buff *skb, const struct pkt *p)
{
	if (p->proto != IPPROTO_TCP || !p->ihl_ok || (p->tcp_flags & TH_RST))
		return -1;

	struct ethhdr eth;
	struct tcphdr th;
	if (bpf_skb_load_bytes(skb, 0, &eth, sizeof(eth)) < 0)
		return -1;
	if (bpf_skb_load_bytes(skb, p->l4_off, &th, sizeof(th)) < 0)
		return -1;

	__u32 payload = p->len - p->l4_off - th.doff * 4;
	__u32 seq = bpf_ntohl(th.seq);
	__u32 ack_in = th.ack_seq;
	__u8 fl = p->tcp_flags;

	if (bpf_skb_change_tail(skb, p->l4_off + sizeof(th), 0) < 0)
		return -1;

	__u8 mac[ETH_ALEN];
	__builtin_memcpy(mac, eth.h_source, ETH_ALEN);
	__builtin_memcpy(eth.h_source, eth.h_dest, ETH_ALEN);
	__builtin_memcpy(eth.h_dest, mac, ETH_ALEN);

	__u16 tmp = th.source;
	th.source = th.dest;
	th.dest = tmp;
	__builtin_memset(((__u8 *)&th) + 12, 0, 2);
	th.doff = 5;
	th.rst = 1;
	if (fl & TH_ACK) {
		th.seq = ack_in;
		th.ack_seq = 0;
	} else {
		__u32 ack = seq + payload + ((fl & TH_SYN) ? 1 : 0) + ((fl & TH_FIN) ? 1 : 0);
		th.seq = 0;
		th.ack_seq = bpf_htonl(ack);
		th.ack = 1;
	}
	th.window = 0;
	th.check = 0;
	th.urg_ptr = 0;

	__u64 sum;
	if (p->v4) {
		struct iphdr ip;
		if (bpf_skb_load_bytes(skb, p->l3_off, &ip, sizeof(ip)) < 0)
			return -1;
		__u32 a = ip.saddr;
		ip.saddr = ip.daddr;
		ip.daddr = a;
		ip.tot_len = bpf_htons(sizeof(ip) + sizeof(th));
		ip.ttl = 64;
		ip.frag_off = 0;
		ip.check = 0;
		ip.check = csum_fold(bpf_csum_diff(0, 0, (__be32 *)&ip, sizeof(ip), 0));

		struct { __u32 s, d; __u8 z, proto; __u16 len; } ph = {
			.s = ip.saddr, .d = ip.daddr, .z = 0, .proto = IPPROTO_TCP,
			.len = bpf_htons(sizeof(th)),
		};
		sum = bpf_csum_diff(0, 0, (__be32 *)&ph, sizeof(ph), 0);
		sum = bpf_csum_diff(0, 0, (__be32 *)&th, sizeof(th), sum);
		th.check = csum_fold(sum);
		if (bpf_skb_store_bytes(skb, p->l3_off, &ip, sizeof(ip), 0) < 0)
			return -1;
	} else {
		struct ipv6hdr ip6;
		if (bpf_skb_load_bytes(skb, p->l3_off, &ip6, sizeof(ip6)) < 0)
			return -1;
		struct in6_addr a = ip6.saddr;
		ip6.saddr = ip6.daddr;
		ip6.daddr = a;
		ip6.payload_len = bpf_htons(sizeof(th));
		ip6.hop_limit = 64;

		struct { struct in6_addr s, d; __u32 len; __u8 z[3]; __u8 nh; } ph = {};
		ph.s = ip6.saddr;
		ph.d = ip6.daddr;
		ph.len = bpf_htonl(sizeof(th));
		ph.nh = IPPROTO_TCP;
		sum = bpf_csum_diff(0, 0, (__be32 *)&ph, sizeof(ph), 0);
		sum = bpf_csum_diff(0, 0, (__be32 *)&th, sizeof(th), sum);
		th.check = csum_fold(sum);
		if (bpf_skb_store_bytes(skb, p->l3_off, &ip6, sizeof(ip6), 0) < 0)
			return -1;
	}
	if (bpf_skb_store_bytes(skb, 0, &eth, sizeof(eth), 0) < 0)
		return -1;
	if (bpf_skb_store_bytes(skb, p->l4_off, &th, sizeof(th), 0) < 0)
		return -1;
	stat_inc(ST_REJECT_SENT);
	return bpf_redirect(skb->ifindex, 0);
}

static __always_inline void ct_update(struct ct_val *v, const struct pkt *p, int reply)
{
	__u64 now = bpf_ktime_get_ns();
	v->last_seen = now;
	if (reply) {
		__sync_fetch_and_add(&v->pkts_reply, 1);
		__sync_fetch_and_add(&v->bytes_reply, p->len);
		v->flags |= CT_F_REPLIED;
	} else {
		__sync_fetch_and_add(&v->pkts_orig, 1);
		__sync_fetch_and_add(&v->bytes_orig, p->len);
	}
	if (p->proto != IPPROTO_TCP)
		return;
	if (p->tcp_flags & TH_RST)
		v->state = CT_CLOSE;
	else if (p->tcp_flags & TH_FIN)
		v->state = CT_FIN;
	else if (v->state == CT_SYN_SENT && !reply && (v->flags & CT_F_REPLIED) && (p->tcp_flags & TH_ACK))
		v->state = CT_EST;
}

static __always_inline int is_icmp_error(const struct pkt *p)
{
	if (p->proto == IPPROTO_ICMP)
		return p->icmp_type == ICMP_DEST_UNREACH || p->icmp_type == ICMP_TIME_EXCEEDED ||
		       p->icmp_type == ICMP_PARAMETERPROB;
	if (p->proto == IPPROTO_ICMPV6)
		return p->icmp_type >= 1 && p->icmp_type <= 4;
	return 0;
}

/* соседство IPv6 (RS/RA/NS/NA/Redirect) нужно сети, а не политике */
static __always_inline int is_nd(const struct pkt *p)
{
	return p->proto == IPPROTO_ICMPV6 && p->icmp_type >= 133 && p->icmp_type <= 137;
}

/* Групповой и широковещательный трафик (mDNS, SSDP, IGMP, соседство IPv6)
 * отбрасывается молча: иначе он забивает журнал отбоев по умолчанию. */
static __always_inline int is_noise(const struct pkt *p)
{
	if (p->v4) {
		__u32 d = bpf_ntohl(p->dst.w[3]);
		return (d >> 28) == 0xe || d == 0xffffffff || (d & 0xff) == 0xff;
	}
	return (bpf_ntohl(p->dst.w[0]) >> 24) == 0xff;
}

static __always_inline struct ct_val *ct_find(const struct pkt *p, struct ct_key *k, int *reply)
{
	__u64 now = bpf_ktime_get_ns();
	make_key(p, k, 0);
	struct ct_val *v = bpf_map_lookup_elem(&ct, k);
	*reply = 0;
	if (!v) {
		make_key(p, k, 1);
		v = bpf_map_lookup_elem(&ct, k);
		*reply = 1;
	}
	if (v && now - v->last_seen > ct_timeout(v->state)) {
		bpf_map_delete_elem(&ct, k);
		return 0;
	}
	return v;
}

SEC("tc")
int tc_ingress(struct __sk_buff *skb)
{
	__u32 ifindex = skb->ifindex;
	struct iface *ifc = bpf_map_lookup_elem(&ifaces, &ifindex);
	if (!ifc)
		return TC_ACT_OK;
	/* тегированный кадр разберёт VLAN-интерфейс со своей зоной */
	if (skb->vlan_present)
		return TC_ACT_OK;

	struct pkt p;
	if (parse(skb, &p) < 0)
		return TC_ACT_OK;
	if (p.frag) {
		stat_inc(ST_FRAG_PASS);
		return TC_ACT_OK;
	}
	if (!p.ok || is_nd(&p))
		return TC_ACT_OK;

	__u32 zero = 0;
	__u32 *genp = bpf_map_lookup_elem(&active, &zero);
	if (!genp)
		return TC_ACT_OK;
	__u32 gen = *genp;
	__u32 bank = gen & 1;
	struct bank_cfg *cfg = bpf_map_lookup_elem(&bankcfg, &bank);
	if (!cfg)
		return TC_ACT_OK;

	skb->mark |= RN_MARK;

	/* 1. обратная трансляция SNAT */
	if (p.v4 && ifc->addr4 && p.dst.w[3] == ifc->addr4) {
		struct snat_key sk = {
			.nat_addr = p.dst.w[3], .remote_addr = p.src.w[3],
			.nat_port = p.dport, .remote_port = p.sport, .proto = p.proto,
		};
		struct snat_val *sv = bpf_map_lookup_elem(&snat_rev, &sk);
		if (sv) {
			__u32 orig = sv->orig_addr;
			__u16 oport = sv->orig_port;
			if (rw_port(skb, &p, 0, p.dport, oport) < 0 || rw_addr4(skb, &p, 0, p.dst.w[3], orig) < 0)
				return TC_ACT_SHOT;
			p.dst.w[3] = orig;
			p.dport = oport;
		}
	}

	struct ct_key k;
	int reply;
	struct ct_val *v = ct_find(&p, &k, &reply);

	/* 2. DNAT, если пакет не относится к известной сессии */
	struct dnat_val dv = {};
	int dnat_hit = 0;
	__u32 orig_dst = p.dst.w[3];
	__u16 orig_dport = p.dport;
	if (!v && p.v4 && ifc->addr4 && p.dst.w[3] == ifc->addr4 &&
	    (p.proto == IPPROTO_TCP || p.proto == IPPROTO_UDP)) {
		struct dnat_key dk = { .ifindex = ifindex, .port = p.dport, .proto = p.proto };
		struct dnat_val *d = bpf_map_lookup_elem(&dnat, &dk);
		if (d) {
			dv = *d;
			__u16 to_port = dv.to_port ? dv.to_port : p.dport;
			if (rw_port(skb, &p, 0, p.dport, to_port) < 0 || rw_addr4(skb, &p, 0, p.dst.w[3], dv.to_addr) < 0)
				return TC_ACT_SHOT;
			p.dst.w[3] = dv.to_addr;
			p.dport = to_port;
			dnat_hit = 1;
			v = ct_find(&p, &k, &reply);
		}
	}

	if (v) {
		stat_inc(ST_CT_HIT);
		if (!reply && v->gen != gen && !(v->flags & CT_F_LOCAL_OUT)) {
			/* политику поменяли: старая сессия проверяется заново */
			struct verdict vd;
			int local;
			__u16 to = egress_zone(skb, &p, &local);
			if (mgmt_allowed(cfg, skb, &p) || ((v->flags & CT_F_DNAT) && v->rule_id == RULE_ID_DNAT)) {
				vd.action = ACT_ALLOW;
				vd.rule_id = v->rule_id;
			} else {
				policy(bank, ifc->zone, to, &p, &vd);
			}
			if (vd.action != ACT_ALLOW) {
				bpf_map_delete_elem(&ct, &k);
				stat_inc(ST_POLICY_DENY);
				return TC_ACT_SHOT;
			}
			v->gen = gen;
			v->rule_id = vd.rule_id;
		}
		ct_update(v, &p, reply);
		if (reply && (v->flags & CT_F_DNAT) && p.v4) {
			__u32 na = v->nat_addr;
			__u16 np = v->nat_port;
			if (rw_port(skb, &p, 1, p.sport, np) < 0 || rw_addr4(skb, &p, 1, p.src.w[3], na) < 0)
				return TC_ACT_SHOT;
		}
		return TC_ACT_OK;
	}

	/* 3. новая сессия */
	if (is_icmp_error(&p))
		return TC_ACT_OK;

	int local;
	__u16 from = ifc->zone;
	__u16 to = egress_zone(skb, &p, &local);

	/* Ответ DHCP-сервера приходит на адрес, которого у узла ещё нет, и ядро
	 * не считает его локальным, поэтому проверка без local. */
	if (p.proto == IPPROTO_UDP &&
	    ((p.sport == bpf_htons(67) && p.dport == bpf_htons(68)) ||
	     (p.sport == bpf_htons(547) && p.dport == bpf_htons(546))))
		return TC_ACT_OK;

	struct verdict vd;
	if (local && mgmt_allowed(cfg, skb, &p)) {
		vd.action = ACT_ALLOW;
		vd.rule_id = RULE_ID_MGMT;
		vd.flags = 0;
	} else if (dnat_hit && dv.allow) {
		vd.action = ACT_ALLOW;
		vd.rule_id = RULE_ID_DNAT;
		vd.flags = 0;
	} else {
		policy(bank, from, to, &p, &vd);
	}

	if (vd.action != ACT_ALLOW) {
		stat_inc(ST_POLICY_DENY);
		if ((vd.flags & RULE_F_LOG) || (vd.rule_id == RULE_ID_DEFAULT && cfg->log_default && !is_noise(&p)))
			emit(EV_DENY, &p, skb, from, to, vd.rule_id, vd.action);
		if (vd.action == ACT_REJECT && send_rst(skb, &p) == TC_ACT_REDIRECT)
			return TC_ACT_REDIRECT;
		return TC_ACT_SHOT;
	}
	stat_inc(ST_POLICY_ALLOW);
	stat_inc(ST_CT_NEW);

	struct ct_val nv = {};
	nv.created = bpf_ktime_get_ns();
	nv.last_seen = nv.created;
	nv.pkts_orig = 1;
	nv.bytes_orig = p.len;
	nv.rule_id = vd.rule_id;
	nv.gen = gen;
	nv.in_ifindex = ifindex;
	nv.from_zone = from;
	nv.to_zone = to;
	if (p.proto == IPPROTO_TCP)
		nv.state = (p.tcp_flags & TH_SYN) && !(p.tcp_flags & TH_ACK) ? CT_SYN_SENT : CT_EST;
	else if (p.proto == IPPROTO_UDP)
		nv.state = CT_UDP;
	else if (p.proto == IPPROTO_ICMP || p.proto == IPPROTO_ICMPV6)
		nv.state = CT_ICMP;
	else
		nv.state = CT_OTHER;
	if (vd.flags & RULE_F_LOG)
		nv.flags |= CT_F_LOG;
	if (dnat_hit) {
		nv.flags |= CT_F_DNAT;
		nv.nat_addr = orig_dst;
		nv.nat_port = orig_dport;
	}
	make_key(&p, &k, 0);
	bpf_map_update_elem(&ct, &k, &nv, BPF_NOEXIST);
	if (vd.flags & RULE_F_LOG)
		emit(EV_ALLOW, &p, skb, from, to, vd.rule_id, ACT_ALLOW);
	return TC_ACT_OK;
}

static __always_inline int snat_match(struct bank_cfg *cfg, __u32 bank, __u32 ifindex, const struct pkt *p, __u32 *to_addr)
{
	__u32 n = cfg->snat_count;
	for (__u32 i = 0; i < MAX_SNAT_RULES; i++) {
		if (i >= n)
			break;
		__u32 idx = bank * MAX_SNAT_RULES + i;
		struct snat_rule *r = bpf_map_lookup_elem(&snat_rules, &idx);
		if (!r)
			break;
		if (r->out_ifindex != ifindex)
			continue;
		if (r->src_set && !in_set(r->src_set, &p->src))
			continue;
		*to_addr = r->to_addr;
		return 1;
	}
	return 0;
}

SEC("tc")
int tc_egress(struct __sk_buff *skb)
{
	__u32 ifindex = skb->ifindex;
	struct iface *ifc = bpf_map_lookup_elem(&ifaces, &ifindex);
	if (!ifc)
		return TC_ACT_OK;

	struct pkt p;
	if (parse(skb, &p) < 0 || p.frag || !p.ok || is_nd(&p))
		return TC_ACT_OK;

	struct ct_key k;
	int reply;
	struct ct_val *v = ct_find(&p, &k, &reply);

	if (!v) {
		if (skb->mark & RN_MARK)
			return TC_ACT_OK;
		/* исходящее от самого узла: запоминаем, чтобы пропустить ответы */
		if (is_icmp_error(&p))
			return TC_ACT_OK;
		__u32 zero = 0;
		__u32 *genp = bpf_map_lookup_elem(&active, &zero);
		struct ct_val nv = {};
		nv.created = bpf_ktime_get_ns();
		nv.last_seen = nv.created;
		nv.pkts_orig = 1;
		nv.bytes_orig = p.len;
		nv.gen = genp ? *genp : 0;
		nv.from_zone = ZONE_SELF;
		nv.to_zone = ifc->zone;
		nv.flags = CT_F_LOCAL_OUT;
		nv.rule_id = RULE_ID_DEFAULT;
		if (p.proto == IPPROTO_TCP)
			nv.state = (p.tcp_flags & TH_SYN) && !(p.tcp_flags & TH_ACK) ? CT_SYN_SENT : CT_EST;
		else if (p.proto == IPPROTO_UDP)
			nv.state = CT_UDP;
		else if (p.proto == IPPROTO_ICMP || p.proto == IPPROTO_ICMPV6)
			nv.state = CT_ICMP;
		else
			nv.state = CT_OTHER;
		make_key(&p, &k, 0);
		bpf_map_update_elem(&ct, &k, &nv, BPF_NOEXIST);
		return TC_ACT_OK;
	}

	if (!(skb->mark & RN_MARK))
		ct_update(v, &p, reply);

	if (reply || !p.v4 || (v->flags & (CT_F_LOCAL_OUT | CT_F_DNAT)))
		return TC_ACT_OK;
	if (p.proto != IPPROTO_TCP && p.proto != IPPROTO_UDP &&
	    !(p.proto == IPPROTO_ICMP && p.icmp_type == ICMP_ECHO))
		return TC_ACT_OK;

	if (v->flags & CT_F_SNAT) {
		if (rw_port(skb, &p, 1, p.sport, v->nat_port) < 0 || rw_addr4(skb, &p, 1, p.src.w[3], v->nat_addr) < 0)
			return TC_ACT_SHOT;
		return TC_ACT_OK;
	}

	__u32 zero = 0;
	__u32 *genp = bpf_map_lookup_elem(&active, &zero);
	if (!genp)
		return TC_ACT_OK;
	__u32 bank = *genp & 1;
	struct bank_cfg *cfg = bpf_map_lookup_elem(&bankcfg, &bank);
	if (!cfg)
		return TC_ACT_OK;

	__u32 to_addr = 0;
	if (!snat_match(cfg, bank, ifindex, &p, &to_addr))
		return TC_ACT_OK;
	if (!to_addr)
		to_addr = ifc->addr4;
	if (!to_addr || to_addr == p.src.w[3])
		return TC_ACT_OK;

	/* Порты NAT берутся из 41000–65535: 32768–40999 оставлены ядру для
	 * собственных соединений узла (см. sysctl в агенте). */
	struct snat_key sk = {
		.nat_addr = to_addr, .remote_addr = p.dst.w[3],
		.remote_port = p.dport, .proto = p.proto,
	};
	struct snat_val sv = { .orig_addr = p.src.w[3], .orig_port = p.sport };
	__u16 port = 0;
	__u16 first = bpf_ntohs(p.sport);
	if (first >= 41000) {
		sk.nat_port = p.sport;
		if (bpf_map_update_elem(&snat_rev, &sk, &sv, BPF_NOEXIST) == 0)
			port = first;
	}
	if (!port) {
		__u32 base = bpf_get_prandom_u32();
#pragma unroll
		for (int i = 0; i < 16; i++) {
			__u16 cand = 41000 + (base + i * 7919) % 24536;
			sk.nat_port = bpf_htons(cand);
			if (bpf_map_update_elem(&snat_rev, &sk, &sv, BPF_NOEXIST) == 0) {
				port = cand;
				break;
			}
		}
	}
	if (!port) {
		stat_inc(ST_NAT_FAIL);
		return TC_ACT_SHOT;
	}

	v->flags |= CT_F_SNAT;
	v->nat_addr = to_addr;
	v->nat_port = bpf_htons(port);
	if (rw_port(skb, &p, 1, p.sport, bpf_htons(port)) < 0 || rw_addr4(skb, &p, 1, p.src.w[3], to_addr) < 0)
		return TC_ACT_SHOT;
	return TC_ACT_OK;
}
