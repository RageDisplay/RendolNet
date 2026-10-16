import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { usePoll } from "../lib/router";
import { useApp } from "../lib/state";
import type { AgentStatus, AuditEntry, HostInfo, Settings } from "../lib/types";
import { bytes, datetime, duration } from "../lib/format";
import { t } from "../i18n";
import { Confirm, Field, Meter, useToast } from "../components/ui";

interface SysResp {
  version: string;
  api_started: string;
  host: HostInfo;
  agent?: AgentStatus;
  installed_version?: string;
}

export function SystemPage() {
  const app = useApp();
  const toast = useToast();
  const { data } = usePoll(() => api.get<SysResp>("/v1/system"), 10000);
  const audit = usePoll(() => (app.isAdmin ? api.get<AuditEntry[]>("/v1/audit?limit=200") : Promise.resolve([] as AuditEntry[])), 15000);
  const [set, setSet] = useState<Settings | null>(null);
  const [orig, setOrig] = useState<Settings | null>(null);
  const [power, setPower] = useState<"reboot" | "poweroff" | null>(null);

  useEffect(() => {
    api.get<Settings>("/v1/settings").then((s) => {
      setSet(s);
      setOrig(s);
    });
  }, []);

  const h = data?.host;
  const a = data?.agent;

  return (
    <>
      <div className="grid grid-2">
        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">{t("sys.node")}</span>
            <div className="spacer" />
            {app.isAdmin && (
              <>
                <button className="btn btn-sm" onClick={() => setPower("reboot")}>
                  {t("sys.reboot")}
                </button>
                <button className="btn btn-sm btn-danger" onClick={() => setPower("poweroff")}>
                  {t("sys.poweroff")}
                </button>
              </>
            )}
          </div>
          <div className="panel-body">
            <dl className="kv">
              <dt>{t("ov.hostname")}</dt>
              <dd className="mono">{h?.hostname ?? "—"}</dd>
              <dt>{t("ov.version")}</dt>
              <dd className="mono">{data?.version ?? "—"}</dd>
              <dt>{t("ov.kernel")}</dt>
              <dd className="mono">{h?.kernel ?? "—"}</dd>
              <dt>{t("sys.cpus")}</dt>
              <dd className="mono">{h?.cpus ?? "—"}</dd>
              <dt>{t("ov.uptime")}</dt>
              <dd>{h ? duration(h.uptime_s) : "—"}</dd>
              <dt>{t("sys.memory")}</dt>
              <dd>
                {h ? `${bytes(h.mem_used)} / ${bytes(h.mem_total)}` : "—"}
                {h && <Meter value={(h.mem_used / Math.max(1, h.mem_total)) * 100} />}
              </dd>
              <dt>{t("ov.disk")}</dt>
              <dd>
                {h?.disk_total ? `${bytes(h.disk_used)} / ${bytes(h.disk_total)}` : "—"}
                {h?.disk_total ? <Meter value={(h.disk_used / h.disk_total) * 100} /> : null}
              </dd>
              <dt>{t("sys.time")}</dt>
              <dd className="mono">{datetime(h?.time)}</dd>
            </dl>
          </div>
        </div>

        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">{t("sys.dataplane")}</span>
          </div>
          <div className="panel-body">
            {a ? (
              <dl className="kv">
                <dt>{t("sys.agent")}</dt>
                <dd className="mono">{a.version}</dd>
                <dt>{t("ov.revision")}</dt>
                <dd className="mono">
                  r{a.revision} {a.confirmed_revision !== a.revision && <span className="faint">({t("sys.confirmed")} r{a.confirmed_revision})</span>}
                </dd>
                <dt>{t("sys.applied")}</dt>
                <dd className="mono">{datetime(a.applied)}</dd>
                <dt>{t("sys.generation")}</dt>
                <dd className="mono">{a.generation}</dd>
                <dt>XDP</dt>
                <dd className="mono">{a.xdp_mode}</dd>
                <dt>{t("sys.bootguard")}</dt>
                <dd>{a.boot_guard ? <span className="badge warn">{t("sys.bootguard.on")}</span> : <span className="badge ok">{t("sys.bootguard.off")}</span>}</dd>
                <dt>{t("sys.gateways")}</dt>
                <dd>
                  {(a.gateways ?? []).length === 0
                    ? "—"
                    : a.gateways!.map((g) => (
                        <div key={g.interface + g.address} className="mono">
                          {g.address} <span className="faint">{g.interface}</span>{" "}
                          {g.ok ? <span className="badge ok">{g.rtt_ms?.toFixed(1)} ms</span> : <span className="badge bad">{t("sys.gw.down")}</span>}
                        </div>
                      ))}
                </dd>
                <dt>NAT</dt>
                <dd className="mono">{a.stats.nat_fail > 0 ? <span style={{ color: "var(--bad)" }}>{t("sys.natfail", { n: a.stats.nat_fail })}</span> : "ok"}</dd>
                <dt>{t("sys.ringbuf")}</dt>
                <dd className="mono">{a.stats.ringbuf_full}</dd>
                <dt>{t("sys.frags")}</dt>
                <dd className="mono">{a.stats.frag_pass}</dd>
              </dl>
            ) : (
              <div className="note bad">{t("ov.agentdown")}</div>
            )}
          </div>
        </div>
      </div>

      {set && (
        <div className="section">
          <div className="section-head">
            <span className="section-title">{t("sys.settings")}</span>
            <div className="actions">
              {app.isAdmin && (
                <button
                  className="btn btn-primary btn-sm"
                  disabled={JSON.stringify(set) === JSON.stringify(orig)}
                  onClick={async () => {
                    try {
                      const s = await api.put<Settings>("/v1/settings", set);
                      setOrig(s);
                      toast(t("common.saved"));
                    } catch (e: any) {
                      toast(e.message, true);
                    }
                  }}
                >
                  {t("common.save")}
                </button>
              )}
            </div>
          </div>
          <div className="panel panel-body">
            <div className="form-grid" style={{ gridTemplateColumns: "repeat(3, minmax(0, 1fr))" }}>
              <Field label={t("sys.evret")} hint={t("sys.days.hint")}>
                <input className="input mono" type="number" disabled={!app.isAdmin} value={set.event_retention_days} onChange={(e) => setSet({ ...set, event_retention_days: Number(e.target.value) })} />
              </Field>
              <Field label={t("sys.trret")} hint={t("sys.days.hint")}>
                <input className="input mono" type="number" disabled={!app.isAdmin} value={set.traffic_retention_days} onChange={(e) => setSet({ ...set, traffic_retention_days: Number(e.target.value) })} />
              </Field>
              <Field label={t("sys.confirmdef")} hint={t("sys.confirmdef.hint")}>
                <input className="input mono" type="number" disabled={!app.isAdmin} value={set.default_confirm_seconds} onChange={(e) => setSet({ ...set, default_confirm_seconds: Number(e.target.value) })} />
              </Field>
              <Field label={t("sys.banner")} hint={t("sys.banner.hint")} className="span-2" >
                <textarea className="textarea" rows={3} disabled={!app.isAdmin} value={set.login_banner} onChange={(e) => setSet({ ...set, login_banner: e.target.value })} />
              </Field>
            </div>
          </div>
        </div>
      )}

      {app.isAdmin && <SSHPanel />}

      {app.isAdmin && (
        <div className="section">
          <div className="section-head">
            <span className="section-title">{t("sys.audit")}</span>
          </div>
          <div className="table-wrap" style={{ maxHeight: 480 }}>
            <table className="t">
              <thead>
                <tr>
                  <th>{t("col.time")}</th>
                  <th>{t("login.username")}</th>
                  <th>{t("sys.event")}</th>
                  <th>{t("sys.target")}</th>
                  <th>{t("sys.from")}</th>
                </tr>
              </thead>
              <tbody>
                {(audit.data ?? []).map((e) => (
                  <tr key={e.id}>
                    <td className="mono faint nowrap">{datetime(e.at)}</td>
                    <td className="mono">{e.username}</td>
                    <td>
                      <span className={e.action.endsWith("fail") ? "action-deny" : ""}>{e.action}</span>
                    </td>
                    <td className="mono">{e.target}</td>
                    <td className="mono faint">{e.remote_addr}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {power && (
        <Confirm
          title={power === "reboot" ? t("sys.reboot") : t("sys.poweroff")}
          text={power === "reboot" ? t("sys.reboot.text") : t("sys.poweroff.text")}
          action={power === "reboot" ? t("sys.reboot") : t("sys.poweroff")}
          danger
          onClose={() => setPower(null)}
          onConfirm={async () => {
            try {
              await api.post(`/v1/system/${power}`);
              toast(t("sys.requested"));
            } catch (e: any) {
              toast(e.message, true);
            }
          }}
        />
      )}
    </>
  );
}

function SSHPanel() {
  const toast = useToast();
  const [key, setKey] = useState("");
  const send = async (k: string) => {
    try {
      await api.post("/v1/system/ssh", { key: k });
      toast(k ? t("sys.ssh.on") : t("sys.ssh.off"));
      setKey("");
    } catch (e: any) {
      toast(e.message, true);
    }
  };
  return (
    <div className="section">
      <div className="section-head">
        <span className="section-title">{t("sys.ssh")}</span>
      </div>
      <div className="panel panel-body stack">
        <div className="muted">{t("sys.ssh.hint")}</div>
        <textarea className="textarea mono" rows={2} value={key} placeholder="ssh-ed25519 AAAA… user@host" onChange={(e) => setKey(e.target.value)} />
        <div className="row">
          <button className="btn btn-primary" disabled={!key.trim()} onClick={() => send(key.trim())}>
            {t("sys.ssh.enable")}
          </button>
          <button className="btn" onClick={() => send("")}>
            {t("sys.ssh.disable")}
          </button>
        </div>
      </div>
    </div>
  );
}
