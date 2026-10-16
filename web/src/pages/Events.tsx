import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { usePoll } from "../lib/router";
import type { FwEvent } from "../lib/types";
import { bytes, datetime, endpoint, protoName } from "../lib/format";
import { t } from "../i18n";
import { Seg, Switch, ZoneChip, cx } from "../components/ui";
import * as I from "../components/icons";

function initialAddr(): string {
  const m = window.location.hash.match(/addr=([^&]+)/);
  return m ? decodeURIComponent(m[1]) : "";
}

export function Events() {
  const [type, setType] = useState<"" | "deny" | "allow" | "session_end">("");
  const [addr, setAddr] = useState(initialAddr());
  const [port, setPort] = useState("");
  const [rule, setRule] = useState("");
  const [live, setLive] = useState(true);
  const [older, setOlder] = useState<FwEvent[]>([]);

  const query = () => {
    const p = new URLSearchParams({ limit: "300" });
    if (type) p.set("type", type);
    if (addr.trim()) p.set("addr", addr.trim());
    if (port.trim()) p.set("port", port.trim());
    if (rule.trim()) p.set("rule", rule.trim());
    return p;
  };
  const { data, error } = usePoll(() => api.get<FwEvent[]>("/v1/events?" + query().toString()), live ? 3000 : 0, [type, addr, port, rule, live]);
  useEffect(() => setOlder([]), [type, addr, port, rule]);

  const rows = [...(data ?? []), ...older];
  const loadMore = async () => {
    const last = rows[rows.length - 1];
    if (!last) return;
    const p = query();
    p.set("before", String(last.id));
    const more = await api.get<FwEvent[]>("/v1/events?" + p.toString());
    setOlder((o) => [...o, ...more]);
  };

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("ev.lead")}</div>
      </div>
      <div className="row" style={{ marginBottom: 12, flexWrap: "wrap" }}>
        <Seg
          value={type}
          onChange={setType}
          options={[
            { value: "", label: t("ev.all") },
            { value: "deny", label: t("ev.deny") },
            { value: "allow", label: t("ev.allow") },
            { value: "session_end", label: t("ev.sessions") },
          ]}
        />
        <div className="search">
          <I.IconSearch />
          <input className="input mono" style={{ width: 200 }} placeholder={t("ev.addr")} value={addr} onChange={(e) => setAddr(e.target.value)} />
        </div>
        <input className="input mono" style={{ width: 90 }} placeholder={t("ev.port")} value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))} />
        <input className="input mono" style={{ width: 140 }} placeholder={t("ev.rule")} value={rule} onChange={(e) => setRule(e.target.value)} />
        <div className="spacer" />
        <Switch checked={live} onChange={setLive} label={t("common.live")} />
      </div>
      {error && <div className="note bad" style={{ marginBottom: 12 }}>{error}</div>}
      <div className="table-wrap" style={{ maxHeight: "calc(100vh - 240px)" }}>
        <table className="t">
          <thead>
            <tr>
              <th>{t("col.time")}</th>
              <th>{t("col.action")}</th>
              <th>{t("col.proto")}</th>
              <th>{t("col.source")}</th>
              <th>{t("col.destination")}</th>
              <th>{t("col.zones")}</th>
              <th>{t("col.rule")}</th>
              <th>{t("col.interface")}</th>
              <th className="num">{t("ev.count")}</th>
              <th className="num">{t("ev.bytes")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((e) => (
              <tr key={e.id}>
                <td className="mono faint nowrap">{datetime(e.at)}:{String(new Date(e.at).getSeconds()).padStart(2, "0")}</td>
                <td>
                  <span className={cx("action-cell", e.type === "session_end" ? "" : `action-${e.action}`)}>
                    {e.type === "session_end" ? t("ev.ended") : t(`action.${e.action}` as any)}
                  </span>
                </td>
                <td className="mono">{protoName(e.proto)}</td>
                <td className="mono">
                  <a href="#/events" onClick={(ev) => { ev.preventDefault(); setAddr(e.src); }}>{endpoint(e.src, e.sport, e.proto)}</a>
                </td>
                <td className="mono">{endpoint(e.dst, e.dport, e.proto)}</td>
                <td>
                  <span className="chips">
                    {e.from_zone && <ZoneChip name={e.from_zone} />}
                    <span className="arrow">→</span>
                    <ZoneChip name={e.to_zone || "self"} />
                  </span>
                </td>
                <td className="mono">{e.rule_id || <span className="faint">{t("ev.default")}</span>}</td>
                <td className="mono faint">{e.iface}</td>
                <td className="num mono">{e.count > 1 ? `×${e.count}` : ""}</td>
                <td className="num mono faint">{bytes(e.bytes)}</td>
              </tr>
            ))}
            {rows.length === 0 && (
              <tr>
                <td colSpan={10} className="empty">
                  {t("ev.empty")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {rows.length >= 300 && (
        <div style={{ marginTop: 10, textAlign: "center" }}>
          <button className="btn" onClick={loadMore}>
            {t("ev.more")}
          </button>
        </div>
      )}
    </>
  );
}
