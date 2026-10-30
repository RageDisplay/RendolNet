import { useState } from "react";
import { api } from "../lib/api";
import { ago, bytes } from "../lib/format";
import { usePoll } from "../lib/router";
import { useApp } from "../lib/state";
import type { Iface, WireGuardKeypair, WireGuardPeer, WireGuardStatus } from "../lib/types";
import { fieldError } from "../lib/suggest";
import { t } from "../i18n";
import { Confirm, Drawer, EmptyState, Field, Modal, Switch, useToast } from "../components/ui";
import { TagInput } from "../components/TagInput";
import * as I from "../components/icons";

export function WireGuard() {
  const app = useApp();
  const toast = useToast();
  const cfg = app.candidate!.body;
  const { data: devices } = usePoll(() => api.get<WireGuardStatus[]>("/v1/wireguard"), 5000);
  const [edit, setEdit] = useState<{ iface: Iface; index: number | null } | null>(null);
  const [del, setDel] = useState<number | null>(null);
  const [client, setClient] = useState<number | null>(null);
  const tunnels = cfg.interfaces.map((iface, index) => ({ iface, index })).filter((x) => x.iface.wireguard);
  const status = (name: string) => devices?.find((d) => d.name === name);

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

  const add = async () => {
    try {
      const keys = await api.post<WireGuardKeypair>("/v1/wireguard/keypair");
      let n = 0;
      while (cfg.interfaces.some((i) => i.name === `wg${n}`)) n++;
      setEdit({
        index: null,
        iface: {
          name: `wg${n}`, description: "", role: "lan", zone: cfg.zones[0]?.name ?? "lan", enabled: true,
          addresses: ["10.200.0.1/24"], mtu: 1420,
          wireguard: { listen_port: 51820 + n, private_key: keys.private_key, peers: [] },
        },
      });
    } catch (e: any) {
      toast(e.message, true);
    }
  };

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("wg.lead")}</div>
        <div className="actions">
          {app.canEdit && <button className="btn btn-primary" onClick={add}><I.IconPlus />{t("wg.add")}</button>}
        </div>
      </div>
      <div className="note" style={{ marginBottom: 12 }}>{t("wg.routes.hint")}</div>
      {tunnels.length === 0 ? <EmptyState title={t("wg.empty")}>{t("wg.empty.hint")}</EmptyState> : (
        <div className="table-wrap">
          <table className="t">
            <thead><tr>
              <th>{t("col.name")}</th><th>{t("col.zone")}</th><th>{t("col.addresses")}</th>
              <th>UDP</th><th>Peers</th><th>{t("wg.handshake")}</th><th>RX / TX</th><th className="act" />
            </tr></thead>
            <tbody>{tunnels.map(({ iface, index }) => {
              const wg = iface.wireguard!;
              const st = status(iface.name);
              const handshakes = (st?.peers ?? []).map((p) => p.last_handshake).filter((s) => new Date(s).getTime() > 0).sort();
              const rx = (st?.peers ?? []).reduce((n, p) => n + p.rx_bytes, 0);
              const tx = (st?.peers ?? []).reduce((n, p) => n + p.tx_bytes, 0);
              return <tr key={iface.name} className="clickable" onClick={() => setEdit({ iface: structuredClone(iface), index })}>
                <td><span className="mono">{iface.name}</span> {!iface.enabled && <span className="badge neutral">{t("common.off")}</span>}</td>
                <td>{iface.zone}</td><td className="mono">{(iface.addresses ?? []).join(", ")}</td>
                <td className="mono">{st?.listen_port ?? wg.listen_port}</td><td className="num mono">{wg.peers.length}</td>
                <td className="muted nowrap">{handshakes.length ? ago(handshakes[handshakes.length - 1]) : "—"}</td>
                <td className="mono faint nowrap">{st ? `${bytes(rx)} / ${bytes(tx)}` : "—"}</td>
                <td className="act nowrap" onClick={(e) => e.stopPropagation()}>
                  {app.canEdit && <>
                    <button className="btn btn-ghost btn-sm btn-icon" title={t("wg.client")} disabled={!st} onClick={() => setClient(index)}><I.IconDownload /></button>
                    <button className="btn btn-ghost btn-sm btn-icon" title={t("common.delete")} onClick={() => setDel(index)}><I.IconTrash /></button>
                  </>}
                </td>
              </tr>;
            })}</tbody>
          </table>
        </div>
      )}
      {edit && <TunnelEditor initial={edit.iface} index={edit.index} onClose={() => setEdit(null)} onSave={async (iface) => {
        const ok = await save((c) => edit.index === null ? c.interfaces.push(iface) : c.interfaces[edit.index] = iface);
        if (ok) setEdit(null);
      }} />}
      {del !== null && <Confirm title={t("wg.delete")} text={<span className="mono">{cfg.interfaces[del]?.name}</span>} action={t("common.delete")} danger onClose={() => setDel(null)} onConfirm={() => save((c) => {
        const name = c.interfaces[del]?.name;
        c.interfaces.splice(del, 1);
        c.routes = c.routes.filter((r) => r.interface !== name);
      }, t("common.deleted"))} />}
      {client !== null && <ClientConfig index={client} status={status(cfg.interfaces[client]?.name)} onClose={() => setClient(null)} />}
    </>
  );
}

function TunnelEditor({ initial, index, onClose, onSave }: { initial: Iface; index: number | null; onClose: () => void; onSave: (iface: Iface) => void }) {
  const app = useApp();
  const toast = useToast();
  const cfg = app.candidate!.body;
  const [iface, setIface] = useState(initial);
  const [peer, setPeer] = useState<{ value: WireGuardPeer; index: number | null } | null>(null);
  const wg = iface.wireguard!;
  const prefix = index === null ? "" : `interfaces[${index}].wireguard`;
  const err = (field: string) => prefix ? fieldError(app.validation, `${prefix}.${field}`) : undefined;
  const setWG = (value: Partial<typeof wg>) => setIface({ ...iface, wireguard: { ...wg, ...value } });
  const generate = async () => {
    try {
      const keys = await api.post<WireGuardKeypair>("/v1/wireguard/keypair");
      setWG({ private_key: keys.private_key });
    } catch (e: any) { toast(e.message, true); }
  };
  return <Drawer title={index === null ? t("wg.new") : iface.name} onClose={onClose} footer={<>
    <div className="spacer" /><button className="btn" onClick={onClose}>{t("common.cancel")}</button>
    {app.canEdit && <button className="btn btn-primary" disabled={!iface.name || !wg.private_key || !(iface.addresses ?? []).length} onClick={() => onSave(iface)}>{t("common.tocandidate")}</button>}
  </>}>
    <div className="form-grid">
      <Field label={t("col.name")}><input className="input mono" value={iface.name} disabled={index !== null} onChange={(e) => setIface({ ...iface, name: e.target.value.trim() })} /></Field>
      <Field label={t("col.description")}><input className="input" value={iface.description ?? ""} onChange={(e) => setIface({ ...iface, description: e.target.value })} /></Field>
      <Field label={t("col.role")}><select className="select" value={iface.role} onChange={(e) => setIface({ ...iface, role: e.target.value as "wan" | "lan" })}><option value="lan">LAN</option><option value="wan">WAN</option></select></Field>
      <Field label={t("col.zone")}><select className="select" value={iface.zone} onChange={(e) => setIface({ ...iface, zone: e.target.value })}>{cfg.zones.map((z) => <option key={z.name}>{z.name}</option>)}</select></Field>
      <Field label={t("col.addresses")} className="span-2"><TagInput value={iface.addresses ?? []} onChange={(addresses) => setIface({ ...iface, addresses })} placeholder="10.200.0.1/24" mono /></Field>
      <Field label="MTU"><input className="input mono" type="number" min={576} max={9216} value={iface.mtu ?? 1420} onChange={(e) => setIface({ ...iface, mtu: Number(e.target.value) })} /></Field>
      <Field label={t("wg.port")} error={err("listen_port")}><input className="input mono" type="number" min={1} max={65535} value={wg.listen_port} onChange={(e) => setWG({ listen_port: Number(e.target.value) })} /></Field>
      <Field label={t("wg.private")} hint={t("wg.private.hint")} error={err("private_key")} className="span-2"><div style={{ display: "flex", gap: 8 }}><input className="input mono" type="password" value={wg.private_key} onChange={(e) => setWG({ private_key: e.target.value.trim() })} /><button className="btn" type="button" onClick={generate}>{t("wg.generate")}</button></div></Field>
    </div>
    <div style={{ margin: "18px 0" }}><Switch checked={iface.enabled} onChange={(enabled) => setIface({ ...iface, enabled })} label={t("common.enabled")} disabled={!app.canEdit} /></div>
    <div className="section-head"><span className="section-title">Peers</span><div className="spacer" />{app.canEdit && <button className="btn btn-sm" onClick={() => setPeer({ index: null, value: { name: "", public_key: "", allowed_ips: [], persistent_keepalive: 25 } })}><I.IconPlus />{t("wg.peer.add")}</button>}</div>
    {wg.peers.length === 0 ? <div className="note">{t("wg.peer.empty")}</div> : <table className="t"><tbody>{wg.peers.map((p, i) => <tr key={i} className="clickable" onClick={() => setPeer({ value: structuredClone(p), index: i })}>
      <td>{p.name}<div className="faint mono" style={{ maxWidth: 220, overflow: "hidden", textOverflow: "ellipsis" }}>{p.public_key || "—"}</div></td>
      <td className="mono">{p.allowed_ips.join(", ")}</td>
      <td className="act" onClick={(e) => e.stopPropagation()}>{app.canEdit && <button className="btn btn-ghost btn-sm btn-icon" onClick={() => setWG({ peers: wg.peers.filter((_, j) => j !== i) })}><I.IconTrash /></button>}</td>
    </tr>)}</tbody></table>}
    {peer && <PeerEditor initial={peer.value} index={peer.index} prefix={prefix} onClose={() => setPeer(null)} onSave={(value) => {
      const peers = [...wg.peers];
      if (peer.index === null) peers.push(value); else peers[peer.index] = value;
      setWG({ peers }); setPeer(null);
    }} />}
  </Drawer>;
}

function PeerEditor({ initial, index, prefix, onClose, onSave }: { initial: WireGuardPeer; index: number | null; prefix: string; onClose: () => void; onSave: (peer: WireGuardPeer) => void }) {
  const app = useApp();
  const [peer, setPeer] = useState(initial);
  const base = prefix && index !== null ? `${prefix}.peers[${index}]` : "";
  const err = (field: string) => base ? fieldError(app.validation, `${base}.${field}`) : undefined;
  return <Modal title={index === null ? t("wg.peer.new") : peer.name} onClose={onClose} wide footer={<><div className="spacer" /><button className="btn" onClick={onClose}>{t("common.cancel")}</button><button className="btn btn-primary" disabled={!peer.name || !peer.public_key || !peer.allowed_ips.length} onClick={() => onSave(peer)}>{t("common.save")}</button></>}>
    <div className="form-grid">
      <Field label={t("col.name")}><input className="input" autoFocus value={peer.name} onChange={(e) => setPeer({ ...peer, name: e.target.value.trim() })} /></Field>
      <Field label="Endpoint" hint="host:51820" error={err("endpoint")}><input className="input mono" value={peer.endpoint ?? ""} onChange={(e) => setPeer({ ...peer, endpoint: e.target.value.trim() || undefined })} /></Field>
      <Field label={t("wg.public")} error={err("public_key")} className="span-2"><input className="input mono" value={peer.public_key} onChange={(e) => setPeer({ ...peer, public_key: e.target.value.trim() })} /></Field>
      <Field label="Preshared key" hint={t("wg.psk.hint")} error={err("preshared_key")} className="span-2"><input className="input mono" type="password" value={peer.preshared_key ?? ""} onChange={(e) => setPeer({ ...peer, preshared_key: e.target.value.trim() || undefined })} /></Field>
      <Field label="AllowedIPs" hint={t("wg.allowed.hint")} className="span-2"><TagInput value={peer.allowed_ips} onChange={(allowed_ips) => setPeer({ ...peer, allowed_ips })} placeholder="10.200.0.2/32" mono /></Field>
      <Field label={t("wg.keepalive")} hint={t("wg.keepalive.hint")}><input className="input mono" type="number" min={0} max={65535} value={peer.persistent_keepalive ?? 0} onChange={(e) => setPeer({ ...peer, persistent_keepalive: Number(e.target.value) || undefined })} /></Field>
    </div>
  </Modal>;
}

function ClientConfig({ index, status, onClose }: { index: number; status?: WireGuardStatus; onClose: () => void }) {
  const app = useApp();
  const toast = useToast();
  const iface = app.candidate!.body.interfaces[index];
  const wg = iface.wireguard!;
  const [peerIndex, setPeerIndex] = useState(wg.peers.length ? 0 : -1);
  const [clientName, setClientName] = useState("client");
  const [clientAddress, setClientAddress] = useState((iface.addresses?.[0] ?? "").replace(/^(\d+\.\d+\.\d+)\.\d+\/\d+$/, "$1.2/32"));
  const [endpoint, setEndpoint] = useState("");
  const [allowed, setAllowed] = useState("0.0.0.0/0, ::/0");
  const [busy, setBusy] = useState(false);
  const download = async () => {
    setBusy(true);
    try {
      const keys = await api.post<WireGuardKeypair>("/v1/wireguard/keypair");
      const peer: WireGuardPeer = peerIndex < 0
        ? { name: clientName, public_key: keys.public_key, allowed_ips: [clientAddress], persistent_keepalive: 25 }
        : { ...wg.peers[peerIndex], public_key: keys.public_key };
      await app.edit((c) => {
        const peers = c.interfaces[index].wireguard!.peers;
        if (peerIndex < 0) peers.push(peer); else peers[peerIndex] = peer;
      });
      const lines = ["[Interface]", `PrivateKey = ${keys.private_key}`, `Address = ${peer.allowed_ips.join(", ")}`, "", "[Peer]", `PublicKey = ${status!.public_key}`];
      if (peer.preshared_key) lines.push(`PresharedKey = ${peer.preshared_key}`);
      lines.push(`Endpoint = ${endpoint}`, `AllowedIPs = ${allowed}`, `PersistentKeepalive = ${peer.persistent_keepalive || 25}`);
      const url = URL.createObjectURL(new Blob([lines.join("\n") + "\n"], { type: "text/plain" }));
      const a = document.createElement("a");
      a.href = url;
      a.download = `${iface.name}-${peer.name}.conf`.replace(/[^A-Za-z0-9_.-]/g, "_");
      a.click();
      URL.revokeObjectURL(url);
      toast(t("wg.client.done")); onClose();
    } catch (e: any) { toast(e.message, true); } finally { setBusy(false); }
  };
  const invalidNew = peerIndex < 0 && (!clientName.trim() || !clientAddress.trim());
  return <Modal title={t("wg.client")} onClose={onClose} footer={<><div className="spacer" /><button className="btn" onClick={onClose}>{t("common.cancel")}</button><button className="btn btn-primary" disabled={busy || app.dirty || !status || invalidNew || !endpoint.includes(":") || !allowed.trim()} onClick={download}><I.IconDownload />{busy ? t("wg.client.busy") : t("wg.client.download")}</button></>}>
    <div className="stack">
      <div className="note warn">{t(peerIndex < 0 ? "wg.client.new.warn" : "wg.client.warn")}</div>
      {app.dirty && <div className="note bad">{t("wg.client.commit")}</div>}
      <Field label="Peer"><select className="select" value={peerIndex} onChange={(e) => setPeerIndex(Number(e.target.value))}><option value={-1}>{t("wg.client.new")}</option>{wg.peers.map((p, i) => <option key={i} value={i}>{p.name}</option>)}</select></Field>
      {peerIndex < 0 && <div className="form-grid">
        <Field label={t("col.name")}><input className="input" value={clientName} onChange={(e) => setClientName(e.target.value)} /></Field>
        <Field label={t("wg.client.address")}><input className="input mono" value={clientAddress} placeholder="10.200.0.2/32" onChange={(e) => setClientAddress(e.target.value.trim())} /></Field>
      </div>}
      <Field label={t("wg.server.endpoint")} hint={t("wg.server.endpoint.hint")}><input className="input mono" value={endpoint} placeholder={`vpn.example.com:${wg.listen_port}`} onChange={(e) => setEndpoint(e.target.value.trim())} /></Field>
      <Field label="AllowedIPs" hint={t("wg.client.allowed.hint")}><input className="input mono" value={allowed} onChange={(e) => setAllowed(e.target.value)} /></Field>
    </div>
  </Modal>;
}
