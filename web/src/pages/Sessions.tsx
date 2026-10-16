import { useMemo, useState } from "react";
import { api } from "../lib/api";
import { usePoll } from "../lib/router";
import { useApp } from "../lib/state";
import type { Session } from "../lib/types";
import { bytes, duration, endpoint, protoName } from "../lib/format";
import { t } from "../i18n";
import { Confirm, Switch, ZoneChip, useToast } from "../components/ui";
import * as I from "../components/icons";

export function Sessions() {
  const app = useApp();
  const toast = useToast();
  const [live, setLive] = useState(true);
  const [q, setQ] = useState("");
  const [flush, setFlush] = useState(false);
  const { data, error, reload } = usePoll(() => api.get<Session[]>("/v1/sessions?limit=5000"), live ? 3000 : 0, [live]);

  const rows = useMemo(() => {
    const s = q.trim().toLowerCase();
    const list = data ?? [];
    if (!s) return list;
    return list.filter((x) =>
      [x.src, x.dst, String(x.sport), String(x.dport), protoName(x.proto), x.state, x.from_zone, x.to_zone, x.rule_id ?? "", x.nat ?? ""]
        .join(" ")
        .toLowerCase()
        .includes(s),
    );
  }, [data, q]);

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("ses.lead")}</div>
        <div className="actions">
          <div className="search">
            <I.IconSearch />
            <input className="input" placeholder={t("ses.filter")} value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
          <Switch checked={live} onChange={setLive} label={t("common.live")} />
          <button className="btn btn-icon" onClick={reload} title={t("common.refresh")}>
            <I.IconRefresh />
          </button>
          {app.canEdit && (
            <button className="btn btn-danger" onClick={() => setFlush(true)}>
              {t("ses.flush")}
            </button>
          )}
        </div>
      </div>
      {error && <div className="note bad" style={{ marginBottom: 12 }}>{error}</div>}
      <div className="faint" style={{ marginBottom: 8, fontSize: 12 }}>
        {t("ses.count", { shown: rows.length, total: data?.length ?? 0 })}
      </div>
      <div className="table-wrap" style={{ maxHeight: "calc(100vh - 230px)" }}>
        <table className="t">
          <thead>
            <tr>
              <th>{t("col.proto")}</th>
              <th>{t("col.source")}</th>
              <th>{t("col.destination")}</th>
              <th>{t("col.zones")}</th>
              <th>{t("col.state")}</th>
              <th>NAT</th>
              <th>{t("col.rule")}</th>
              <th className="num">{t("ses.orig")}</th>
              <th className="num">{t("ses.reply")}</th>
              <th className="num">{t("ses.age")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.slice(0, 1000).map((s, i) => (
              <tr key={i}>
                <td className="mono">{protoName(s.proto)}</td>
                <td className="mono">{endpoint(s.src, s.sport, s.proto)}</td>
                <td className="mono">{endpoint(s.dst, s.dport, s.proto)}</td>
                <td>
                  <span className="chips">
                    <ZoneChip name={s.from_zone || "self"} />
                    <span className="arrow">→</span>
                    <ZoneChip name={s.to_zone || "self"} />
                  </span>
                </td>
                <td>
                  <span className={s.state === "established" ? "action-allow" : "muted"}>{s.state}</span>
                </td>
                <td className="mono faint">{s.nat || "—"}</td>
                <td className="mono">{s.rule_id || <span className="faint">—</span>}</td>
                <td className="num mono">{bytes(s.bytes_orig)}</td>
                <td className="num mono">{bytes(s.bytes_reply)}</td>
                <td className="num mono faint">{duration(s.age_s)}</td>
              </tr>
            ))}
            {rows.length === 0 && (
              <tr>
                <td colSpan={10} className="empty">
                  {t("ses.empty")}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {flush && (
        <Confirm
          title={t("ses.flush")}
          text={t("ses.flush.text")}
          action={t("ses.flush")}
          danger
          onClose={() => setFlush(false)}
          onConfirm={async () => {
            try {
              await api.del("/v1/sessions");
              toast(t("ses.flushed"));
              reload();
            } catch (e: any) {
              toast(e.message, true);
            }
          }}
        />
      )}
    </>
  );
}
