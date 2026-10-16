import { useState } from "react";
import { api } from "../lib/api";
import { usePoll } from "../lib/router";
import { useApp } from "../lib/state";
import type { User } from "../lib/types";
import { ago, datetime } from "../lib/format";
import { t, type Key } from "../i18n";
import { Confirm, Field, Modal, Seg, Switch, cx, useToast } from "../components/ui";
import * as I from "../components/icons";

interface Token {
  id: number;
  name: string;
  created_at: string;
  last_used?: string;
  expires_at?: string;
}

export function Users() {
  const app = useApp();
  const toast = useToast();
  const users = usePoll(() => (app.isAdmin ? api.get<User[]>("/v1/users") : Promise.resolve([app.user])), 0);
  const tokens = usePoll(() => api.get<Token[] | null>("/v1/tokens"), 0);
  const [edit, setEdit] = useState<User | null>(null);
  const [creating, setCreating] = useState(false);
  const [pw, setPw] = useState<User | null>(null);
  const [del, setDel] = useState<User | null>(null);
  const [own, setOwn] = useState(false);
  const [newToken, setNewToken] = useState(false);
  const [shown, setShown] = useState<string | null>(null);

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("users.lead")}</div>
        <div className="actions">
          <button className="btn" onClick={() => setOwn(true)}>
            {t("users.ownpw")}
          </button>
          {app.isAdmin && (
            <button className="btn btn-primary" onClick={() => setCreating(true)}>
              <I.IconPlus />
              {t("users.add")}
            </button>
          )}
        </div>
      </div>

      <div className="table-wrap">
        <table className="t">
          <thead>
            <tr>
              <th>{t("login.username")}</th>
              <th>{t("users.fullname")}</th>
              <th>{t("users.role")}</th>
              <th>{t("users.lastlogin")}</th>
              <th>{t("col.state")}</th>
              <th className="act" />
            </tr>
          </thead>
          <tbody>
            {(users.data ?? []).map((u) => (
              <tr key={u.id} className={cx(u.disabled && "off")}>
                <td className="mono" style={{ fontWeight: 500 }}>
                  {u.username}
                  {u.id === app.user.id && <span className="badge neutral" style={{ marginLeft: 6 }}>{t("users.you")}</span>}
                </td>
                <td>{u.full_name || <span className="faint">—</span>}</td>
                <td>
                  <span className={cx("badge", u.role === "admin" ? "accent" : u.role === "operator" ? "info" : "neutral")}>{t(`role.${u.role}` as Key)}</span>
                </td>
                <td className="faint">{u.last_login ? ago(u.last_login) : t("users.never")}</td>
                <td>{u.disabled ? <span className="badge bad">{t("users.disabled")}</span> : <span className="badge ok">{t("users.active")}</span>}</td>
                <td className="act">
                  {app.isAdmin && (
                    <span className="row" style={{ gap: 4 }}>
                      <button className="btn btn-sm btn-ghost" onClick={() => setEdit(u)}>
                        {t("common.edit")}
                      </button>
                      <button className="btn btn-sm btn-ghost" onClick={() => setPw(u)}>
                        {t("users.setpw")}
                      </button>
                      <button className="btn btn-sm btn-ghost btn-icon" disabled={u.id === app.user.id} onClick={() => setDel(u)}>
                        <I.IconTrash />
                      </button>
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="section">
        <div className="section-head">
          <span className="section-title">{t("users.tokens")}</span>
          <div className="actions">
            <button className="btn btn-sm" onClick={() => setNewToken(true)}>
              <I.IconPlus />
              {t("users.newtoken")}
            </button>
          </div>
        </div>
        <div className="note" style={{ marginBottom: 10 }}>
          {t("users.tokens.hint")}
        </div>
        <div className="table-wrap">
          <table className="t">
            <thead>
              <tr>
                <th>{t("col.name")}</th>
                <th>{t("users.created")}</th>
                <th>{t("users.lastused")}</th>
                <th>{t("users.expires")}</th>
                <th className="act" />
              </tr>
            </thead>
            <tbody>
              {(tokens.data ?? []).map((tk) => (
                <tr key={tk.id}>
                  <td style={{ fontWeight: 500 }}>{tk.name}</td>
                  <td className="mono faint">{datetime(tk.created_at)}</td>
                  <td className="faint">{tk.last_used ? ago(tk.last_used) : t("users.never")}</td>
                  <td className="mono faint">{tk.expires_at ? datetime(tk.expires_at) : t("users.noexp")}</td>
                  <td className="act">
                    <button
                      className="btn btn-sm btn-ghost btn-icon"
                      onClick={async () => {
                        await api.del(`/v1/tokens/${tk.id}`);
                        tokens.reload();
                      }}
                    >
                      <I.IconTrash />
                    </button>
                  </td>
                </tr>
              ))}
              {(tokens.data ?? []).length === 0 && (
                <tr>
                  <td colSpan={5} className="empty">
                    {t("users.notokens")}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {(creating || edit) && (
        <UserModal
          user={edit}
          onClose={() => {
            setCreating(false);
            setEdit(null);
          }}
          onDone={() => {
            setCreating(false);
            setEdit(null);
            users.reload();
          }}
        />
      )}
      {pw && (
        <PasswordModal
          title={t("users.setpw.for", { u: pw.username })}
          onClose={() => setPw(null)}
          onSave={async (p) => {
            await api.post(`/v1/users/${pw.id}/password`, { password: p });
            toast(t("users.pwchanged"));
            setPw(null);
          }}
        />
      )}
      {own && (
        <PasswordModal
          title={t("users.ownpw")}
          withCurrent
          onClose={() => setOwn(false)}
          onSave={async (p, cur) => {
            await api.post("/v1/auth/password", { current: cur, new: p });
            toast(t("users.pwchanged"));
            setOwn(false);
          }}
        />
      )}
      {del && (
        <Confirm
          title={t("users.delete")}
          text={<span className="mono">{del.username}</span>}
          action={t("common.delete")}
          danger
          onClose={() => setDel(null)}
          onConfirm={async () => {
            try {
              await api.del(`/v1/users/${del.id}`);
              users.reload();
            } catch (e: any) {
              toast(e.message, true);
            }
          }}
        />
      )}
      {newToken && (
        <TokenModal
          onClose={() => setNewToken(false)}
          onCreated={(tok) => {
            setNewToken(false);
            setShown(tok);
            tokens.reload();
          }}
        />
      )}
      {shown && (
        <Modal title={t("users.tokencreated")} onClose={() => setShown(null)}>
          <div className="stack">
            <div className="note warn">{t("users.tokenonce")}</div>
            <div className="row">
              <input className="input mono" readOnly value={shown} onFocus={(e) => e.target.select()} />
              <button
                className="btn btn-icon"
                onClick={() => {
                  navigator.clipboard?.writeText(shown);
                  toast(t("common.copied"));
                }}
              >
                <I.IconCopy />
              </button>
            </div>
          </div>
        </Modal>
      )}
    </>
  );
}

function UserModal({ user, onClose, onDone }: { user: User | null; onClose: () => void; onDone: () => void }) {
  const toast = useToast();
  const [u, setU] = useState({ username: user?.username ?? "", full_name: user?.full_name ?? "", role: user?.role ?? "operator", disabled: user?.disabled ?? false, password: "" });
  const save = async () => {
    try {
      if (user) await api.put(`/v1/users/${user.id}`, { full_name: u.full_name, role: u.role, disabled: u.disabled });
      else await api.post("/v1/users", u);
      onDone();
    } catch (e: any) {
      toast(e.message, true);
    }
  };
  return (
    <Modal
      title={user ? user.username : t("users.add")}
      onClose={onClose}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={onClose}>
            {t("common.cancel")}
          </button>
          <button className="btn btn-primary" disabled={!u.username || (!user && !u.password)} onClick={save}>
            {t("common.save")}
          </button>
        </>
      }
    >
      <div className="stack">
        <Field label={t("login.username")}>
          <input className="input mono" disabled={!!user} autoFocus value={u.username} onChange={(e) => setU({ ...u, username: e.target.value.trim() })} />
        </Field>
        <Field label={t("users.fullname")}>
          <input className="input" value={u.full_name} onChange={(e) => setU({ ...u, full_name: e.target.value })} />
        </Field>
        {!user && (
          <Field label={t("login.password")}>
            <input className="input" type="password" value={u.password} onChange={(e) => setU({ ...u, password: e.target.value })} />
          </Field>
        )}
        <Field label={t("users.role")} hint={t(`role.${u.role}.hint` as Key)}>
          <Seg
            value={u.role}
            onChange={(v) => setU({ ...u, role: v })}
            options={[
              { value: "viewer", label: t("role.viewer") },
              { value: "operator", label: t("role.operator") },
              { value: "admin", label: t("role.admin") },
            ]}
          />
        </Field>
        {user && <Switch checked={u.disabled} onChange={(v) => setU({ ...u, disabled: v })} label={t("users.disabled")} />}
      </div>
    </Modal>
  );
}

function PasswordModal({ title, withCurrent, onClose, onSave }: { title: string; withCurrent?: boolean; onClose: () => void; onSave: (p: string, cur: string) => Promise<void> }) {
  const toast = useToast();
  const [cur, setCur] = useState("");
  const [p, setP] = useState("");
  return (
    <Modal
      title={title}
      onClose={onClose}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={onClose}>
            {t("common.cancel")}
          </button>
          <button className="btn btn-primary" disabled={!p} onClick={() => onSave(p, cur).catch((e) => toast(e.message, true))}>
            {t("common.save")}
          </button>
        </>
      }
    >
      <div className="stack">
        {withCurrent && (
          <Field label={t("users.curpw")}>
            <input className="input" type="password" autoFocus value={cur} onChange={(e) => setCur(e.target.value)} />
          </Field>
        )}
        <Field label={t("users.newpw")}>
          <input className="input" type="password" autoFocus={!withCurrent} value={p} onChange={(e) => setP(e.target.value)} />
        </Field>
      </div>
    </Modal>
  );
}

function TokenModal({ onClose, onCreated }: { onClose: () => void; onCreated: (t: string) => void }) {
  const toast = useToast();
  const [name, setName] = useState("");
  const [days, setDays] = useState("0");
  return (
    <Modal
      title={t("users.newtoken")}
      onClose={onClose}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={onClose}>
            {t("common.cancel")}
          </button>
          <button
            className="btn btn-primary"
            disabled={!name.trim()}
            onClick={async () => {
              try {
                const r = await api.post<{ token: string }>("/v1/tokens", { name, days: Number(days) });
                onCreated(r.token);
              } catch (e: any) {
                toast(e.message, true);
              }
            }}
          >
            {t("users.create")}
          </button>
        </>
      }
    >
      <div className="stack">
        <Field label={t("col.name")} hint={t("users.token.name.hint")}>
          <input className="input" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={t("users.expires")}>
          <Seg
            value={days}
            onChange={setDays}
            options={[
              { value: "30", label: "30 " + t("unit.days") },
              { value: "90", label: "90 " + t("unit.days") },
              { value: "365", label: "365 " + t("unit.days") },
              { value: "0", label: t("users.noexp") },
            ]}
          />
        </Field>
      </div>
    </Modal>
  );
}
