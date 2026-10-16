import { useMemo, useState } from "react";
import { api } from "../lib/api";
import { usePoll, navigate } from "../lib/router";
import { useApp } from "../lib/state";
import type { Dashboard, Link } from "../lib/types";
import { bps, bpsParts, bytes, count, duration } from "../lib/format";
import { t } from "../i18n";
import { TrafficChart, Sparkline } from "../components/Chart";
import { Meter, ZoneChip, cx } from "../components/ui";

function Stat({ label, value, unit, foot, children }: { label: string; value: string; unit?: string; foot?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <div className="panel stat">
      <div className="stat-label">{label}</div>
      <div className="stat-value">
        {value}
        {unit && <small>{unit}</small>}
      </div>
      {foot && <div className="stat-foot">{foot}</div>}
      {children}
    </div>
  );
}

export function Overview() {
  const app = useApp();
  const { data, error } = usePoll(() => api.get<Dashboard>("/v1/dashboard"), 5000);
  const cfg = app.candidate!.body;
  const wanNames = cfg.interfaces.filter((i) => i.role === "wan").map((i) => i.name);
  const [chartIface, setChartIface] = useState<string>("");

  const ifaceOptions = useMemo(() => (data?.links ?? []).map((l) => l.name), [data]);
  const selected = chartIface || wanNames.find((n) => ifaceOptions.includes(n)) || ifaceOptions[0] || "";

  const samples = data?.traffic ?? [];
  const times = samples.map((s) => s.at);
  const rx = samples.map((s) => s.rates[selected]?.[0] ?? 0);
  const tx = samples.map((s) => s.rates[selected]?.[1] ?? 0);
  const lastRates = samples.length ? samples[samples.length - 1].rates : {};

  const wanRx = wanNames.reduce((a, n) => a + (lastRates[n]?.[0] ?? 0), 0);
  const wanTx = wanNames.reduce((a, n) => a + (lastRates[n]?.[1] ?? 0), 0);
  const [rxv, rxu] = bpsParts(wanRx);

  const st = data?.agent?.stats;
  const host = data?.host;
  const memPct = host && host.mem_total ? (host.mem_used / host.mem_total) * 100 : 0;
  const diskPct = host && host.disk_total ? (host.disk_used / host.disk_total) * 100 : 0;

  const byName = new Map(cfg.interfaces.map((i) => [i.name, i]));
  const links: Link[] = (data?.links ?? []).slice().sort((a, b) => {
    const ra = byName.has(a.name) ? 0 : 1;
    const rb = byName.has(b.name) ? 0 : 1;
    return ra - rb || a.name.localeCompare(b.name);
  });

  return (
    <>
      {error && !data && <div className="note bad">{error}</div>}
      {data?.agent_error && <div className="note bad" style={{ marginBottom: 12 }}>{t("ov.agentdown")}: {data.agent_error}</div>}
      {(data?.agent?.gateways ?? [])
        .filter((g) => !g.ok && wanNames.includes(g.interface))
        .map((g) => (
          <div key={g.interface + g.address} className="note warn" style={{ marginBottom: 12 }}>
            {t("ov.gwdown", { gw: g.address, iface: g.interface })}
          </div>
        ))}

      <div className="grid grid-4">
        <Stat label={t("ov.wan")} value={rxv} unit={rxu} foot={<>↑ {bps(wanTx)}</>} />
        <Stat label={t("ov.sessions")} value={count(st?.sessions ?? 0)} foot={t("ov.sessions.foot", { n: count(st?.ct_new ?? 0) })} />
        <Stat
          label={t("ov.dropped")}
          value={count((st?.policy_deny ?? 0) + (st?.xdp_blocklist ?? 0) + (st?.xdp_synflood ?? 0))}
          foot={t("ov.dropped.foot", { p: count(st?.policy_deny ?? 0), x: count((st?.xdp_blocklist ?? 0) + (st?.xdp_synflood ?? 0)) })}
        />
        <Stat label={t("ov.cpu")} value={(host?.cpu_percent ?? 0).toFixed(0)} unit="%" foot={t("ov.mem", { used: bytes(host?.mem_used ?? 0), total: bytes(host?.mem_total ?? 0) })}>
          <Meter value={memPct} />
        </Stat>
      </div>

      <div className="grid" style={{ gridTemplateColumns: "minmax(0, 2fr) minmax(260px, 1fr)", marginTop: 12 }}>
        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">{t("ov.traffic")}</span>
            <div className="legend">
              <span>
                <i style={{ background: "var(--accent)" }} />
                {t("ov.rx")}
              </span>
              <span>
                <i style={{ background: "var(--info)" }} />
                {t("ov.tx")}
              </span>
            </div>
            <div className="spacer" />
            <select className="select" style={{ width: 140, height: 26 }} value={selected} onChange={(e) => setChartIface(e.target.value)}>
              {ifaceOptions.map((n) => (
                <option key={n}>{n}</option>
              ))}
            </select>
          </div>
          <div style={{ padding: "10px 12px 6px" }}>
            {samples.length < 2 ? (
              <div className="faint" style={{ height: 180, display: "grid", placeItems: "center" }}>
                {t("ov.collecting")}
              </div>
            ) : (
              <TrafficChart
                times={times}
                series={[
                  { label: t("ov.rx"), color: "var(--accent)", values: rx },
                  { label: t("ov.tx"), color: "var(--info)", values: tx },
                ]}
              />
            )}
          </div>
        </div>

        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">{t("ov.node")}</span>
          </div>
          <div className="panel-body">
            <dl className="kv">
              <dt>{t("ov.hostname")}</dt>
              <dd className="mono">{cfg.system.hostname || host?.hostname}</dd>
              <dt>{t("ov.version")}</dt>
              <dd className="mono">{data?.version ?? "—"}</dd>
              <dt>{t("ov.uptime")}</dt>
              <dd>{host ? duration(host.uptime_s) : "—"}</dd>
              <dt>{t("ov.kernel")}</dt>
              <dd className="mono">{host?.kernel ?? "—"}</dd>
              <dt>{t("ov.load")}</dt>
              <dd className="mono">{host ? host.load.map((l) => l.toFixed(2)).join("  ") : "—"}</dd>
              <dt>{t("ov.revision")}</dt>
              <dd>
                {data?.revision ? (
                  <a href="#/config" className="mono">
                    r{data.revision.id}
                  </a>
                ) : (
                  "—"
                )}
                {data?.candidate_dirty && <span className="badge accent" style={{ marginLeft: 8 }}>{t("ov.dirty")}</span>}
              </dd>
              <dt>XDP</dt>
              <dd>
                {data?.agent ? (
                  <span className={cx("badge", data.agent.xdp_mode === "native" ? "ok" : "warn")}>{data.agent.xdp_mode}</span>
                ) : (
                  "—"
                )}
              </dd>
              <dt>{t("ov.disk")}</dt>
              <dd>
                {host?.disk_total ? `${bytes(host.disk_used)} / ${bytes(host.disk_total)}` : "—"}
                {host?.disk_total ? <Meter value={diskPct} /> : null}
              </dd>
            </dl>
          </div>
        </div>
      </div>

      <div className="section">
        <div className="section-head">
          <span className="section-title">{t("ov.interfaces")}</span>
          <div className="actions">
            <button className="btn btn-sm btn-ghost" onClick={() => navigate("/interfaces")}>
              {t("ov.configure")}
            </button>
          </div>
        </div>
        <div className="table-wrap">
          <table className="t">
            <thead>
              <tr>
                <th>{t("col.interface")}</th>
                <th>{t("col.zone")}</th>
                <th>{t("col.state")}</th>
                <th>{t("col.addresses")}</th>
                <th className="num">{t("ov.rx")}</th>
                <th className="num">{t("ov.tx")}</th>
                <th style={{ width: 110 }}>{t("ov.trend")}</th>
              </tr>
            </thead>
            <tbody>
              {links.map((l) => {
                const c = byName.get(l.name);
                const r = lastRates[l.name] ?? [0, 0];
                return (
                  <tr key={l.name} className={cx(!c && "off")}>
                    <td>
                      <span className="mono">{l.name}</span>
                      {l.speed_mbps ? <span className="faint"> · {l.speed_mbps >= 1000 ? `${l.speed_mbps / 1000}G` : `${l.speed_mbps}M`}</span> : null}
                    </td>
                    <td>{c ? <ZoneChip name={c.zone} /> : <span className="faint">{t("ov.unassigned")}</span>}</td>
                    <td>
                      <span className="row" style={{ gap: 6 }}>
                        <span className={cx("dot", l.carrier ? "ok" : l.up ? "warn" : "")} />
                        {l.carrier ? t("link.up") : l.up ? t("link.nocarrier") : t("link.down")}
                      </span>
                    </td>
                    <td className="mono">{l.addresses.filter((a) => !a.startsWith("fe80")).join(", ") || <span className="faint">—</span>}</td>
                    <td className="num mono">{bps(r[0])}</td>
                    <td className="num mono">{bps(r[1])}</td>
                    <td>
                      <Sparkline values={samples.map((s) => (s.rates[l.name]?.[0] ?? 0) + (s.rates[l.name]?.[1] ?? 0))} color="var(--text-2)" />
                    </td>
                  </tr>
                );
              })}
              {links.length === 0 && (
                <tr>
                  <td colSpan={7} className="empty">
                    {t("common.nodata")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      <div className="grid grid-2 section">
        <TopTable title={t("ov.topsrc")} items={data?.top_denied_sources} mono link={(k) => `#/events?addr=${encodeURIComponent(k)}`} />
        <TopTable title={t("ov.topports")} items={data?.top_denied_ports} mono />
      </div>
    </>
  );
}

function TopTable({ title, items, mono, link }: { title: string; items?: { key: string; count: number }[]; mono?: boolean; link?: (k: string) => string }) {
  const max = Math.max(1, ...(items ?? []).map((i) => i.count));
  return (
    <div>
      <div className="section-head">
        <span className="section-title">{title}</span>
        <span className="faint" style={{ fontSize: 12 }}>
          {t("ov.lasthour")}
        </span>
      </div>
      <div className="table-wrap">
        <table className="t">
          <tbody>
            {(items ?? []).map((i) => (
              <tr key={i.key}>
                <td className={cx(mono && "mono")} style={{ width: "45%" }}>
                  {link ? <a href={link(i.key)}>{i.key}</a> : i.key}
                </td>
                <td>
                  <div className="meter" style={{ marginTop: 0 }}>
                    <i style={{ width: `${(i.count / max) * 100}%`, background: "var(--bad)" }} />
                  </div>
                </td>
                <td className="num mono" style={{ width: 70 }}>
                  {count(i.count)}
                </td>
              </tr>
            ))}
            {(!items || items.length === 0) && (
              <tr>
                <td className="empty">{t("ov.nodenied")}</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
