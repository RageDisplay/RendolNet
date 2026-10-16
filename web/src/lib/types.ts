// Повторяет структуры Go из internal/netcfg и internal/store.

export type Role = "wan" | "lan" | "mgmt";
export type Proto = "any" | "tcp" | "udp" | "icmp" | "icmpv6";
export type Action = "allow" | "deny" | "reject";

export interface Zone {
  name: string;
  description?: string;
}

export interface Iface {
  name: string;
  description?: string;
  role: Role;
  zone: string;
  enabled: boolean;
  dhcp?: boolean;
  addresses?: string[];
  mtu?: number;
  vlan?: { parent: string; id: number } | null;
}

export interface Route {
  destination: string;
  gateway?: string;
  interface?: string;
  metric?: number;
  comment?: string;
}

export interface AddressObject {
  name: string;
  prefixes: string[];
  comment?: string;
}
export interface AddressGroup {
  name: string;
  members: string[];
  comment?: string;
}
export interface Service {
  name: string;
  proto: Proto;
  ports?: string;
  comment?: string;
}

export interface Rule {
  id: string;
  name: string;
  enabled: boolean;
  from: string[];
  to: string[];
  source?: string[];
  destination?: string[];
  services?: string[];
  action: Action;
  log?: boolean;
  comment?: string;
}

export interface SNATRule {
  id: string;
  name: string;
  enabled: boolean;
  interface: string;
  source?: string[];
  translate?: string;
  comment?: string;
}

export interface DNATRule {
  id: string;
  name: string;
  enabled: boolean;
  interface: string;
  proto: Proto;
  port: number;
  to_address: string;
  to_port?: number;
  allow_policy: boolean;
  comment?: string;
}

export interface Config {
  schema: number;
  system: { hostname: string; dns?: string[] };
  zones: Zone[];
  interfaces: Iface[];
  routes: Route[];
  objects: { addresses: AddressObject[]; groups: AddressGroup[]; services: Service[] };
  rules: Rule[];
  nat: { source: SNATRule[]; destination: DNATRule[] };
  protection: { blocklist?: string[]; syn_rate?: number; syn_burst?: number; log_default_deny: boolean };
  management: { interface: string; allow_from?: string[]; ports: number[] };
  zapret: { enabled: boolean; strategy?: string; interface?: string; fooling?: string; hosts?: string[]; ips?: string[] };
}

export interface FieldError {
  path: string;
  message: string;
}

export interface Candidate {
  body: Config;
  base_revision: number;
  updated_at: string;
  updated_by: string;
}

export type RevState = "pending" | "committed" | "rolled_back" | "failed" | "imported";

export interface Revision {
  id: number;
  created_at: string;
  author: string;
  comment: string;
  state: RevState;
  confirm_deadline?: string;
  error?: string;
  body?: Config;
}

export interface User {
  id: number;
  username: string;
  full_name: string;
  role: "admin" | "operator" | "viewer";
  disabled: boolean;
  created_at: string;
  last_login?: string;
}

export interface Link {
  name: string;
  index: number;
  mac: string;
  kind: string;
  up: boolean;
  carrier: boolean;
  mtu: number;
  speed_mbps?: number;
  addresses: string[];
  rx_bytes: number;
  tx_bytes: number;
  rx_packets: number;
  tx_packets: number;
  rx_dropped: number;
  tx_dropped: number;
}

export interface DPStats {
  xdp_blocklist: number;
  xdp_synflood: number;
  policy_deny: number;
  policy_allow: number;
  ct_hit: number;
  ct_new: number;
  nat_fail: number;
  frag_pass: number;
  ringbuf_full: number;
  reject_sent: number;
  sessions: number;
}

export interface AgentStatus {
  version: string;
  revision: number;
  confirmed_revision: number;
  deadline?: string;
  applied: string;
  generation: number;
  xdp_mode: string;
  last_error?: string;
  rollback?: { time: string; revision: number; reason: string };
  stats: DPStats;
  boot_guard: boolean;
  gateways?: { interface: string; address: string; ok: boolean; rtt_ms?: number; error?: string; checked: string }[];
}

export interface HostInfo {
  cpu_percent: number;
  cpus: number;
  mem_total: number;
  mem_used: number;
  load: [number, number, number];
  uptime_s: number;
  disk_total: number;
  disk_used: number;
  kernel: string;
  hostname: string;
  time: string;
}

export interface TrafficSample {
  at: string;
  rates: Record<string, [number, number]>;
}

export interface TopEntry {
  key: string;
  count: number;
}

export interface Dashboard {
  version: string;
  host: HostInfo;
  traffic: TrafficSample[];
  agent?: AgentStatus;
  agent_error?: string;
  links?: Link[];
  revision?: Revision;
  candidate_dirty?: boolean;
  top_denied_sources?: TopEntry[];
  top_denied_ports?: TopEntry[];
}

export interface Session {
  proto: number;
  src: string;
  dst: string;
  sport: number;
  dport: number;
  state: string;
  from_zone: string;
  to_zone: string;
  rule_id?: string;
  nat?: string;
  pkts_orig: number;
  bytes_orig: number;
  pkts_reply: number;
  bytes_reply: number;
  age_s: number;
  idle_s: number;
}

export interface FwEvent {
  id: number;
  at: string;
  type: "deny" | "allow" | "session_end";
  action: string;
  proto: number;
  src: string;
  dst: string;
  sport: number;
  dport: number;
  from_zone: string;
  to_zone: string;
  rule_id: string;
  iface: string;
  packets: number;
  bytes: number;
  count: number;
}

export interface AuditEntry {
  id: number;
  at: string;
  username: string;
  action: string;
  target: string;
  details?: unknown;
  remote_addr: string;
}

export interface UpdateManifest {
  product: string;
  version: string;
  created: string;
  min_version?: string;
  images: string[];
  k3s?: string;
  notes?: string;
}

export interface UpdateStatus {
  request_id?: string;
  action?: string;
  state: "idle" | "queued" | "running" | "done" | "failed" | "rolled_back";
  version?: string;
  from?: string;
  step?: string;
  message?: string;
  started?: string;
  finished?: string;
  log?: string[];
}

export interface Settings {
  event_retention_days: number;
  traffic_retention_days: number;
  login_banner: string;
  default_confirm_seconds: number;
}

export interface Strategy {
  name: string;
  source: string;
  args: string[];
  compatible: boolean;
  problems?: string[];
}

export interface ZapretStatus {
  enabled: boolean;
  running: boolean;
  strategy?: string;
  iface?: string;
  started?: string;
  restarts: number;
  error?: string;
}
