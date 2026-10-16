import { useState } from "react";
import { api } from "../lib/api";
import { usePoll } from "../lib/router";
import { useApp } from "../lib/state";
import type { Iface, Link, Zone } from "../lib/types";
import { bytes } from "../lib/format";
import { errorsFor, fieldError } from "../lib/suggest";
import { t } from "../i18n";
import { Confirm, Drawer, Field, Modal, Seg, Switch, ZoneChip, cx, useToast } from "../components/ui";
import { TagInput } from "../components/TagInput";
import * as I from "../components/icons";

export function Interfaces() {
  const app = useApp();
  const toast = useToast();
  const cfg = app.candidate!.body;
  const { data: links } = usePoll(() => api.get<Link[]>("/v1/links"), 10000);
  const [edit, setEdit] = useState<{ iface: Iface; index: number | null } | null>(null);
  const [zoneEdit, setZoneEdit] = useState<{ zone: Zone; index: number | null } | null>(null);
  const [delZone, setDelZone] = useState<number | null>(null);

  const byName = new Map((links ?? []).map((l) => [l.name, l]));
  const configured = new Set(cfg.interfaces.map((i) => i.name));
  const free = (links ?? []).filter((l) => !configured.has(l.name) && l.kind !== "vlan");

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

  const zoneUse = (z: string) => ({
    ifaces: cfg.interfaces.filter((i) => i.zone === z).length,
    rules: cfg.rules.filter((r) => r.from.includes(z) || r.to.includes(z)).length,
  });

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("if.lead")}</div>
        <div className="actions">
          {app.canEdit && (
            <button
              className="btn"
              onClick={() =>
                setEdit({
                  index: null,
                  iface: { name: "", role: "lan", zone: cfg.zones[0]?.name ?? "", enabled: true, addresses: [], vlan: { parent: cfg.interfaces.find((i) => !i.vlan && i.role !== "mgmt")?.name ?? "", id: 10 } },
                })
              }
            >
              <I.IconPlus />
              {t("if.addvlan")}
            </button>
          )}
        </div>
      </div>

      <div className="table-wrap">
        <table className="t">
          <thead>
            <tr>
              <th>{t("col.interface")}</th>
              <th>{t("col.role")}</th>
              <th>{t("col.zone")}</th>
              <th>{t("col.addressing")}</th>
              <th>{t("col.state")}</th>
              <th>MAC</th>
              <th className="num">MTU</th>
              <th className="num">{t("if.traffic")}</th>
            </tr>
          </thead>
          <tbody>
            {cfg.interfaces.map((it, i) => {
              const l = byName.get(it.name);
              const errs = errorsFor(app.validation, `interfaces[${i}]`);
              return (
                <tr key={it.name} className={cx("clickable", !it.enabled && "off")} onClick={() => setEdit({ iface: JSON.parse(JSON.stringify(it)), index: i })}>
                  <td>
                    <span className="mono" style={{ fontWeight: 500 }}>
                      {it.name}
                    </span>
                    {it.vlan && <span className="faint"> · VLAN {it.vlan.id}</span>}
                    {errs.length > 0 && (
                      <span className="badge bad" style={{ marginLeft: 6 }} title={errs.join("\n")}>
                        {t("common.error")}
                      </span>
                    )}
                    {it.description && <div className="faint" style={{ fontSize: 12 }}>{it.description}</div>}
                  </td>
                  <td>
                    <span className={cx("badge", it.role === "wan" ? "warn" : it.role === "mgmt" ? "info" : "neutral")}>{it.role}</span>
                  </td>
                  <td>
                    <ZoneChip name={it.zone} />
                  </td>
                  <td className="mono">
                    {it.role === "mgmt" ? (
                      <span className="faint">{t("if.mgmt.os")}</span>
                    ) : it.dhcp ? (
                      <>
                        DHCP{" "}
                        <span className="faint">
                          {l?.addresses.filter((a) => !a.startsWith("fe80") && a.includes(".")).join(", ")}
                        </span>
                      </>
                    ) : (
                      (it.addresses ?? []).join(", ") || <span className="faint">—</span>
                    )}
                  </td>
                  <td>
                    {l ? (
                      <span className="row" style={{ gap: 6 }}>
                        <span className={cx("dot", l.carrier ? "ok" : l.up ? "warn" : "")} />
                        {l.carrier ? t("link.up") : l.up ? t("link.nocarrier") : t("link.down")}
                      </span>
                    ) : (
                      <span className="badge bad">{t("if.missing")}</span>
                    )}
                  </td>
                  <td className="mono faint">{l?.mac ?? "—"}</td>
                  <td className="num mono">{it.mtu || l?.mtu || "—"}</td>
                  <td className="num mono faint">{l ? `${bytes(l.rx_bytes)} / ${bytes(l.tx_bytes)}` : "—"}</td>
                </tr>
              );
            })}
            {free.map((l) => (
              <tr key={l.name} className="off">
                <td className="mono">{l.name}</td>
                <td colSpan={4} className="faint">
                  {t("if.free")}
                </td>
                <td className="mono faint">{l.mac}</td>
                <td className="num mono">{l.mtu}</td>
                <td className="act">
                  {app.canEdit && (
                    <button
                      className="btn btn-sm"
                      onClick={() => setEdit({ index: null, iface: { name: l.name, role: "lan", zone: cfg.zones.find((z) => z.name === "lan")?.name ?? cfg.zones[0]?.name ?? "", enabled: true, addresses: [] } })}
                    >
                      {t("if.use")}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="section">
        <div className="section-head">
          <span className="section-title">{t("if.zones")}</span>
          <div className="actions">
            {app.canEdit && (
              <button className="btn btn-sm" onClick={() => setZoneEdit({ zone: { name: "", description: "" }, index: null })}>
                <I.IconPlus />
                {t("if.addzone")}
              </button>
            )}
          </div>
        </div>
        <div className="table-wrap">
          <table className="t">
            <thead>
              <tr>
                <th>{t("col.zone")}</th>
                <th>{t("col.description")}</th>
                <th className="num">{t("nav.interfaces")}</th>
                <th className="num">{t("if.zone.rules")}</th>
                <th className="act" />
              </tr>
            </thead>
            <tbody>
              {cfg.zones.map((z, i) => {
                const u = zoneUse(z.name);
                return (
                  <tr key={z.name} className="clickable" onClick={() => app.canEdit && setZoneEdit({ zone: { ...z }, index: i })}>
                    <td>
                      <ZoneChip name={z.name} />
                    </td>
                    <td className="muted">{z.description}</td>
                    <td className="num mono">{u.ifaces}</td>
                    <td className="num mono">{u.rules}</td>
                    <td className="act" onClick={(e) => e.stopPropagation()}>
                      {app.canEdit && (
                        <button className="btn btn-ghost btn-sm btn-icon" disabled={u.ifaces + u.rules > 0} title={u.ifaces + u.rules > 0 ? t("if.zone.inuse") : t("common.delete")} onClick={() => setDelZone(i)}>
                          <I.IconTrash />
                        </button>
                      )}
                    </td>
                  </tr>
                );
              })}
              <tr className="off">
                <td>
                  <ZoneChip name="self" />
                </td>
                <td className="faint">{t("if.zone.self")}</td>
                <td />
                <td className="num mono">{cfg.rules.filter((r) => r.to.includes("self")).length}</td>
                <td />
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      {edit && (
        <IfaceEditor
          initial={edit.iface}
          isNew={edit.index === null}
          links={links ?? []}
          errorsPrefix={edit.index !== null ? `interfaces[${edit.index}]` : ""}
          onClose={() => setEdit(null)}
          onDelete={
            edit.index !== null && edit.iface.role !== "mgmt"
              ? async () => {
                  const name = edit.iface.name;
                  if (await save((c) => void c.interfaces.splice(edit.index!, 1), t("common.deleted"))) setEdit(null);
                  void name;
                }
              : undefined
          }
          onSave={async (it) => {
            const ok = await save((c) => {
              if (edit.index === null) c.interfaces.push(it);
              else {
                const old = c.interfaces[edit.index].name;
                c.interfaces[edit.index] = it;
                if (old !== it.name) {
                  for (const r of c.routes) if (r.interface === old) r.interface = it.name;
                  for (const s of c.nat.source) if (s.interface === old) s.interface = it.name;
                  for (const d of c.nat.destination) if (d.interface === old) d.interface = it.name;
                }
              }
            });
            if (ok) setEdit(null);
          }}
        />
      )}
      {zoneEdit && (
        <ZoneEditor
          initial={zoneEdit.zone}
          onClose={() => setZoneEdit(null)}
          onSave={async (z) => {
            const ok = await save((c) => {
              if (zoneEdit.index === null) c.zones.push(z);
              else {
                const old = c.zones[zoneEdit.index].name;
                c.zones[zoneEdit.index] = z;
                if (old !== z.name) {
                  for (const i of c.interfaces) if (i.zone === old) i.zone = z.name;
                  for (const r of c.rules) {
                    r.from = r.from.map((x) => (x === old ? z.name : x));
                    r.to = r.to.map((x) => (x === old ? z.name : x));
                  }
                }
              }
            });
            if (ok) setZoneEdit(null);
          }}
        />
      )}
      {delZone !== null && (
        <Confirm
          title={t("if.zone.delete")}
          text={cfg.zones[delZone]?.name}
          action={t("common.delete")}
          danger
          onClose={() => setDelZone(null)}
          onConfirm={() => save((c) => void c.zones.splice(delZone, 1), t("common.deleted"))}
        />
      )}
    </>
  );
}

function IfaceEditor(p: {
  initial: Iface;
  isNew: boolean;
  links: Link[];
  errorsPrefix: string;
  onClose: () => void;
  onSave: (i: Iface) => void;
  onDelete?: () => void;
}) {
  const app = useApp();
  const cfg = app.candidate!.body;
  const [it, setIt] = useState<Iface>(p.initial);
  const set = <K extends keyof Iface>(k: K, v: Iface[K]) => setIt((x) => ({ ...x, [k]: v }));
  const mgmt = it.role === "mgmt";
  const err = (f: string) => (p.errorsPrefix ? fieldError(app.validation, `${p.errorsPrefix}.${f}`) : undefined);
  const parents = cfg.interfaces.filter((i) => !i.vlan).map((i) => i.name);

  const setVlan = (parent: string, id: number) => setIt((x) => ({ ...x, vlan: { parent, id }, name: x.name && !p.isNew ? x.name : `${parent}.${id}` }));

  return (
    <Drawer
      title={p.isNew ? (it.vlan ? t("if.newvlan") : t("if.new")) : it.name}
      onClose={p.onClose}
      footer={
        <>
          {p.onDelete && app.canEdit && (
            <button className="btn btn-danger" onClick={p.onDelete}>
              {t("common.delete")}
            </button>
          )}
          <div className="spacer" />
          <button className="btn" onClick={p.onClose}>
            {t("common.cancel")}
          </button>
          {app.canEdit && (
            <button className="btn btn-primary" disabled={!it.name || !it.zone} onClick={() => p.onSave(it)}>
              {t("common.tocandidate")}
            </button>
          )}
        </>
      }
    >
      {mgmt && <div className="note" style={{ marginBottom: 16 }}>{t("if.mgmt.note")}</div>}
      <div className="form-grid">
        {it.vlan ? (
          <>
            <Field label={t("if.vlan.parent")}>
              <select className="select" value={it.vlan.parent} onChange={(e) => setVlan(e.target.value, it.vlan!.id)} disabled={!p.isNew}>
                {parents.map((n) => (
                  <option key={n}>{n}</option>
                ))}
              </select>
            </Field>
            <Field label="VLAN ID" error={err("vlan.id")}>
              <input className="input mono" type="number" min={1} max={4094} value={it.vlan.id} disabled={!p.isNew} onChange={(e) => setVlan(it.vlan!.parent, Number(e.target.value))} />
            </Field>
          </>
        ) : null}
        <Field label={t("col.interface")} error={err("name")}>
          <input className="input mono" value={it.name} disabled={!p.isNew || !!it.vlan} onChange={(e) => set("name", e.target.value)} list="links" />
          <datalist id="links">
            {p.links.map((l) => (
              <option key={l.name} value={l.name} />
            ))}
          </datalist>
        </Field>
        <Field label={t("col.description")}>
          <input className="input" value={it.description ?? ""} onChange={(e) => set("description", e.target.value)} />
        </Field>
        <Field label={t("col.role")}>
          {mgmt ? (
            <input className="input" value="mgmt" disabled />
          ) : (
            <Seg value={it.role} onChange={(v) => set("role", v)} options={[{ value: "lan", label: "LAN" }, { value: "wan", label: "WAN" }]} />
          )}
        </Field>
        <Field label={t("col.zone")} error={err("zone")}>
          <select className="select" value={it.zone} onChange={(e) => set("zone", e.target.value)}>
            {cfg.zones.map((z) => (
              <option key={z.name}>{z.name}</option>
            ))}
          </select>
        </Field>
        {!mgmt && (
          <>
            <Field label={t("col.addressing")} className="span-2">
              <Seg
                value={it.dhcp ? "dhcp" : "static"}
                onChange={(v) => setIt((x) => ({ ...x, dhcp: v === "dhcp", addresses: v === "dhcp" ? [] : x.addresses }))}
                options={[
                  { value: "static", label: t("if.static") },
                  { value: "dhcp", label: "DHCP" },
                ]}
              />
            </Field>
            {!it.dhcp && (
              <Field label={t("col.addresses")} hint={t("if.addr.hint")} error={err("addresses")} className="span-2">
                <TagInput value={it.addresses ?? []} onChange={(v) => set("addresses", v)} placeholder="192.168.1.1/24" />
              </Field>
            )}
            <Field label="MTU" hint={t("if.mtu.hint")} error={err("mtu")}>
              <input className="input mono" type="number" value={it.mtu || ""} placeholder="1500" onChange={(e) => set("mtu", Number(e.target.value) || 0)} />
            </Field>
            <Field label={t("common.state")}>
              <div style={{ height: 30, display: "flex", alignItems: "center" }}>
                <Switch checked={it.enabled} onChange={(v) => set("enabled", v)} label={t("common.enabled")} />
              </div>
            </Field>
          </>
        )}
      </div>
    </Drawer>
  );
}

function ZoneEditor(p: { initial: Zone; onClose: () => void; onSave: (z: Zone) => void }) {
  const [z, setZ] = useState(p.initial);
  return (
    <Modal
      title={p.initial.name ? p.initial.name : t("if.addzone")}
      onClose={p.onClose}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={p.onClose}>
            {t("common.cancel")}
          </button>
          <button className="btn btn-primary" disabled={!/^[A-Za-z0-9][A-Za-z0-9_.-]*$/.test(z.name)} onClick={() => p.onSave(z)}>
            {t("common.tocandidate")}
          </button>
        </>
      }
    >
      <div className="stack">
        <Field label={t("col.name")} hint={t("if.zone.name.hint")}>
          <input className="input mono" autoFocus value={z.name} onChange={(e) => setZ({ ...z, name: e.target.value.trim() })} />
        </Field>
        <Field label={t("col.description")}>
          <input className="input" value={z.description ?? ""} onChange={(e) => setZ({ ...z, description: e.target.value })} />
        </Field>
      </div>
    </Modal>
  );
}
