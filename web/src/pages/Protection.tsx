import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { usePoll } from "../lib/router";
import { useApp } from "../lib/state";
import type { AgentStatus, Config } from "../lib/types";
import { count } from "../lib/format";
import { fieldError } from "../lib/suggest";
import { t } from "../i18n";
import { Field, Switch, useToast } from "../components/ui";
import { TagInput } from "../components/TagInput";

const lines = (s: string) => s.split(/[\s,;]+/).map((x) => x.trim()).filter(Boolean);

export function Protection() {
  const app = useApp();
  const toast = useToast();
  const cfg = app.candidate!.body;
  const { data: sys } = usePoll(() => api.get<{ agent?: AgentStatus }>("/v1/system"), 5000);
  const [p, setP] = useState(cfg.protection);
  const [m, setM] = useState(cfg.management);
  const [block, setBlock] = useState((cfg.protection.blocklist ?? []).join("\n"));

  useEffect(() => {
    setP(cfg.protection);
    setM(cfg.management);
    setBlock((cfg.protection.blocklist ?? []).join("\n"));
  }, [cfg]);

  const changed =
    JSON.stringify({ ...p, blocklist: lines(block) }) !== JSON.stringify({ ...cfg.protection, blocklist: cfg.protection.blocklist ?? [] }) ||
    JSON.stringify(m) !== JSON.stringify(cfg.management);

  const save = async () => {
    try {
      await app.edit((c: Config) => {
        c.protection = { ...p, blocklist: lines(block) };
        c.management = m;
      });
      toast(t("common.saved"));
    } catch (e: any) {
      toast(e.message, true);
    }
  };

  const st = sys?.agent?.stats;
  const err = (f: string) => fieldError(app.validation, f);

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("prot.lead")}</div>
        <div className="actions">
          {app.canEdit && (
            <button className="btn btn-primary" disabled={!changed} onClick={save}>
              {t("common.tocandidate")}
            </button>
          )}
        </div>
      </div>

      <div className="grid grid-3">
        <div className="panel stat">
          <div className="stat-label">{t("prot.blocked")}</div>
          <div className="stat-value">{count(st?.xdp_blocklist ?? 0)}</div>
          <div className="stat-foot">{t("prot.blocked.foot")}</div>
        </div>
        <div className="panel stat">
          <div className="stat-label">{t("prot.syn")}</div>
          <div className="stat-value">{count(st?.xdp_synflood ?? 0)}</div>
          <div className="stat-foot">{t("prot.syn.foot")}</div>
        </div>
        <div className="panel stat">
          <div className="stat-label">{t("prot.rst")}</div>
          <div className="stat-value">{count(st?.reject_sent ?? 0)}</div>
          <div className="stat-foot">{t("prot.rst.foot")}</div>
        </div>
      </div>

      <div className="grid grid-2 section">
        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">{t("prot.xdp")}</span>
          </div>
          <div className="panel-body stack">
            <Field label={t("prot.blocklist")} hint={t("prot.blocklist.hint")} error={err("protection.blocklist")}>
              <textarea className="textarea mono" rows={8} value={block} onChange={(e) => setBlock(e.target.value)} placeholder={"192.0.2.0/24\n198.51.100.7"} />
            </Field>
            <div className="form-grid">
              <Field label={t("prot.synrate")} hint={t("prot.synrate.hint")}>
                <input className="input mono" type="number" min={0} value={p.syn_rate || ""} placeholder="0" onChange={(e) => setP({ ...p, syn_rate: Number(e.target.value) || 0 })} />
              </Field>
              <Field label={t("prot.synburst")} hint={t("prot.synburst.hint")}>
                <input className="input mono" type="number" min={0} value={p.syn_burst || ""} placeholder={String(p.syn_rate || 0)} onChange={(e) => setP({ ...p, syn_burst: Number(e.target.value) || 0 })} />
              </Field>
            </div>
            <Switch checked={p.log_default_deny} onChange={(v) => setP({ ...p, log_default_deny: v })} label={t("prot.logdefault")} />
          </div>
        </div>

        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">{t("prot.mgmt")}</span>
          </div>
          <div className="panel-body stack">
            <div className="note">{t("prot.mgmt.note")}</div>
            <Field label={t("col.interface")} error={err("management.interface")}>
              <select className="select" value={m.interface} onChange={(e) => setM({ ...m, interface: e.target.value })}>
                <option value="">—</option>
                {cfg.interfaces
                  .filter((i) => i.role === "mgmt")
                  .map((i) => (
                    <option key={i.name}>{i.name}</option>
                  ))}
              </select>
            </Field>
            <Field label={t("prot.allowfrom")} hint={t("prot.allowfrom.hint")} error={err("management.allow_from")}>
              <TagInput value={m.allow_from ?? []} onChange={(v) => setM({ ...m, allow_from: v })} placeholder={t("common.any")} />
            </Field>
            <Field label={t("prot.ports")} error={err("management.ports")}>
              <TagInput
                value={m.ports.map(String)}
                onChange={(v) => setM({ ...m, ports: v.map(Number).filter((n) => n > 0 && n < 65536) })}
                placeholder="22, 443"
              />
            </Field>
          </div>
        </div>
      </div>
    </>
  );
}
