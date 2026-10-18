import { useState } from "react";
import { api } from "../lib/api";
import { usePoll } from "../lib/router";
import { useApp } from "../lib/state";
import { t } from "../i18n";
import { Field, Seg, cx } from "../components/ui";
import { IconRefresh } from "../components/icons";

interface PingResult {
  target: string;
  address: string;
  replies: { seq: number; rtt_ms?: number; lost?: boolean }[];
  received: number;
}
interface TraceResult {
  target: string;
  address: string;
  hops: { ttl: number; address?: string; rtt_ms?: number }[];
  reached: boolean;
}
interface LookupResult {
  name: string;
  server: string;
  addresses: string[];
  rtt_ms: number;
}
interface RouteEntry {
  destination: string;
  gateway?: string;
  interface?: string;
  source?: string;
  protocol: string;
  metric?: number;
}
interface Neighbor {
  address: string;
  mac?: string;
  interface: string;
  state: string;
}

type Tool = "ping" | "trace" | "lookup";
type Result = { tool: "ping"; r: PingResult } | { tool: "trace"; r: TraceResult } | { tool: "lookup"; r: LookupResult };

const rtt = (v?: number) => (v === undefined ? "—" : v < 10 ? v.toFixed(2) : v.toFixed(1));

export function Diag() {
  const app = useApp();
  const cfg = app.candidate!.body;
  const [tool, setTool] = useState<Tool>("ping");
  const [target, setTarget] = useState("8.8.8.8");
  const [iface, setIface] = useState("");
  const [server, setServer] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [res, setRes] = useState<Result | null>(null);
  const [tab, setTab] = useState<"routes" | "neighbors">("routes");

  const routes = usePoll(() => api.get<RouteEntry[]>("/v1/diag/routes"), 0);
  const neigh = usePoll(() => api.get<Neighbor[]>("/v1/diag/neighbors"), 0);

  const run = async () => {
    setBusy(true);
    setErr("");
    setRes(null);
    try {
      if (tool === "ping") setRes({ tool, r: await api.post<PingResult>("/v1/diag/ping", { target: target.trim(), interface: iface, count: 4 }) });
      if (tool === "trace") setRes({ tool, r: await api.post<TraceResult>("/v1/diag/trace", { target: target.trim() }) });
      if (tool === "lookup") setRes({ tool, r: await api.post<LookupResult>("/v1/diag/lookup", { name: target.trim(), server: server.trim() }) });
    } catch (e: any) {
      setErr(e.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("diag.lead")}</div>
      </div>

      <div className="panel">
        <div className="panel-head">
          <Seg
            value={tool}
            onChange={(v) => {
              setTool(v);
              setRes(null);
              setErr("");
            }}
            options={[
              { value: "ping", label: "ping" },
              { value: "trace", label: "traceroute" },
              { value: "lookup", label: "DNS" },
            ]}
          />
        </div>
        <div className="panel-body stack">
          <form
            className="form-grid"
            style={{ gridTemplateColumns: "minmax(0, 2fr) minmax(0, 1fr) auto", alignItems: "end" }}
            onSubmit={(e) => {
              e.preventDefault();
              if (!busy && target.trim()) void run();
            }}
          >
            <Field label={tool === "lookup" ? t("diag.name") : t("diag.target")}>
              <input className="input mono" value={target} onChange={(e) => setTarget(e.target.value)} placeholder={tool === "lookup" ? "youtube.com" : "8.8.8.8 / ya.ru"} />
            </Field>
            {tool === "ping" && (
              <Field label={t("diag.iface")}>
                <select className="select" value={iface} onChange={(e) => setIface(e.target.value)}>
                  <option value="">{t("diag.iface.route")}</option>
                  {cfg.interfaces.map((i) => (
                    <option key={i.name}>{i.name}</option>
                  ))}
                </select>
              </Field>
            )}
            {tool === "lookup" && (
              <Field label={t("diag.server")}>
                <input className="input mono" value={server} onChange={(e) => setServer(e.target.value)} placeholder={cfg.system.dns?.[0] ?? "1.1.1.1"} />
              </Field>
            )}
            {tool === "trace" && <div />}
            <button className="btn btn-primary" type="submit" disabled={busy || !target.trim() || !app.canEdit}>
              {busy ? t("diag.running") : t("diag.run")}
            </button>
          </form>
          {!app.canEdit && <div className="faint">{t("diag.ro")}</div>}
          {err && <div className="note bad">{err}</div>}
          {res && <ResultView res={res} />}
        </div>
      </div>

      <div className="section">
        <div className="tabs">
          <button className={cx(tab === "routes" && "on")} onClick={() => setTab("routes")}>
            {t("diag.routes")}
            <span className="count">{routes.data?.length ?? 0}</span>
          </button>
          <button className={cx(tab === "neighbors" && "on")} onClick={() => setTab("neighbors")}>
            {t("diag.neighbors")}
            <span className="count">{neigh.data?.length ?? 0}</span>
          </button>
          <div className="spacer" />
          <button className="btn btn-sm btn-ghost" title={t("diag.refresh")} onClick={() => (tab === "routes" ? routes.reload() : neigh.reload())}>
            <IconRefresh width={14} height={14} />
          </button>
        </div>
        {(tab === "routes" ? routes.error : neigh.error) && <div className="note bad">{tab === "routes" ? routes.error : neigh.error}</div>}
        <div className="table-wrap">
          {tab === "routes" ? (
            <table className="t">
              <thead>
                <tr>
                  <th>{t("diag.dst")}</th>
                  <th>{t("diag.gw")}</th>
                  <th>{t("diag.iface")}</th>
                  <th>{t("diag.src")}</th>
                  <th>{t("diag.proto")}</th>
                  <th>{t("diag.metric")}</th>
                </tr>
              </thead>
              <tbody>
                {(routes.data ?? []).map((r, i) => (
                  <tr key={i}>
                    <td className="mono">{r.destination}</td>
                    <td className="mono">{r.gateway ?? <span className="faint">—</span>}</td>
                    <td className="mono">{r.interface}</td>
                    <td className="mono faint">{r.source}</td>
                    <td className="faint">{r.protocol}</td>
                    <td className="mono faint">{r.metric || ""}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <table className="t">
              <thead>
                <tr>
                  <th>IP</th>
                  <th>MAC</th>
                  <th>{t("diag.iface")}</th>
                  <th>{t("diag.state")}</th>
                </tr>
              </thead>
              <tbody>
                {(neigh.data ?? []).map((n) => (
                  <tr key={n.interface + n.address}>
                    <td className="mono">{n.address}</td>
                    <td className="mono faint">{n.mac ?? "—"}</td>
                    <td className="mono">{n.interface}</td>
                    <td>
                      <span className={cx("badge", n.state === "reachable" || n.state === "permanent" ? "ok" : n.state === "failed" || n.state === "incomplete" ? "bad" : "neutral")}>{n.state}</span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </>
  );
}

function ResultView({ res }: { res: Result }) {
  if (res.tool === "ping") {
    const r = res.r;
    const ok = r.replies.filter((x) => !x.lost).map((x) => x.rtt_ms!);
    return (
      <div className="stack">
        <div>
          <span className="mono">{r.address}</span>{" "}
          <span className={cx("badge", r.received === r.replies.length ? "ok" : r.received === 0 ? "bad" : "warn")}>
            {t("diag.ping.sum", { got: r.received, sent: r.replies.length })}
          </span>
          {ok.length > 0 && (
            <span className="faint mono" style={{ marginLeft: 8 }}>
              min/avg/max {rtt(Math.min(...ok))}/{rtt(ok.reduce((a, b) => a + b, 0) / ok.length)}/{rtt(Math.max(...ok))} ms
            </span>
          )}
        </div>
        <div className="log">{r.replies.map((x) => (x.lost ? `seq=${x.seq}  ${t("diag.lost")}` : `seq=${x.seq}  ${rtt(x.rtt_ms)} ms`)).join("\n")}</div>
      </div>
    );
  }
  if (res.tool === "trace") {
    const r = res.r;
    return (
      <div className="stack">
        <div>
          <span className="mono">{r.address}</span>{" "}
          {r.reached ? <span className="badge ok">{t("diag.reached")}</span> : <span className="badge warn">{t("diag.notreached")}</span>}
        </div>
        <table className="t">
          <thead>
            <tr>
              <th style={{ width: 48 }}>#</th>
              <th>{t("diag.hop")}</th>
              <th>ms</th>
            </tr>
          </thead>
          <tbody>
            {r.hops.map((h) => (
              <tr key={h.ttl}>
                <td className="mono faint">{h.ttl}</td>
                <td className="mono">{h.address ?? <span className="faint">* * *</span>}</td>
                <td className="mono">{rtt(h.rtt_ms)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }
  const r = res.r;
  return (
    <div className="stack">
      <div className="faint">
        {t("diag.lookup.via", { server: r.server })} · <span className="mono">{rtt(r.rtt_ms)} ms</span>
      </div>
      <div className="log">{r.addresses.join("\n")}</div>
    </div>
  );
}
