import { useRef, useState } from "react";
import { api } from "../lib/api";
import { usePoll } from "../lib/router";
import { useApp } from "../lib/state";
import type { UpdateManifest, UpdateStatus } from "../lib/types";
import { bytes, datetime } from "../lib/format";
import { t, type Key } from "../i18n";
import { Confirm, cx, useToast } from "../components/ui";

interface Resp {
  status: UpdateStatus;
  current: string;
  staged?: UpdateManifest;
  staged_cmp?: number;
}

export function Update() {
  const app = useApp();
  const toast = useToast();
  const { data, reload } = usePoll(() => api.get<Resp>("/v1/update"), 3000);
  const [uploading, setUploading] = useState<{ name: string; size: number; sent: number } | null>(null);
  const [over, setOver] = useState(false);
  const [apply, setApply] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const st = data?.status;
  const busy = st?.state === "queued" || st?.state === "running";

  // XMLHttpRequest вместо fetch: нужен прогресс отправки многосотмегабайтного файла.
  const upload = (f: File) => {
    setUploading({ name: f.name, size: f.size, sent: 0 });
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/v1/update/upload");
    xhr.setRequestHeader("X-RN-Request", "1");
    xhr.upload.onprogress = (e) => setUploading((u) => (u ? { ...u, sent: e.loaded } : u));
    xhr.onload = () => {
      setUploading(null);
      if (xhr.status >= 200 && xhr.status < 300) {
        toast(t("upd.uploaded"));
      } else {
        let msg = xhr.statusText;
        try {
          msg = JSON.parse(xhr.responseText).error;
        } catch {
          /* текст как есть */
        }
        toast(msg, true);
      }
      reload();
    };
    xhr.onerror = () => {
      setUploading(null);
      toast(t("upd.neterr"), true);
    };
    xhr.send(f);
  };

  return (
    <>
      <div className="page-head">
        <div className="lead">{t("upd.lead")}</div>
      </div>

      <div className="grid grid-2">
        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">{t("upd.installed")}</span>
          </div>
          <div className="panel-body">
            <div className="stat-value">{data?.current ?? "—"}</div>
            {st && st.state !== "idle" && (
              <div className="stack" style={{ marginTop: 14 }}>
                <div className="row">
                  <span className={cx("badge", st.state === "done" ? "ok" : st.state === "failed" || st.state === "rolled_back" ? "bad" : "warn")}>
                    {t(`upd.state.${st.state}` as Key)}
                  </span>
                  <span className="muted">
                    {st.action === "update" ? t("upd.to", { v: st.version ?? "" }) : st.action}
                    {st.step && <> · {st.step}</>}
                  </span>
                  <span className="spacer" />
                  <span className="faint mono" style={{ fontSize: 12 }}>
                    {datetime(st.finished ?? st.started)}
                  </span>
                </div>
                {busy && (
                  <div className="progress">
                    <i />
                  </div>
                )}
                {st.message && <div className={cx("note", st.state === "failed" || st.state === "rolled_back" ? "bad" : "")}>{st.message}</div>}
                {st.log && st.log.length > 0 && <div className="log">{st.log.join("\n")}</div>}
              </div>
            )}
          </div>
        </div>

        <div className="panel">
          <div className="panel-head">
            <span className="panel-title">{t("upd.package")}</span>
            {data?.staged && app.isAdmin && !busy && (
              <>
                <div className="spacer" />
                <button
                  className="btn btn-sm btn-ghost"
                  onClick={async () => {
                    await api.del("/v1/update/staged");
                    reload();
                  }}
                >
                  {t("upd.discard")}
                </button>
              </>
            )}
          </div>
          <div className="panel-body stack">
            {data?.staged ? (
              <>
                <dl className="kv">
                  <dt>{t("ov.version")}</dt>
                  <dd className="mono">
                    {data.staged.version}
                    {data.staged_cmp === 0 && <span className="badge neutral" style={{ marginLeft: 8 }}>{t("upd.same")}</span>}
                  </dd>
                  <dt>{t("upd.built")}</dt>
                  <dd className="mono">{datetime(data.staged.created)}</dd>
                  {data.staged.k3s && (
                    <>
                      <dt>k3s</dt>
                      <dd className="mono">{data.staged.k3s}</dd>
                    </>
                  )}
                  <dt>{t("upd.images")}</dt>
                  <dd className="mono" style={{ fontSize: 12 }}>
                    {data.staged.images.join(", ")}
                  </dd>
                </dl>
                {data.staged.notes && <div className="log">{data.staged.notes}</div>}
                {app.isAdmin && (
                  <button className="btn btn-primary" disabled={busy} onClick={() => setApply(true)}>
                    {t("upd.install", { v: data.staged.version })}
                  </button>
                )}
              </>
            ) : uploading ? (
              <div className="stack">
                <div className="row">
                  <span className="mono">{uploading.name}</span>
                  <span className="spacer" />
                  <span className="mono faint">
                    {bytes(uploading.sent)} / {bytes(uploading.size)}
                  </span>
                </div>
                <div className="meter">
                  <i style={{ width: `${(uploading.sent / Math.max(1, uploading.size)) * 100}%`, background: "var(--accent)" }} />
                </div>
                <div className="faint" style={{ fontSize: 12 }}>
                  {uploading.sent >= uploading.size ? t("upd.verifying") : t("upd.uploading")}
                </div>
              </div>
            ) : app.isAdmin ? (
              <div
                className={cx("drop", over && "over")}
                onClick={() => input.current?.click()}
                onDragOver={(e) => {
                  e.preventDefault();
                  setOver(true);
                }}
                onDragLeave={() => setOver(false)}
                onDrop={(e) => {
                  e.preventDefault();
                  setOver(false);
                  const f = e.dataTransfer.files[0];
                  if (f) upload(f);
                }}
              >
                <div style={{ fontWeight: 500, color: "var(--text)" }}>{t("upd.drop")}</div>
                <div className="faint" style={{ marginTop: 4 }}>
                  rendolnet-X.Y.Z.rnu
                </div>
                <input ref={input} type="file" hidden accept=".rnu,.tar" onChange={(e) => e.target.files?.[0] && upload(e.target.files[0])} />
              </div>
            ) : (
              <div className="faint">{t("upd.adminonly")}</div>
            )}
            <div className="note">{t("upd.note")}</div>
          </div>
        </div>
      </div>

      {apply && data?.staged && (
        <Confirm
          title={t("upd.install", { v: data.staged.version })}
          text={t("upd.confirm")}
          action={t("upd.go")}
          onClose={() => setApply(false)}
          onConfirm={async () => {
            try {
              await api.post("/v1/update/apply");
              reload();
            } catch (e: any) {
              toast(e.message, true);
            }
          }}
        />
      )}
    </>
  );
}
