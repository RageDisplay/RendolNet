import { lang } from "../i18n";

export function bytes(n: number, digits = 1): string {
  if (!isFinite(n)) return "—";
  const u = lang() === "ru" ? ["Б", "КБ", "МБ", "ГБ", "ТБ"] : ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n.toFixed(i === 0 ? 0 : digits)} ${u[i]}`;
}

// Скорость в битах — так её считают сетевики.
export function bps(n: number): string {
  const u = lang() === "ru" ? ["бит/с", "Кбит/с", "Мбит/с", "Гбит/с"] : ["bps", "Kbps", "Mbps", "Gbps"];
  let i = 0;
  while (n >= 1000 && i < u.length - 1) {
    n /= 1000;
    i++;
  }
  return `${n.toFixed(i === 0 ? 0 : n < 10 ? 2 : 1)} ${u[i]}`;
}

export function bpsParts(n: number): [string, string] {
  const s = bps(n);
  const i = s.lastIndexOf(" ");
  return [s.slice(0, i), s.slice(i + 1)];
}

export function count(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1e6) return (n / 1e3).toFixed(n < 1e4 ? 1 : 0) + "k";
  if (n < 1e9) return (n / 1e6).toFixed(n < 1e7 ? 1 : 0) + "M";
  return (n / 1e9).toFixed(1) + "G";
}

export function duration(sec: number): string {
  const ru = lang() === "ru";
  sec = Math.max(0, Math.floor(sec));
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  if (d > 0) return ru ? `${d} д ${h} ч` : `${d}d ${h}h`;
  if (h > 0) return ru ? `${h} ч ${m} мин` : `${h}h ${m}m`;
  if (m > 0) return ru ? `${m} мин ${s} с` : `${m}m ${s}s`;
  return ru ? `${s} с` : `${s}s`;
}

const pad = (n: number) => String(n).padStart(2, "0");

export function time(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export function datetime(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function ago(iso?: string): string {
  if (!iso) return "—";
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return lang() === "ru" ? "только что" : "just now";
  return (lang() === "ru" ? "" : "") + duration(s) + (lang() === "ru" ? " назад" : " ago");
}

export const protoName = (p: number) =>
  ({ 6: "tcp", 17: "udp", 1: "icmp", 58: "icmpv6" } as Record<number, string>)[p] ?? String(p);

export function endpoint(addr: string, port: number, proto: number): string {
  if (proto !== 6 && proto !== 17) return addr;
  return addr.includes(":") ? `[${addr}]:${port}` : `${addr}:${port}`;
}

// Цвет зоны выводится из имени: одна и та же зона везде одного цвета.
export function zoneColor(name: string): string {
  if (name === "self") return "var(--accent)";
  const fixed: Record<string, number> = { wan: 4, lan: 150, mgmt: 210, dmz: 40 };
  let h = fixed[name];
  if (h === undefined) {
    h = 0;
    for (const c of name) h = (h * 31 + c.charCodeAt(0)) % 360;
  }
  return `hsl(${h} 55% 58%)`;
}

export function uid(prefix: string): string {
  return `${prefix}-${Math.random().toString(36).slice(2, 8)}`;
}
