import { useEffect, useState } from "react";
import { api } from "../lib/api";
import { usePoll } from "../lib/router";
import { useApp } from "../lib/state";
import type { DHCPServer, Iface, Lease } from "../lib/types";
import { datetime } from "../lib/format";
import { fieldError } from "../lib/suggest";
import { t } from "../i18n";
import { Field, Switch, useToast } from "../components/ui";
import { TagInput } from "../components/TagInput";
import { IconPlus, IconTrash } from "../components/icons";

type Servers = Record<string, DHCPServer>;

const v4 = (it: Iface) => (it.addresses ?? []).find((a) => /^\d+\.\d+\.\d+\.\d+\//.test(a));

// .100–.199 в /24 узла, как DefaultDHCPRange на сервере
function suggestRange(cidr: string): [string, string] {
  const [ip] = cidr.split("/");
  const o = ip.split(".");
  const self = Number(o[3]);
  const [lo, hi] = self >= 100 && self <= 199 ? [10, 99] : [100, 199];
  const p = o.slice(0, 3).join(".");
  return [`${p}.${lo}`, `${p}.${hi}`];
}

function serversOf(ifaces: Iface[]): Servers {
  const out: Servers = {};
  for (const it of ifaces) if (it.dhcp_server) out[it.name] = it.dhcp_server;
  return out;
}

export function DhcpDns() {
  const app = useApp();
  const toast = useToast();
  const cfg = app.candidate!.body;
  const { data: leases } = usePoll(() => api.get<Lease[]>("/v1/dhcp/leases"), 10000);

  const [proxy, setProxy] = useState(cfg.dns_proxy?.enabled ?? false);
  const [upstreams, setUpstreams] = useState(cfg.system.dns ?? []);
  const [srv, setSrv] = useState<Servers>(serversOf(cfg.interfaces));

  useEffect(() => {
    setProxy(cfg.dns_proxy?.enabled ?? false);
    setUpstreams(cfg.system.dns ?? []);
    setSrv(serversOf(cfg.interfaces));
  }, [cfg]);

  const lans = cfg.interfaces.filter((i) => i.role === "lan");
  const changed =
    proxy !== (cfg.dns_proxy?.enabled ?? false) ||
    JSON.stringify(upstreams) !== JSON.stringify(cfg.system.dns ?? []) ||
    JSON.stringify(srv) !== JSON.stringify(serversOf(cfg.interfaces));

  const put = (name: string, d: DHCPServer) => setSrv({ ...srv, [name]: d });

  const save = async () => {
    try {
      await app.edit((c) => {
        c.dns_proxy = { enabled: proxy };
        c.system.dns = upstreams;
        for (const it of c.interfaces) {
          if (srv[it.name]) it.dhcp_server = srv[it.name];
        }
      });
      toast(t("common.saved"));
    } catch (e: any) {
      toast(e.message, true);
    }
  };

  const pin = (l: Lease) => {
    const d = srv[l.interface];
    if (!d) return;
    put(l.interface, { ...d, static: [...(d.static ?? []), { mac: l.mac, address: l.ip, name: l.hostname }] });
  };

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("dhcp.lead")}</div>
        <div className="actions">
          {app.canEdit && (
            <button className="btn btn-primary" disabled={!changed} onClick={save}>
              {t("common.tocandidate")}
            </button>
          )}
        </div>
      </div>

      <div className="panel">
        <div className="panel-head">
          <span className="panel-title">{t("dhcp.dns")}</span>
        </div>
        <div className="panel-body form-grid" style={{ gridTemplateColumns: "minmax(0, 1fr) minmax(0, 2fr)" }}>
          <Switch checked={proxy} onChange={setProxy} label={t("dhcp.dns.enable")} disabled={!app.canEdit} />
          <Field label={t("dhcp.dns.upstreams")} hint={t("dhcp.dns.upstreams.hint")} error={fieldError(app.validation, "system.dns")}>
            <TagInput value={upstreams} onChange={setUpstreams} placeholder="1.1.1.1" mono />
          </Field>
        </div>
      </div>

      {lans.length === 0 && <div className="note">{t("dhcp.nolan")}</div>}

      {lans.map((it) => (
        <ServerPanel
          key={it.name}
          it={it}
          idx={cfg.interfaces.indexOf(it)}
          d={srv[it.name]}
          proxy={proxy}
          onChange={(d) => put(it.name, d)}
        />
      ))}

      <div className="section">
        <div className="section-head">
          <span className="section-title">{t("dhcp.leases")}</span>
          <span className="faint">{leases?.length ?? 0}</span>
        </div>
        <div className="table-wrap">
          <table className="t">
            <thead>
              <tr>
                <th>{t("dhcp.iface")}</th>
                <th>IP</th>
                <th>MAC</th>
                <th>{t("dhcp.host")}</th>
                <th>{t("dhcp.expires")}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {(leases ?? []).map((l) => (
                <tr key={l.interface + l.mac}>
                  <td className="mono">{l.interface}</td>
                  <td className="mono">{l.ip}</td>
                  <td className="mono faint">{l.mac}</td>
                  <td>{l.hostname || <span className="faint">—</span>}</td>
                  <td className="mono faint nowrap">{l.static ? <span className="badge neutral">{t("dhcp.static")}</span> : datetime(l.expires)}</td>
                  <td className="nowrap" style={{ textAlign: "right" }}>
                    {!l.static && app.canEdit && srv[l.interface] && !(srv[l.interface].static ?? []).some((s) => s.mac === l.mac) && (
                      <button className="btn btn-sm btn-ghost" onClick={() => pin(l)}>
                        {t("dhcp.pin")}
                      </button>
                    )}
                  </td>
                </tr>
              ))}
              {leases && leases.length === 0 && (
                <tr>
                  <td colSpan={6} className="faint">
                    {t("dhcp.noleases")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </>
  );
}

function ServerPanel(p: { it: Iface; idx: number; d?: DHCPServer; proxy: boolean; onChange: (d: DHCPServer) => void }) {
  const app = useApp();
  const { it, d } = p;
  const addr = v4(it);
  const base = `interfaces[${p.idx}].dhcp_server`;
  const err = (f: string) => fieldError(app.validation, `${base}.${f}`);
  const [mac, setMac] = useState("");
  const [ip, setIp] = useState("");
  const [name, setName] = useState("");

  const enable = (on: boolean) => {
    if (d) return p.onChange({ ...d, enabled: on });
    const [s, e] = addr ? suggestRange(addr) : ["", ""];
    p.onChange({ enabled: on, range_start: s, range_end: e });
  };

  return (
    <div className="section">
      <div className="section-head">
        <span className="section-title mono">{it.name}</span>
        <span className="faint">
          {t("dhcp.zone", { z: it.zone })} · <span className="mono">{addr ?? (it.dhcp ? "DHCP" : "—")}</span>
        </span>
        <div className="spacer" />
        {d?.enabled ? <span className="badge ok">{t("dhcp.on")}</span> : <span className="badge neutral">{t("common.off")}</span>}
      </div>
      <div className="panel panel-body stack">
        {!addr || it.dhcp ? (
          <div className="note warn">{t("dhcp.needaddr")}</div>
        ) : (
          <>
            {err("") && <div className="note bad">{err("")}</div>}
            <Switch checked={!!d?.enabled} onChange={enable} label={t("dhcp.enable")} disabled={!app.canEdit} />
            {d?.enabled && (
              <>
                <div className="form-grid" style={{ gridTemplateColumns: "repeat(3, minmax(0, 1fr))" }}>
                  <Field label={t("dhcp.start")} error={err("range_start")}>
                    <input className="input mono" value={d.range_start} onChange={(e) => p.onChange({ ...d, range_start: e.target.value.trim() })} />
                  </Field>
                  <Field label={t("dhcp.end")} error={err("range_end")}>
                    <input className="input mono" value={d.range_end} onChange={(e) => p.onChange({ ...d, range_end: e.target.value.trim() })} />
                  </Field>
                  <Field label={t("dhcp.lease")} hint={t("dhcp.lease.hint")} error={err("lease_minutes")}>
                    <input
                      className="input mono"
                      type="number"
                      value={d.lease_minutes ?? ""}
                      placeholder="720"
                      onChange={(e) => p.onChange({ ...d, lease_minutes: e.target.value ? Number(e.target.value) : undefined })}
                    />
                  </Field>
                  <Field label={t("dhcp.clientdns")} hint={p.proxy ? t("dhcp.clientdns.proxy", { a: addr.split("/")[0] }) : t("dhcp.clientdns.system")} error={err("dns")} className="span-2">
                    <TagInput value={d.dns ?? []} onChange={(v) => p.onChange({ ...d, dns: v.length ? v : undefined })} placeholder={p.proxy ? addr.split("/")[0] : ""} mono />
                  </Field>
                  <Field label={t("dhcp.domain")} error={err("domain")}>
                    <input className="input mono" value={d.domain ?? ""} placeholder="lan" onChange={(e) => p.onChange({ ...d, domain: e.target.value.trim() || undefined })} />
                  </Field>
                </div>

                <div className="muted" style={{ marginTop: 4 }}>
                  {t("dhcp.statics")}
                </div>
                {err("static") && <div className="note bad">{err("static")}</div>}
                <table className="t">
                  <thead>
                    <tr>
                      <th>MAC</th>
                      <th>IP</th>
                      <th>{t("dhcp.host")}</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {(d.static ?? []).map((s, i) => (
                      <tr key={s.mac + i}>
                        <td className="mono">{s.mac}</td>
                        <td className="mono">{s.address}</td>
                        <td>{s.name}</td>
                        <td style={{ textAlign: "right" }}>
                          <button className="btn btn-sm btn-ghost" title={t("common.delete")} onClick={() => p.onChange({ ...d, static: (d.static ?? []).filter((_, j) => j !== i) })}>
                            <IconTrash width={14} height={14} />
                          </button>
                        </td>
                      </tr>
                    ))}
                    <tr>
                      <td>
                        <input className="input input-sm mono" value={mac} placeholder="aa:bb:cc:dd:ee:ff" onChange={(e) => setMac(e.target.value.trim())} />
                      </td>
                      <td>
                        <input className="input input-sm mono" value={ip} placeholder={suggestRange(addr)[0].replace(/\.\d+$/, ".50")} onChange={(e) => setIp(e.target.value.trim())} />
                      </td>
                      <td>
                        <input className="input input-sm" value={name} onChange={(e) => setName(e.target.value)} />
                      </td>
                      <td style={{ textAlign: "right" }}>
                        <button
                          className="btn btn-sm"
                          disabled={!mac || !ip}
                          onClick={() => {
                            p.onChange({ ...d, static: [...(d.static ?? []), { mac: mac.toLowerCase(), address: ip, name: name.trim() || undefined }] });
                            setMac("");
                            setIp("");
                            setName("");
                          }}
                        >
                          <IconPlus width={14} height={14} />
                        </button>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </>
            )}
          </>
        )}
      </div>
    </div>
  );
}
