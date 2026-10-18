import { useState } from "react";
import { useApp } from "../lib/state";
import { api } from "../lib/api";
import { usePoll } from "../lib/router";
import type { AddressGroup, AddressObject, Config, FQDNInfo, Proto, Service } from "../lib/types";
import { addressSuggestions } from "../lib/suggest";
import { t } from "../i18n";
import { Chips, Confirm, Field, Modal, cx, useToast } from "../components/ui";
import { TagInput } from "../components/TagInput";
import * as I from "../components/icons";

type Tab = "addresses" | "groups" | "services";

function usage(c: Config, name: string): number {
  let n = 0;
  const has = (l?: string[]) => !!l && l.includes(name);
  for (const r of c.rules) if (has(r.source) || has(r.destination) || has(r.services)) n++;
  for (const g of c.objects.groups) if (has(g.members)) n++;
  for (const s of c.nat.source) if (has(s.source)) n++;
  if (has(c.management.allow_from)) n++;
  return n;
}

export function Objects() {
  const app = useApp();
  const toast = useToast();
  const cfg = app.candidate!.body;
  const [tab, setTab] = useState<Tab>("addresses");
  const [addr, setAddr] = useState<{ o: AddressObject; index: number | null } | null>(null);
  const [grp, setGrp] = useState<{ o: AddressGroup; index: number | null } | null>(null);
  const [svc, setSvc] = useState<{ o: Service; index: number | null } | null>(null);
  const [del, setDel] = useState<{ tab: Tab; index: number; name: string } | null>(null);
  const hasDomains = cfg.objects.addresses.some((o) => (o.domains ?? []).length > 0);
  const fqdn = usePoll(() => (hasDomains ? api.get<FQDNInfo[]>("/v1/diag/fqdn") : Promise.resolve([] as FQDNInfo[])), 15000, [hasDomains]);
  const learned = (domains?: string[]) => {
    const all = new Set<string>();
    for (const f of fqdn.data ?? []) if (domains?.includes(f.domain)) f.addresses.forEach((a) => all.add(a.address));
    return [...all].sort();
  };

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

  // Переименование объекта меняет ссылки на него во всём конфиге.
  const rename = (c: Config, from: string, to: string) => {
    if (from === to || !from) return;
    const fix = (l?: string[]) => l?.map((x) => (x === from ? to : x));
    for (const r of c.rules) {
      r.source = fix(r.source);
      r.destination = fix(r.destination);
      r.services = fix(r.services);
    }
    for (const g of c.objects.groups) g.members = fix(g.members)!;
    for (const s of c.nat.source) s.source = fix(s.source);
    c.management.allow_from = fix(c.management.allow_from);
  };

  const DelBtn = ({ t: tb, i, name }: { t: Tab; i: number; name: string }) => {
    const u = usage(cfg, name);
    return app.canEdit ? (
      <button className="btn btn-ghost btn-sm btn-icon" disabled={u > 0} title={u > 0 ? t("obj.inuse", { n: u }) : t("common.delete")} onClick={(e) => {
        e.stopPropagation();
        setDel({ tab: tb, index: i, name });
      }}>
        <I.IconTrash />
      </button>
    ) : null;
  };

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("obj.lead")}</div>
        <div className="actions">
          {app.canEdit && (
            <button
              className="btn btn-primary"
              onClick={() => {
                if (tab === "addresses") setAddr({ index: null, o: { name: "", prefixes: [] } });
                if (tab === "groups") setGrp({ index: null, o: { name: "", members: [] } });
                if (tab === "services") setSvc({ index: null, o: { name: "", proto: "tcp", ports: "" } });
              }}
            >
              <I.IconPlus />
              {t("obj.add")}
            </button>
          )}
        </div>
      </div>
      <div className="tabs">
        {(["addresses", "groups", "services"] as Tab[]).map((x) => (
          <button key={x} className={cx(tab === x && "on")} onClick={() => setTab(x)}>
            {t(`obj.${x}` as any)}
            <span className="count">{cfg.objects[x].length}</span>
          </button>
        ))}
      </div>

      <div className="table-wrap">
        <table className="t">
          <thead>
            <tr>
              <th>{t("col.name")}</th>
              <th>{tab === "addresses" ? t("obj.prefixes") : tab === "groups" ? t("obj.members") : t("obj.def")}</th>
              <th>{t("col.comment")}</th>
              <th className="num">{t("obj.refs")}</th>
              <th className="act" />
            </tr>
          </thead>
          <tbody>
            {tab === "addresses" &&
              cfg.objects.addresses.map((o, i) => (
                <tr key={o.name} className="clickable" onClick={() => setAddr({ o: JSON.parse(JSON.stringify(o)), index: i })}>
                  <td className="mono" style={{ fontWeight: 500 }}>{o.name}</td>
                  <td>
                    {o.prefixes.length > 0 && <Chips items={o.prefixes} mono />}
                    {(o.domains ?? []).length > 0 && (
                      <div className="row" style={{ gap: 6, flexWrap: "wrap" }}>
                        <Chips items={o.domains} mono />
                        <span className="faint" style={{ fontSize: 12 }}>{t("obj.learned", { n: learned(o.domains).length })}</span>
                      </div>
                    )}
                  </td>
                  <td className="muted">{o.comment}</td>
                  <td className="num mono">{usage(cfg, o.name)}</td>
                  <td className="act"><DelBtn t="addresses" i={i} name={o.name} /></td>
                </tr>
              ))}
            {tab === "groups" &&
              cfg.objects.groups.map((o, i) => (
                <tr key={o.name} className="clickable" onClick={() => setGrp({ o: JSON.parse(JSON.stringify(o)), index: i })}>
                  <td className="mono" style={{ fontWeight: 500 }}>{o.name}</td>
                  <td><Chips items={o.members} mono /></td>
                  <td className="muted">{o.comment}</td>
                  <td className="num mono">{usage(cfg, o.name)}</td>
                  <td className="act"><DelBtn t="groups" i={i} name={o.name} /></td>
                </tr>
              ))}
            {tab === "services" &&
              cfg.objects.services.map((o, i) => (
                <tr key={o.name} className="clickable" onClick={() => setSvc({ o: { ...o }, index: i })}>
                  <td className="mono" style={{ fontWeight: 500 }}>{o.name}</td>
                  <td className="mono">{o.proto}{o.ports ? `/${o.ports}` : ""}</td>
                  <td className="muted">{o.comment}</td>
                  <td className="num mono">{usage(cfg, o.name)}</td>
                  <td className="act"><DelBtn t="services" i={i} name={o.name} /></td>
                </tr>
              ))}
            {cfg.objects[tab].length === 0 && (
              <tr>
                <td colSpan={5} className="empty">{t("obj.empty")}</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {addr && (
        <ObjModal
          title={addr.o.name || t("obj.newaddr")}
          name={addr.o.name}
          comment={addr.o.comment}
          onClose={() => setAddr(null)}
          valid={addr.o.prefixes.length > 0 || (addr.o.domains ?? []).length > 0}
          onSave={async (name, comment) => {
            const o = { ...addr.o, name, comment };
            const old = addr.index !== null ? cfg.objects.addresses[addr.index].name : "";
            if (await save((c) => {
              if (addr.index === null) c.objects.addresses.push(o);
              else c.objects.addresses[addr.index] = o;
              rename(c, old, name);
            })) setAddr(null);
          }}
        >
          <Field label={t("obj.prefixes")} hint={t("obj.prefixes.hint")}>
            <TagInput value={addr.o.prefixes} onChange={(v) => setAddr({ ...addr, o: { ...addr.o, prefixes: v } })} placeholder="10.0.0.0/8" />
          </Field>
          <Field label={t("obj.domains")} hint={t("obj.domains.hint")}>
            <TagInput
              value={addr.o.domains ?? []}
              onChange={(v) => setAddr({ ...addr, o: { ...addr.o, domains: v.length ? v.map((d) => d.trim().toLowerCase()) : undefined } })}
              placeholder="tiktok.com"
              mono
            />
          </Field>
          {(addr.o.domains ?? []).length > 0 && !cfg.dns_proxy?.enabled && <div className="note warn">{t("obj.domains.noproxy")}</div>}
          {addr.index !== null && learned(cfg.objects.addresses[addr.index].domains).length > 0 && (
            <details>
              <summary className="muted">{t("obj.learned", { n: learned(cfg.objects.addresses[addr.index].domains).length })}</summary>
              <div className="log" style={{ maxHeight: 200, marginTop: 6 }}>{learned(cfg.objects.addresses[addr.index].domains).join("\n")}</div>
            </details>
          )}
        </ObjModal>
      )}
      {grp && (
        <ObjModal
          title={grp.o.name || t("obj.newgroup")}
          name={grp.o.name}
          comment={grp.o.comment}
          onClose={() => setGrp(null)}
          valid={grp.o.members.length > 0}
          onSave={async (name, comment) => {
            const o = { ...grp.o, name, comment };
            const old = grp.index !== null ? cfg.objects.groups[grp.index].name : "";
            if (await save((c) => {
              if (grp.index === null) c.objects.groups.push(o);
              else c.objects.groups[grp.index] = o;
              rename(c, old, name);
            })) setGrp(null);
          }}
        >
          <Field label={t("obj.members")} hint={t("obj.members.hint")}>
            <TagInput value={grp.o.members} onChange={(v) => setGrp({ ...grp, o: { ...grp.o, members: v } })} suggestions={addressSuggestions(cfg).filter((s) => s.value !== grp.o.name)} />
          </Field>
        </ObjModal>
      )}
      {svc && (
        <ObjModal
          title={svc.o.name || t("obj.newsvc")}
          name={svc.o.name}
          comment={svc.o.comment}
          onClose={() => setSvc(null)}
          valid
          onSave={async (name, comment) => {
            const o = { ...svc.o, name, comment };
            const old = svc.index !== null ? cfg.objects.services[svc.index].name : "";
            if (await save((c) => {
              if (svc.index === null) c.objects.services.push(o);
              else c.objects.services[svc.index] = o;
              rename(c, old, name);
            })) setSvc(null);
          }}
        >
          <div className="form-grid">
            <Field label={t("col.proto")}>
              <select className="select" value={svc.o.proto} onChange={(e) => setSvc({ ...svc, o: { ...svc.o, proto: e.target.value as Proto, ports: ["tcp", "udp"].includes(e.target.value) ? svc.o.ports : "" } })}>
                {["tcp", "udp", "icmp", "icmpv6", "any"].map((p) => (
                  <option key={p}>{p}</option>
                ))}
              </select>
            </Field>
            <Field label={t("obj.ports")} hint={t("obj.ports.hint")}>
              <input className="input mono" disabled={!["tcp", "udp"].includes(svc.o.proto)} value={svc.o.ports ?? ""} placeholder="80,443,8000-8080" onChange={(e) => setSvc({ ...svc, o: { ...svc.o, ports: e.target.value } })} />
            </Field>
          </div>
        </ObjModal>
      )}
      {del && (
        <Confirm
          title={t("obj.delete")}
          text={<span className="mono">{del.name}</span>}
          action={t("common.delete")}
          danger
          onClose={() => setDel(null)}
          onConfirm={() => save((c) => void c.objects[del.tab].splice(del.index, 1), t("common.deleted"))}
        />
      )}
    </>
  );
}

function ObjModal(p: { title: string; name: string; comment?: string; valid: boolean; onClose: () => void; onSave: (name: string, comment: string) => void; children: React.ReactNode }) {
  const app = useApp();
  const [name, setName] = useState(p.name);
  const [comment, setComment] = useState(p.comment ?? "");
  return (
    <Modal
      title={p.title}
      onClose={p.onClose}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={p.onClose}>{t("common.cancel")}</button>
          {app.canEdit && (
            <button className="btn btn-primary" disabled={!p.valid || !/^[A-Za-z0-9][A-Za-z0-9_.-]*$/.test(name)} onClick={() => p.onSave(name, comment)}>
              {t("common.tocandidate")}
            </button>
          )}
        </>
      }
    >
      <div className="stack">
        <Field label={t("col.name")} hint={t("obj.name.hint")}>
          <input className="input mono" autoFocus value={name} onChange={(e) => setName(e.target.value.trim())} />
        </Field>
        {p.children}
        <Field label={t("col.comment")}>
          <input className="input" value={comment} onChange={(e) => setComment(e.target.value)} />
        </Field>
      </div>
    </Modal>
  );
}
