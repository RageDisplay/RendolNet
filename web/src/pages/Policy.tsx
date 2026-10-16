import { useMemo, useState } from "react";
import { api } from "../lib/api";
import { usePoll } from "../lib/router";
import { useApp } from "../lib/state";
import type { Rule } from "../lib/types";
import { count, uid } from "../lib/format";
import { addressSuggestions, errorsFor, serviceSuggestions, zoneSuggestions } from "../lib/suggest";
import { t } from "../i18n";
import { ActionLabel, Chips, Confirm, Drawer, Field, Seg, Switch, ZoneChip, cx, useToast } from "../components/ui";
import { TagInput } from "../components/TagInput";
import * as I from "../components/icons";

export function Policy() {
  const app = useApp();
  const toast = useToast();
  const cfg = app.candidate!.body;
  const [editing, setEditing] = useState<{ rule: Rule; index: number | null } | null>(null);
  const [del, setDel] = useState<number | null>(null);
  const [q, setQ] = useState("");
  const [drag, setDrag] = useState<number | null>(null);
  const [over, setOver] = useState<number | null>(null);
  const { data: counters } = usePoll(() => api.get<Record<string, { packets: number; bytes: number }>>("/v1/counters"), 5000);

  const shown = useMemo(() => {
    const s = q.trim().toLowerCase();
    return cfg.rules
      .map((r, i) => ({ r, i }))
      .filter(({ r }) => !s || JSON.stringify(r).toLowerCase().includes(s));
  }, [cfg.rules, q]);

  const save = async (fn: Parameters<typeof app.edit>[0], msg?: string) => {
    try {
      await app.edit(fn);
      if (msg) toast(msg);
    } catch (e: any) {
      toast(e.message, true);
    }
  };

  const move = (from: number, to: number) => {
    if (from === to || to < 0 || to >= cfg.rules.length) return;
    save((c) => {
      const [r] = c.rules.splice(from, 1);
      c.rules.splice(to, 0, r);
    });
  };

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("policy.lead")}</div>
        <div className="actions">
          <div className="search">
            <I.IconSearch />
            <input className="input" placeholder={t("common.filter")} value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
          {app.canEdit && (
            <button
              className="btn btn-primary"
              onClick={() =>
                setEditing({
                  index: null,
                  rule: { id: uid("r"), name: "", enabled: true, from: ["lan"], to: ["wan"], source: [], destination: [], services: [], action: "allow" },
                })
              }
            >
              <I.IconPlus />
              {t("policy.add")}
            </button>
          )}
        </div>
      </div>

      <div className="table-wrap">
        <table className="t">
          <thead>
            <tr>
              <th className="idx">#</th>
              <th style={{ width: 44 }} />
              <th>{t("col.name")}</th>
              <th>{t("col.zones")}</th>
              <th>{t("col.source")}</th>
              <th>{t("col.destination")}</th>
              <th>{t("col.services")}</th>
              <th>{t("col.action")}</th>
              <th className="num">{t("col.hits")}</th>
              <th className="act" />
            </tr>
          </thead>
          <tbody>
            {shown.map(({ r, i }) => {
              const errs = errorsFor(app.validation, `rules[${i}]`);
              const hits = counters?.[r.id];
              return (
                <tr
                  key={r.id}
                  className={cx("clickable", !r.enabled && "off", over === i && drag !== null && drag !== i && "drag-over")}
                  draggable={app.canEdit && !q}
                  onDragStart={() => setDrag(i)}
                  onDragOver={(e) => {
                    e.preventDefault();
                    setOver(i);
                  }}
                  onDragEnd={() => {
                    setDrag(null);
                    setOver(null);
                  }}
                  onDrop={() => {
                    if (drag !== null) move(drag, i);
                    setDrag(null);
                    setOver(null);
                  }}
                  onClick={() => setEditing({ rule: JSON.parse(JSON.stringify(r)), index: i })}
                >
                  <td className="idx">{i + 1}</td>
                  <td>
                    <Switch
                      checked={r.enabled}
                      disabled={!app.canEdit}
                      onChange={(v) => save((c) => void (c.rules[i].enabled = v))}
                    />
                  </td>
                  <td>
                    <div style={{ fontWeight: 500 }}>
                      {r.name}
                      {r.log && <span className="badge neutral" style={{ marginLeft: 6 }}>log</span>}
                      {errs.length > 0 && (
                        <span className="badge bad" style={{ marginLeft: 6 }} title={errs.join("\n")}>
                          {t("common.error")}
                        </span>
                      )}
                    </div>
                    {r.comment && <div className="faint" style={{ fontSize: 12 }}>{r.comment}</div>}
                  </td>
                  <td>
                    <span className="chips" style={{ alignItems: "center" }}>
                      {r.from.map((z) => (
                        <ZoneChip key={z} name={z} />
                      ))}
                      <span className="arrow">→</span>
                      {r.to.map((z) => (
                        <ZoneChip key={z} name={z} />
                      ))}
                    </span>
                  </td>
                  <td>
                    <Chips items={r.source} mono />
                  </td>
                  <td>
                    <Chips items={r.destination} mono />
                  </td>
                  <td>
                    <Chips items={r.services} mono />
                  </td>
                  <td>
                    <ActionLabel action={r.action} />
                  </td>
                  <td className="num mono" title={hits ? `${hits.packets} pkt` : ""}>
                    {hits ? count(hits.packets) : <span className="faint">—</span>}
                  </td>
                  <td className="act" onClick={(e) => e.stopPropagation()}>
                    {app.canEdit && (
                      <span className="row" style={{ gap: 2 }}>
                        <button className="btn btn-ghost btn-sm btn-icon" title={t("common.up")} onClick={() => move(i, i - 1)}>
                          <I.IconUp />
                        </button>
                        <button className="btn btn-ghost btn-sm btn-icon" title={t("common.down")} onClick={() => move(i, i + 1)}>
                          <I.IconDown />
                        </button>
                        <button className="btn btn-ghost btn-sm btn-icon" title={t("common.delete")} onClick={() => setDel(i)}>
                          <I.IconTrash />
                        </button>
                      </span>
                    )}
                  </td>
                </tr>
              );
            })}
            <tr className="off">
              <td className="idx">—</td>
              <td />
              <td>
                <div style={{ fontWeight: 500 }}>{t("policy.implicit.mgmt")}</div>
                <div className="faint" style={{ fontSize: 12 }}>{t("policy.implicit.mgmt.hint")}</div>
              </td>
              <td>
                <span className="chips">
                  <span className="chip mono">{cfg.management.interface || "—"}</span>
                  <span className="arrow">→</span>
                  <ZoneChip name="self" />
                </span>
              </td>
              <td>
                <Chips items={cfg.management.allow_from} mono />
              </td>
              <td className="faint">—</td>
              <td>
                <Chips items={cfg.management.ports.map((p) => `tcp/${p}`)} mono />
              </td>
              <td>
                <ActionLabel action="allow" />
              </td>
              <td />
              <td />
            </tr>
            <tr className="off">
              <td className="idx">—</td>
              <td />
              <td>
                <div style={{ fontWeight: 500 }}>{t("policy.implicit.default")}</div>
                <div className="faint" style={{ fontSize: 12 }}>{t("policy.implicit.default.hint")}</div>
              </td>
              <td>
                <span className="chips">
                  <span className="faint">{t("common.any")}</span>
                </span>
              </td>
              <td className="faint">{t("common.any")}</td>
              <td className="faint">{t("common.any")}</td>
              <td className="faint">{t("common.any")}</td>
              <td>
                <ActionLabel action="deny" />
              </td>
              <td />
              <td />
            </tr>
          </tbody>
        </table>
      </div>

      {editing && (
        <RuleEditor
          initial={editing.rule}
          isNew={editing.index === null}
          onClose={() => setEditing(null)}
          onSave={async (r) => {
            await save((c) => {
              if (editing.index === null) c.rules.push(r);
              else c.rules[editing.index] = r;
            }, t("common.saved"));
            setEditing(null);
          }}
        />
      )}
      {del !== null && (
        <Confirm
          title={t("policy.delete.title")}
          text={t("policy.delete.text", { name: cfg.rules[del]?.name ?? "" })}
          action={t("common.delete")}
          danger
          onClose={() => setDel(null)}
          onConfirm={() => save((c) => void c.rules.splice(del, 1), t("common.deleted"))}
        />
      )}
    </>
  );
}

function RuleEditor({ initial, isNew, onClose, onSave }: { initial: Rule; isNew: boolean; onClose: () => void; onSave: (r: Rule) => void }) {
  const app = useApp();
  const cfg = app.candidate!.body;
  const [r, setR] = useState<Rule>(initial);
  const set = <K extends keyof Rule>(k: K, v: Rule[K]) => setR((x) => ({ ...x, [k]: v }));
  const ro = !app.canEdit;

  return (
    <Drawer
      title={isNew ? t("policy.new") : r.name || t("policy.rule")}
      onClose={onClose}
      footer={
        <>
          <span className="faint mono" style={{ alignSelf: "center", fontSize: 12 }}>
            {r.id}
          </span>
          <div className="spacer" />
          <button className="btn" onClick={onClose}>
            {t("common.cancel")}
          </button>
          {!ro && (
            <button className="btn btn-primary" disabled={!r.name.trim() || r.from.length === 0 || r.to.length === 0} onClick={() => onSave(r)}>
              {t("common.tocandidate")}
            </button>
          )}
        </>
      }
    >
      <div className="form-grid">
        <Field label={t("col.name")} className="span-2">
          <input className="input" autoFocus value={r.name} onChange={(e) => set("name", e.target.value)} />
        </Field>
        <Field label={t("policy.from")} hint={t("policy.from.hint")}>
          <TagInput value={r.from} onChange={(v) => set("from", v)} suggestions={zoneSuggestions(cfg, false)} strict mono={false} />
        </Field>
        <Field label={t("policy.to")} hint={t("policy.to.hint")}>
          <TagInput value={r.to} onChange={(v) => set("to", v)} suggestions={zoneSuggestions(cfg, true)} strict mono={false} />
        </Field>
        <Field label={t("col.source")} hint={t("policy.addr.hint")}>
          <TagInput value={r.source ?? []} onChange={(v) => set("source", v)} suggestions={addressSuggestions(cfg)} placeholder={t("common.any")} />
        </Field>
        <Field label={t("col.destination")} hint={t("policy.addr.hint")}>
          <TagInput value={r.destination ?? []} onChange={(v) => set("destination", v)} suggestions={addressSuggestions(cfg)} placeholder={t("common.any")} />
        </Field>
        <Field label={t("col.services")} hint={t("policy.svc.hint")} className="span-2">
          <TagInput value={r.services ?? []} onChange={(v) => set("services", v)} suggestions={serviceSuggestions(cfg)} placeholder={t("common.any")} />
        </Field>
        <Field label={t("col.action")}>
          <Seg
            value={r.action}
            onChange={(v) => set("action", v)}
            options={[
              { value: "allow", label: <span className="action-allow">{t("action.allow")}</span> },
              { value: "deny", label: <span className="action-deny">{t("action.deny")}</span> },
              { value: "reject", label: <span className="action-reject">{t("action.reject")}</span> },
            ]}
          />
        </Field>
        <Field label={t("policy.options")}>
          <div className="row" style={{ gap: 18, height: 30 }}>
            <Switch checked={r.enabled} onChange={(v) => set("enabled", v)} label={t("common.enabled")} />
            <Switch checked={!!r.log} onChange={(v) => set("log", v)} label={t("policy.log")} />
          </div>
        </Field>
        <Field label={t("col.comment")} className="span-2">
          <input className="input" value={r.comment ?? ""} onChange={(e) => set("comment", e.target.value)} />
        </Field>
      </div>
      {r.action === "reject" && (
        <div className="note" style={{ marginTop: 16 }}>
          {t("policy.reject.note")}
        </div>
      )}
    </Drawer>
  );
}
