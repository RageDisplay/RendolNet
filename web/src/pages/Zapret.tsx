import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { usePoll } from "../lib/router";
import { useApp } from "../lib/state";
import type { Strategy, ZapretStatus } from "../lib/types";
import { datetime } from "../lib/format";
import { fieldError } from "../lib/suggest";
import { t } from "../i18n";
import { Field, Switch, useToast } from "../components/ui";

const lines = (s: string) => s.split(/[\s,;]+/).map((x) => x.trim()).filter(Boolean);

export function Zapret() {
  const app = useApp();
  const toast = useToast();
  const cfg = app.candidate!.body;
  const { data: strategies } = usePoll(() => api.get<Strategy[]>("/v1/zapret/strategies"), 0);
  const { data: st } = usePoll(() => api.get<ZapretStatus>("/v1/zapret/status"), 5000);
  const [z, setZ] = useState(cfg.zapret);
  const [hosts, setHosts] = useState((cfg.zapret.hosts ?? []).join("\n"));
  const [ips, setIps] = useState((cfg.zapret.ips ?? []).join("\n"));
  const wans = cfg.interfaces.filter((i) => i.role === "wan");

  useEffect(() => {
    setZ(cfg.zapret);
    setHosts((cfg.zapret.hosts ?? []).join("\n"));
    setIps((cfg.zapret.ips ?? []).join("\n"));
  }, [cfg]);

  const next = { ...z, hosts: lines(hosts), ips: lines(ips) };
  const changed = JSON.stringify(next) !== JSON.stringify({ ...cfg.zapret, hosts: cfg.zapret.hosts ?? [], ips: cfg.zapret.ips ?? [] });
  const current = strategies?.find((s) => s.name === z.strategy);

  const save = async () => {
    try {
      await app.edit((c) => void (c.zapret = next));
      toast(t("common.saved"));
    } catch (e: any) {
      toast(e.message, true);
    }
  };

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("zap.lead")}</div>
        <div className="actions">
          {app.canEdit && (
            <button className="btn btn-primary" disabled={!changed} onClick={save}>
              {t("common.tocandidate")}
            </button>
          )}
        </div>
      </div>

      <div className="grid" style={{ gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1.2fr)" }}>
        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">{t("zap.settings")}</span>
            <div className="spacer" />
            {!cfg.zapret.enabled ? (
              <span className="badge neutral">{t("common.off")}</span>
            ) : st?.running ? (
              <span className="badge ok">{t("zap.running")}</span>
            ) : (
              <span className="badge bad">{t("zap.stopped")}</span>
            )}
          </div>
          <div className="panel-body stack">
            {cfg.zapret.enabled && st?.error && <div className="note bad">{st.error}</div>}
            {cfg.zapret.enabled && st?.running && (
              <div className="faint" style={{ fontSize: 12 }}>
                {t("zap.since", { s: st.strategy ?? "", when: datetime(st.started) })}
                {st.restarts > 0 && " · " + t("zap.restarts", { n: st.restarts })}
              </div>
            )}
            <Switch checked={z.enabled} onChange={(v) => setZ({ ...z, enabled: v, interface: z.interface || wans[0]?.name })} label={t("zap.enable")} />
            <Field label={t("zap.iface")} error={fieldError(app.validation, "zapret.interface")}>
              <select className="select" value={z.interface ?? ""} onChange={(e) => setZ({ ...z, interface: e.target.value })}>
                <option value="">—</option>
                {wans.map((i) => (
                  <option key={i.name}>{i.name}</option>
                ))}
              </select>
            </Field>
            <Field label={t("zap.strategy")} hint={t("zap.strategy.hint")} error={fieldError(app.validation, "zapret.strategy")}>
              <select className="select" value={z.strategy ?? ""} onChange={(e) => setZ({ ...z, strategy: e.target.value })}>
                <option value="">—</option>
                {(strategies ?? []).map((s) => (
                  <option key={s.name} value={s.name} disabled={!s.compatible}>
                    {s.name}
                    {s.compatible ? "" : ` — ${t("zap.incompatible")}`}
                  </option>
                ))}
              </select>
            </Field>
            <Field label={t("zap.fooling")} hint={t("zap.fooling.hint")} error={fieldError(app.validation, "zapret.fooling")}>
              <select className="select" value={z.fooling ?? ""} onChange={(e) => setZ({ ...z, fooling: e.target.value || undefined })}>
                <option value="">{t("zap.fooling.keep")}</option>
                <option>badseq</option>
                <option>md5sig</option>
              </select>
            </Field>
            <Field label={t("zap.hosts")} hint={t("zap.hosts.hint")}>
              <textarea className="textarea mono" rows={5} value={hosts} onChange={(e) => setHosts(e.target.value)} placeholder="example.com" />
            </Field>
            <Field label={t("zap.ips")} hint={t("zap.ips.hint")} error={fieldError(app.validation, "zapret.ips")}>
              <textarea className="textarea mono" rows={3} value={ips} onChange={(e) => setIps(e.target.value)} placeholder="203.0.113.0/24" />
            </Field>
          </div>
        </div>

        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">{current ? current.name : t("zap.args")}</span>
            {current && <span className="faint mono" style={{ fontSize: 12 }}>{current.source}</span>}
          </div>
          <div className="panel-body stack">
            {!strategies || strategies.length === 0 ? (
              <div className="note warn">{t("zap.nostrategies")}</div>
            ) : current ? (
              <>
                <div className="log" style={{ maxHeight: 420 }}>{current.args.join("\n")}</div>
                {current.problems && current.problems.length > 0 && (
                  <div className="note warn">
                    {current.problems.map((p) => (
                      <div key={p}>{p}</div>
                    ))}
                  </div>
                )}
              </>
            ) : (
              <div className="faint">{t("zap.pick")}</div>
            )}
            <div className="note">{t("zap.how")}</div>
          </div>
        </div>
      </div>
    </>
  );
}
