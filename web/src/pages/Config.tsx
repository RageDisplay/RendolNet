import { useEffect, useRef, useState } from "react";
import { api } from "../lib/api";
import { usePoll } from "../lib/router";
import { useApp } from "../lib/state";
import type { Config, FieldError, Revision, Settings } from "../lib/types";
import { datetime } from "../lib/format";
import { t, type Key } from "../i18n";
import { Confirm, Field, Modal, Seg, cx, useToast } from "../components/ui";
import { DiffView, type DiffLine } from "../components/Diff";
import * as I from "../components/icons";

const stateBadge: Record<string, string> = {
  committed: "ok",
  imported: "neutral",
  pending: "warn",
  rolled_back: "bad",
  failed: "bad",
};

export function ConfigPage() {
  const app = useApp();
  const toast = useToast();
  const [diff, setDiff] = useState<DiffLine[] | null>(null);
  const [commit, setCommit] = useState(false);
  const [discard, setDiscard] = useState(false);
  const [view, setView] = useState<Revision | null>(null);
  const [load, setLoad] = useState<Revision | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const { data: revs, reload } = usePoll(() => api.get<{ items: Revision[]; total: number }>("/v1/config/revisions?limit=100"), 8000);

  const loadDiff = async () => {
    try {
      const d = await api.get<{ lines: DiffLine[] }>("/v1/config/diff");
      setDiff(d.lines);
    } catch (e: any) {
      toast(e.message, true);
    }
  };
  useEffect(() => {
    loadDiff();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [app.candidate?.updated_at, app.running?.id]);

  const importFile = async (f: File) => {
    try {
      const body = JSON.parse(await f.text()) as Config;
      await app.edit((c) => Object.assign(c, body));
      toast(t("cfg.imported"));
    } catch (e: any) {
      toast(t("cfg.import.bad") + ": " + e.message, true);
    }
  };

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("cfg.lead")}</div>
        <div className="actions">
          <input ref={fileRef} type="file" accept="application/json,.json" hidden onChange={(e) => e.target.files?.[0] && importFile(e.target.files[0])} />
          {app.canEdit && (
            <button className="btn" onClick={() => fileRef.current?.click()}>
              {t("cfg.import")}
            </button>
          )}
          <a className="btn" href="/v1/config/export" download>
            <I.IconDownload />
            {t("cfg.export")}
          </a>
        </div>
      </div>

      <div className="panel">
        <div className="panel-head">
          <span className="panel-title">{t("cfg.pending")}</span>
          {app.candidate && (
            <span className="faint" style={{ fontSize: 12 }}>
              {t("cfg.editedby", { who: app.candidate.updated_by || "—", when: datetime(app.candidate.updated_at) })}
            </span>
          )}
          <div className="spacer" />
          {app.canEdit && (
            <>
              <button className="btn btn-sm" disabled={!app.dirty} onClick={() => setDiscard(true)}>
                {t("cfg.discard")}
              </button>
              <button className="btn btn-primary btn-sm" disabled={!app.dirty || app.running?.state === "pending"} onClick={() => setCommit(true)}>
                {t("cfg.commit")}
              </button>
            </>
          )}
        </div>
        <div className="panel-body">
          {app.validation.length > 0 && <Validation errors={app.validation} />}
          {diff ? <DiffView lines={diff} /> : <div className="faint">…</div>}
        </div>
      </div>

      <div className="section">
        <div className="section-head">
          <span className="section-title">{t("cfg.history")}</span>
          <span className="faint" style={{ fontSize: 12 }}>
            {revs ? t("cfg.total", { n: revs.total }) : ""}
          </span>
        </div>
        <div className="table-wrap">
          <table className="t">
            <thead>
              <tr>
                <th>{t("cfg.rev")}</th>
                <th>{t("col.time")}</th>
                <th>{t("cfg.author")}</th>
                <th>{t("col.comment")}</th>
                <th>{t("col.state")}</th>
                <th className="act" />
              </tr>
            </thead>
            <tbody>
              {(revs?.items ?? []).map((r) => (
                <tr key={r.id} className={cx(r.id === app.running?.id && "")}>
                  <td className="mono">
                    r{r.id}
                    {r.id === app.running?.id && <span className="badge accent" style={{ marginLeft: 6 }}>{t("cfg.active")}</span>}
                  </td>
                  <td className="mono faint">{datetime(r.created_at)}</td>
                  <td>{r.author}</td>
                  <td className="muted">
                    {r.comment || <span className="faint">—</span>}
                    {r.error && <div style={{ color: "var(--bad)", fontSize: 12 }}>{r.error}</div>}
                  </td>
                  <td>
                    <span className={cx("badge", stateBadge[r.state])}>{t(`rev.${r.state}` as Key)}</span>
                  </td>
                  <td className="act">
                    <span className="row" style={{ gap: 4 }}>
                      <button className="btn btn-sm" onClick={() => setView(r)}>
                        {t("cfg.changes")}
                      </button>
                      <a className="btn btn-sm btn-ghost btn-icon" href={`/v1/config/export?revision=${r.id}`} download title={t("cfg.export")}>
                        <I.IconDownload />
                      </a>
                      {app.canEdit && r.id !== app.running?.id && (
                        <button className="btn btn-sm btn-ghost" onClick={() => setLoad(r)}>
                          {t("cfg.load")}
                        </button>
                      )}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {commit && (
        <CommitDialog
          onClose={() => setCommit(false)}
          onDone={() => {
            setCommit(false);
            app.refresh();
            reload();
          }}
        />
      )}
      {discard && (
        <Confirm
          title={t("cfg.discard")}
          text={t("cfg.discard.text")}
          action={t("cfg.discard")}
          danger
          onClose={() => setDiscard(false)}
          onConfirm={async () => {
            await api.post("/v1/config/candidate/reset");
            app.refresh();
          }}
        />
      )}
      {view && <RevisionDiff rev={view} prev={revs?.items.find((x) => x.id < view.id && ["committed", "imported"].includes(x.state))} onClose={() => setView(null)} />}
      {load && (
        <Confirm
          title={t("cfg.load.title", { id: load.id })}
          text={t("cfg.load.text")}
          action={t("cfg.load")}
          onClose={() => setLoad(null)}
          onConfirm={async () => {
            try {
              await api.post(`/v1/config/revisions/${load.id}/load`);
              await app.refresh();
              toast(t("cfg.loaded", { id: load.id }));
            } catch (e: any) {
              toast(e.message, true);
            }
          }}
        />
      )}
    </>
  );
}

function Validation({ errors }: { errors: FieldError[] }) {
  return (
    <div className="note bad" style={{ marginBottom: 12 }}>
      <div style={{ fontWeight: 500, color: "var(--text)", marginBottom: 4 }}>{t("cfg.invalid")}</div>
      {errors.map((e, i) => (
        <div key={i}>
          <span className="mono faint">{e.path}</span> — {e.message}
        </div>
      ))}
    </div>
  );
}

function CommitDialog({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const toast = useToast();
  const [comment, setComment] = useState("");
  const [confirm, setConfirm] = useState("120");
  const [busy, setBusy] = useState(false);
  const [errors, setErrors] = useState<FieldError[]>([]);
  const [err, setErr] = useState("");

  useEffect(() => {
    api
      .get<Settings>("/v1/settings")
      .then((s) => setConfirm(String(s.default_confirm_seconds)))
      .catch(() => {});
  }, []);

  const run = async () => {
    setBusy(true);
    setErr("");
    setErrors([]);
    try {
      const r = await api.post<Revision>("/v1/config/commit", { comment, confirm_seconds: Number(confirm) });
      toast(Number(confirm) > 0 ? t("cfg.committed.pending", { id: r.id }) : t("cfg.committed", { id: r.id }));
      onDone();
    } catch (e: any) {
      if (e.body?.errors) setErrors(e.body.errors);
      else setErr(e.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      title={t("cfg.commit")}
      onClose={onClose}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={onClose}>
            {t("common.cancel")}
          </button>
          <button className="btn btn-primary" disabled={busy} onClick={run}>
            {busy ? t("cfg.applying") : t("cfg.apply")}
          </button>
        </>
      }
    >
      <div className="stack">
        <Field label={t("col.comment")} hint={t("cfg.comment.hint")}>
          <input className="input" autoFocus value={comment} onChange={(e) => setComment(e.target.value)} onKeyDown={(e) => e.key === "Enter" && run()} />
        </Field>
        <Field label={t("cfg.confirm")} hint={t("cfg.confirm.hint")}>
          <Seg
            value={confirm}
            onChange={setConfirm}
            options={[
              { value: "0", label: t("cfg.confirm.none") },
              { value: "60", label: "1 " + t("unit.min") },
              { value: "120", label: "2 " + t("unit.min") },
              { value: "300", label: "5 " + t("unit.min") },
              { value: "600", label: "10 " + t("unit.min") },
            ]}
          />
        </Field>
        {errors.length > 0 && <Validation errors={errors} />}
        {err && <div className="note bad">{err}</div>}
      </div>
    </Modal>
  );
}

function RevisionDiff({ rev, prev, onClose }: { rev: Revision; prev?: Revision; onClose: () => void }) {
  const [lines, setLines] = useState<DiffLine[] | null>(null);
  useEffect(() => {
    const q = prev ? `from=${prev.id}&to=${rev.id}` : `to=${rev.id}`;
    api.get<{ lines: DiffLine[] }>(`/v1/config/diff?${q}`).then((d) => setLines(d.lines));
  }, [rev, prev]);
  return (
    <Modal wide title={prev ? t("cfg.diff.title", { a: prev.id, b: rev.id }) : `r${rev.id}`} onClose={onClose}>
      {lines ? <DiffView lines={lines} /> : <div className="progress"><i /></div>}
    </Modal>
  );
}
