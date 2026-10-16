import { useState } from "react";
import { useApp } from "../lib/state";
import type { Route } from "../lib/types";
import { errorsFor, fieldError } from "../lib/suggest";
import { t } from "../i18n";
import { Confirm, Drawer, EmptyState, Field, useToast } from "../components/ui";
import * as I from "../components/icons";

export function Routes() {
  const app = useApp();
  const toast = useToast();
  const cfg = app.candidate!.body;
  const [edit, setEdit] = useState<{ r: Route; index: number | null } | null>(null);
  const [del, setDel] = useState<number | null>(null);
  const dhcpWan = cfg.interfaces.filter((i) => i.dhcp && i.role === "wan");

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
        <div className="lead">{t("routes.lead")}</div>
        <div className="actions">
          {app.canEdit && (
            <button className="btn btn-primary" onClick={() => setEdit({ index: null, r: { destination: "", gateway: "", interface: "", metric: 0 } })}>
              <I.IconPlus />
              {t("routes.add")}
            </button>
          )}
        </div>
      </div>

      {dhcpWan.length > 0 && (
        <div className="note" style={{ marginBottom: 12 }}>
          {t("routes.dhcp", { ifaces: dhcpWan.map((i) => i.name).join(", ") })}
        </div>
      )}

      {cfg.routes.length === 0 ? (
        <EmptyState title={t("routes.empty")}>{t("routes.empty.hint")}</EmptyState>
      ) : (
        <div className="table-wrap">
          <table className="t">
            <thead>
              <tr>
                <th>{t("routes.dst")}</th>
                <th>{t("routes.gw")}</th>
                <th>{t("col.interface")}</th>
                <th className="num">{t("routes.metric")}</th>
                <th>{t("col.comment")}</th>
                <th className="act" />
              </tr>
            </thead>
            <tbody>
              {cfg.routes.map((r, i) => {
                const errs = errorsFor(app.validation, `routes[${i}]`);
                return (
                  <tr key={i} className="clickable" onClick={() => setEdit({ r: { ...r }, index: i })}>
                    <td className="mono">
                      {r.destination === "0.0.0.0/0" || r.destination === "::/0" ? (
                        <>
                          {r.destination} <span className="badge neutral">default</span>
                        </>
                      ) : (
                        r.destination
                      )}
                      {errs.length > 0 && (
                        <span className="badge bad" style={{ marginLeft: 6 }} title={errs.join("\n")}>
                          {t("common.error")}
                        </span>
                      )}
                    </td>
                    <td className="mono">{r.gateway || <span className="faint">—</span>}</td>
                    <td className="mono">{r.interface || <span className="faint">{t("routes.auto")}</span>}</td>
                    <td className="num mono">{r.metric ?? 0}</td>
                    <td className="muted">{r.comment}</td>
                    <td className="act" onClick={(e) => e.stopPropagation()}>
                      {app.canEdit && (
                        <button className="btn btn-ghost btn-sm btn-icon" onClick={() => setDel(i)}>
                          <I.IconTrash />
                        </button>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {edit && <RouteEditor initial={edit.r} index={edit.index} onClose={() => setEdit(null)} onSave={async (r) => {
        const ok = await save((c) => {
          if (edit.index === null) c.routes.push(r);
          else c.routes[edit.index] = r;
        });
        if (ok) setEdit(null);
      }} />}
      {del !== null && (
        <Confirm
          title={t("routes.delete")}
          text={<span className="mono">{cfg.routes[del]?.destination}</span>}
          action={t("common.delete")}
          danger
          onClose={() => setDel(null)}
          onConfirm={() => save((c) => void c.routes.splice(del, 1), t("common.deleted"))}
        />
      )}
    </>
  );
}

function RouteEditor({ initial, index, onClose, onSave }: { initial: Route; index: number | null; onClose: () => void; onSave: (r: Route) => void }) {
  const app = useApp();
  const cfg = app.candidate!.body;
  const [r, setR] = useState(initial);
  const err = (f: string) => (index !== null ? fieldError(app.validation, `routes[${index}].${f}`) : undefined);
  return (
    <Drawer
      title={index === null ? t("routes.new") : r.destination}
      onClose={onClose}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={onClose}>
            {t("common.cancel")}
          </button>
          {app.canEdit && (
            <button className="btn btn-primary" disabled={!r.destination || (!r.gateway && !r.interface)} onClick={() => onSave(r)}>
              {t("common.tocandidate")}
            </button>
          )}
        </>
      }
    >
      <div className="form-grid">
        <Field label={t("routes.dst")} hint={t("routes.dst.hint")} error={err("destination")} className="span-2">
          <input className="input mono" autoFocus value={r.destination} placeholder="10.20.0.0/16" onChange={(e) => setR({ ...r, destination: e.target.value.trim() })} />
        </Field>
        <Field label={t("routes.gw")} error={err("gateway")}>
          <input className="input mono" value={r.gateway ?? ""} placeholder="192.168.1.254" onChange={(e) => setR({ ...r, gateway: e.target.value.trim() })} />
        </Field>
        <Field label={t("col.interface")} error={err("interface")}>
          <select className="select" value={r.interface ?? ""} onChange={(e) => setR({ ...r, interface: e.target.value })}>
            <option value="">{t("routes.auto")}</option>
            {cfg.interfaces.map((i) => (
              <option key={i.name}>{i.name}</option>
            ))}
          </select>
        </Field>
        <Field label={t("routes.metric")} hint={t("routes.metric.hint")}>
          <input className="input mono" type="number" min={0} value={r.metric ?? 0} onChange={(e) => setR({ ...r, metric: Number(e.target.value) })} />
        </Field>
        <Field label={t("col.comment")}>
          <input className="input" value={r.comment ?? ""} onChange={(e) => setR({ ...r, comment: e.target.value })} />
        </Field>
      </div>
    </Drawer>
  );
}
