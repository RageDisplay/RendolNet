import { useState } from "react";
import { useApp } from "../lib/state";
import type { DNATRule, SNATRule } from "../lib/types";
import { uid } from "../lib/format";
import { addressSuggestions, errorsFor } from "../lib/suggest";
import { t } from "../i18n";
import { Chips, Confirm, Drawer, Field, Seg, Switch, cx, useToast } from "../components/ui";
import { TagInput } from "../components/TagInput";
import * as I from "../components/icons";

type Tab = "source" | "destination";

export function Nat() {
  const app = useApp();
  const toast = useToast();
  const cfg = app.candidate!.body;
  const [tab, setTab] = useState<Tab>("source");
  const [snat, setSnat] = useState<{ r: SNATRule; index: number | null } | null>(null);
  const [dnat, setDnat] = useState<{ r: DNATRule; index: number | null } | null>(null);
  const [del, setDel] = useState<{ tab: Tab; index: number } | null>(null);
  const wan = cfg.interfaces.find((i) => i.role === "wan")?.name ?? cfg.interfaces[0]?.name ?? "";

  const save = async (fn: Parameters<typeof app.edit>[0], msg = t("common.saved")) => {
    try {
      await app.edit(fn);
      toast(msg);
      return true;
    } catch (e: any) {
      toast(e.message, true);
      return false;
    }
  };

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("nat.lead")}</div>
        <div className="actions">
          {app.canEdit && tab === "source" && (
            <button className="btn btn-primary" onClick={() => setSnat({ index: null, r: { id: uid("snat"), name: "", enabled: true, interface: wan, source: [] } })}>
              <I.IconPlus />
              {t("nat.addsnat")}
            </button>
          )}
          {app.canEdit && tab === "destination" && (
            <button
              className="btn btn-primary"
              onClick={() => setDnat({ index: null, r: { id: uid("dnat"), name: "", enabled: true, interface: wan, proto: "tcp", port: 0, to_address: "", allow_policy: true } })}
            >
              <I.IconPlus />
              {t("nat.adddnat")}
            </button>
          )}
        </div>
      </div>

      <div className="tabs">
        <button className={cx(tab === "source" && "on")} onClick={() => setTab("source")}>
          {t("nat.snat")}
          <span className="count">{cfg.nat.source.length}</span>
        </button>
        <button className={cx(tab === "destination" && "on")} onClick={() => setTab("destination")}>
          {t("nat.dnat")}
          <span className="count">{cfg.nat.destination.length}</span>
        </button>
      </div>

      {tab === "source" ? (
        <div className="table-wrap">
          <table className="t">
            <thead>
              <tr>
                <th style={{ width: 44 }} />
                <th>{t("col.name")}</th>
                <th>{t("nat.out")}</th>
                <th>{t("col.source")}</th>
                <th>{t("nat.translate")}</th>
                <th className="act" />
              </tr>
            </thead>
            <tbody>
              {cfg.nat.source.map((r, i) => (
                <tr key={r.id} className={cx("clickable", !r.enabled && "off")} onClick={() => setSnat({ r: JSON.parse(JSON.stringify(r)), index: i })}>
                  <td>
                    <Switch checked={r.enabled} disabled={!app.canEdit} onChange={(v) => save((c) => void (c.nat.source[i].enabled = v))} />
                  </td>
                  <td>
                    <span style={{ fontWeight: 500 }}>{r.name}</span>
                    {errorsFor(app.validation, `nat.source[${i}]`).length > 0 && <span className="badge bad" style={{ marginLeft: 6 }}>{t("common.error")}</span>}
                  </td>
                  <td className="mono">{r.interface}</td>
                  <td>
                    <Chips items={r.source} mono />
                  </td>
                  <td className="mono">{r.translate || <span className="muted">{t("nat.masquerade")}</span>}</td>
                  <td className="act" onClick={(e) => e.stopPropagation()}>
                    {app.canEdit && (
                      <button className="btn btn-ghost btn-sm btn-icon" onClick={() => setDel({ tab: "source", index: i })}>
                        <I.IconTrash />
                      </button>
                    )}
                  </td>
                </tr>
              ))}
              {cfg.nat.source.length === 0 && (
                <tr>
                  <td colSpan={6} className="empty">
                    {t("nat.snat.empty")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="table-wrap">
          <table className="t">
            <thead>
              <tr>
                <th style={{ width: 44 }} />
                <th>{t("col.name")}</th>
                <th>{t("nat.in")}</th>
                <th>{t("nat.external")}</th>
                <th>{t("nat.internal")}</th>
                <th>{t("nat.policy")}</th>
                <th className="act" />
              </tr>
            </thead>
            <tbody>
              {cfg.nat.destination.map((r, i) => (
                <tr key={r.id} className={cx("clickable", !r.enabled && "off")} onClick={() => setDnat({ r: { ...r }, index: i })}>
                  <td>
                    <Switch checked={r.enabled} disabled={!app.canEdit} onChange={(v) => save((c) => void (c.nat.destination[i].enabled = v))} />
                  </td>
                  <td>
                    <span style={{ fontWeight: 500 }}>{r.name}</span>
                    {errorsFor(app.validation, `nat.destination[${i}]`).length > 0 && <span className="badge bad" style={{ marginLeft: 6 }}>{t("common.error")}</span>}
                  </td>
                  <td className="mono">{r.interface}</td>
                  <td className="mono">
                    {r.proto}/{r.port}
                  </td>
                  <td className="mono">
                    {r.to_address}:{r.to_port || r.port}
                  </td>
                  <td>{r.allow_policy ? <span className="badge ok">{t("nat.auto")}</span> : <span className="faint">{t("nat.byrules")}</span>}</td>
                  <td className="act" onClick={(e) => e.stopPropagation()}>
                    {app.canEdit && (
                      <button className="btn btn-ghost btn-sm btn-icon" onClick={() => setDel({ tab: "destination", index: i })}>
                        <I.IconTrash />
                      </button>
                    )}
                  </td>
                </tr>
              ))}
              {cfg.nat.destination.length === 0 && (
                <tr>
                  <td colSpan={7} className="empty">
                    {t("nat.dnat.empty")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {snat && (
        <SnatEditor
          initial={snat.r}
          onClose={() => setSnat(null)}
          onSave={async (r) => {
            if (await save((c) => (snat.index === null ? void c.nat.source.push(r) : void (c.nat.source[snat.index] = r)))) setSnat(null);
          }}
        />
      )}
      {dnat && (
        <DnatEditor
          initial={dnat.r}
          onClose={() => setDnat(null)}
          onSave={async (r) => {
            if (await save((c) => (dnat.index === null ? void c.nat.destination.push(r) : void (c.nat.destination[dnat.index] = r)))) setDnat(null);
          }}
        />
      )}
      {del && (
        <Confirm
          title={t("nat.delete")}
          text={(del.tab === "source" ? cfg.nat.source : cfg.nat.destination)[del.index]?.name}
          action={t("common.delete")}
          danger
          onClose={() => setDel(null)}
          onConfirm={() => save((c) => void (del.tab === "source" ? c.nat.source : c.nat.destination).splice(del.index, 1), t("common.deleted"))}
        />
      )}
    </>
  );
}

function Footer({ onClose, onSave, ok }: { onClose: () => void; onSave: () => void; ok: boolean }) {
  const app = useApp();
  return (
    <>
      <div className="spacer" />
      <button className="btn" onClick={onClose}>
        {t("common.cancel")}
      </button>
      {app.canEdit && (
        <button className="btn btn-primary" disabled={!ok} onClick={onSave}>
          {t("common.tocandidate")}
        </button>
      )}
    </>
  );
}

function SnatEditor({ initial, onClose, onSave }: { initial: SNATRule; onClose: () => void; onSave: (r: SNATRule) => void }) {
  const cfg = useApp().candidate!.body;
  const [r, setR] = useState(initial);
  return (
    <Drawer title={r.name || t("nat.addsnat")} onClose={onClose} footer={<Footer onClose={onClose} onSave={() => onSave(r)} ok={!!r.name && !!r.interface} />}>
      <div className="form-grid">
        <Field label={t("col.name")} className="span-2">
          <input className="input" autoFocus value={r.name} onChange={(e) => setR({ ...r, name: e.target.value })} />
        </Field>
        <Field label={t("nat.out")}>
          <select className="select" value={r.interface} onChange={(e) => setR({ ...r, interface: e.target.value })}>
            {cfg.interfaces.map((i) => (
              <option key={i.name}>{i.name}</option>
            ))}
          </select>
        </Field>
        <Field label={t("nat.translate")} hint={t("nat.translate.hint")}>
          <input className="input mono" value={r.translate ?? ""} placeholder={t("nat.masquerade")} onChange={(e) => setR({ ...r, translate: e.target.value.trim() })} />
        </Field>
        <Field label={t("col.source")} hint={t("nat.src.hint")} className="span-2">
          <TagInput value={r.source ?? []} onChange={(v) => setR({ ...r, source: v })} suggestions={addressSuggestions(cfg)} placeholder={t("common.any")} />
        </Field>
        <Field label={t("col.comment")} className="span-2">
          <input className="input" value={r.comment ?? ""} onChange={(e) => setR({ ...r, comment: e.target.value })} />
        </Field>
      </div>
    </Drawer>
  );
}

function DnatEditor({ initial, onClose, onSave }: { initial: DNATRule; onClose: () => void; onSave: (r: DNATRule) => void }) {
  const cfg = useApp().candidate!.body;
  const [r, setR] = useState(initial);
  const ok = !!r.name && r.port > 0 && !!r.to_address;
  return (
    <Drawer title={r.name || t("nat.adddnat")} onClose={onClose} footer={<Footer onClose={onClose} onSave={() => onSave(r)} ok={ok} />}>
      <div className="form-grid">
        <Field label={t("col.name")} className="span-2">
          <input className="input" autoFocus value={r.name} onChange={(e) => setR({ ...r, name: e.target.value })} />
        </Field>
        <Field label={t("nat.in")}>
          <select className="select" value={r.interface} onChange={(e) => setR({ ...r, interface: e.target.value })}>
            {cfg.interfaces.map((i) => (
              <option key={i.name}>{i.name}</option>
            ))}
          </select>
        </Field>
        <Field label={t("col.proto")}>
          <Seg value={r.proto} onChange={(v) => setR({ ...r, proto: v })} options={[{ value: "tcp", label: "TCP" }, { value: "udp", label: "UDP" }]} />
        </Field>
        <Field label={t("nat.extport")}>
          <input className="input mono" type="number" min={1} max={65535} value={r.port || ""} onChange={(e) => setR({ ...r, port: Number(e.target.value) })} />
        </Field>
        <div />
        <Field label={t("nat.toaddr")}>
          <input className="input mono" value={r.to_address} placeholder="192.168.1.10" onChange={(e) => setR({ ...r, to_address: e.target.value.trim() })} />
        </Field>
        <Field label={t("nat.toport")} hint={t("nat.toport.hint")}>
          <input className="input mono" type="number" min={0} max={65535} value={r.to_port || ""} placeholder={String(r.port || "")} onChange={(e) => setR({ ...r, to_port: Number(e.target.value) || 0 })} />
        </Field>
        <Field label={t("nat.policy")} className="span-2" hint={t("nat.policy.hint")}>
          <div style={{ height: 30, display: "flex", alignItems: "center", gap: 18 }}>
            <Switch checked={r.allow_policy} onChange={(v) => setR({ ...r, allow_policy: v })} label={t("nat.auto")} />
            <Switch checked={r.enabled} onChange={(v) => setR({ ...r, enabled: v })} label={t("common.enabled")} />
          </div>
        </Field>
        <Field label={t("col.comment")} className="span-2">
          <input className="input" value={r.comment ?? ""} onChange={(e) => setR({ ...r, comment: e.target.value })} />
        </Field>
      </div>
      <div className="note" style={{ marginTop: 16 }}>
        {t("nat.hairpin")}
      </div>
    </Drawer>
  );
}
