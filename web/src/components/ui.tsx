import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { IconX } from "./icons";
import { zoneColor } from "../lib/format";
import { t } from "../i18n";

export function cx(...c: (string | false | null | undefined)[]): string {
  return c.filter(Boolean).join(" ");
}

export function Field(p: { label: ReactNode; hint?: ReactNode; error?: string; children: ReactNode; className?: string }) {
  return (
    <div className={cx("field", p.className)}>
      <label>{p.label}</label>
      {p.children}
      {p.error ? <span className="err">{p.error}</span> : p.hint ? <span className="hint">{p.hint}</span> : null}
    </div>
  );
}

export function Switch(p: { checked: boolean; onChange: (v: boolean) => void; label?: ReactNode; disabled?: boolean }) {
  return (
    <label className="switch" onClick={(e) => e.stopPropagation()}>
      <input type="checkbox" checked={p.checked} disabled={p.disabled} onChange={(e) => p.onChange(e.target.checked)} />
      <span className="track" />
      {p.label}
    </label>
  );
}

export function Seg<T extends string>(p: { value: T; options: { value: T; label: ReactNode }[]; onChange: (v: T) => void }) {
  return (
    <div className="seg">
      {p.options.map((o) => (
        <button key={o.value} type="button" className={cx(o.value === p.value && "on")} onClick={() => p.onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function ZoneChip({ name }: { name: string }) {
  return (
    <span className="chip zone" style={{ ["--zone" as any]: zoneColor(name) }}>
      {name === "self" ? t("zone.self") : name}
    </span>
  );
}

export function Chips({ items, mono, empty }: { items?: string[]; mono?: boolean; empty?: ReactNode }) {
  if (!items || items.length === 0 || (items.length === 1 && items[0] === "any"))
    return <span className="faint">{empty ?? t("common.any")}</span>;
  return (
    <span className="chips">
      {items.map((i) => (
        <span key={i} className={cx("chip", mono && "mono")}>
          {i}
        </span>
      ))}
    </span>
  );
}

export function ActionLabel({ action }: { action: string }) {
  return <span className={cx("action-cell", `action-${action}`)}>{t(`action.${action}` as any)}</span>;
}

function useEscape(fn: () => void) {
  useEffect(() => {
    const h = (e: KeyboardEvent) => e.key === "Escape" && fn();
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, [fn]);
}

export function Drawer(p: { title: ReactNode; onClose: () => void; children: ReactNode; footer?: ReactNode; extra?: ReactNode }) {
  useEscape(p.onClose);
  return (
    <>
      <div className="scrim" onClick={p.onClose} />
      <aside className="drawer" role="dialog">
        <div className="drawer-head">
          <div className="drawer-title">{p.title}</div>
          <div className="spacer" />
          {p.extra}
          <button className="btn btn-ghost btn-icon" onClick={p.onClose} aria-label={t("common.close")}>
            <IconX />
          </button>
        </div>
        <div className="drawer-body">{p.children}</div>
        {p.footer && <div className="drawer-foot">{p.footer}</div>}
      </aside>
    </>
  );
}

export function Modal(p: { title: ReactNode; onClose: () => void; children: ReactNode; footer?: ReactNode; wide?: boolean }) {
  useEscape(p.onClose);
  return (
    <>
      <div className="scrim" onClick={p.onClose} />
      <div className={cx("modal", p.wide && "wide")} role="dialog">
        <div className="drawer-head">
          <div className="drawer-title">{p.title}</div>
          <div className="spacer" />
          <button className="btn btn-ghost btn-icon" onClick={p.onClose} aria-label={t("common.close")}>
            <IconX />
          </button>
        </div>
        <div className="drawer-body">{p.children}</div>
        {p.footer && <div className="drawer-foot">{p.footer}</div>}
      </div>
    </>
  );
}

// ---- уведомления ----

interface Toast {
  id: number;
  text: string;
  bad?: boolean;
}
const ToastCtx = createContext<(text: string, bad?: boolean) => void>(() => {});
export const useToast = () => useContext(ToastCtx);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<Toast[]>([]);
  const seq = useRef(0);
  const push = useCallback((text: string, bad?: boolean) => {
    const id = ++seq.current;
    setItems((x) => [...x, { id, text, bad }]);
    setTimeout(() => setItems((x) => x.filter((i) => i.id !== id)), bad ? 7000 : 3500);
  }, []);
  return (
    <ToastCtx.Provider value={push}>
      {children}
      <div className="toasts">
        {items.map((i) => (
          <div key={i.id} className={cx("toast", i.bad && "bad")}>
            {i.text}
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  );
}

// ---- подтверждение ----

export function Confirm(p: {
  title: string;
  text: ReactNode;
  action: string;
  danger?: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <Modal
      title={p.title}
      onClose={p.onClose}
      footer={
        <>
          <div className="spacer" />
          <button className="btn" onClick={p.onClose}>
            {t("common.cancel")}
          </button>
          <button
            className={cx("btn", p.danger ? "btn-danger" : "btn-primary")}
            onClick={() => {
              p.onConfirm();
              p.onClose();
            }}
          >
            {p.action}
          </button>
        </>
      }
    >
      <div className="muted">{p.text}</div>
    </Modal>
  );
}

export function EmptyState({ title, children }: { title: ReactNode; children?: ReactNode }) {
  return (
    <div className="empty-state">
      <b>{title}</b>
      {children}
    </div>
  );
}

export function Meter({ value }: { value: number }) {
  const v = Math.max(0, Math.min(100, value));
  return (
    <div className={cx("meter", v > 75 && "hot", v > 90 && "crit")}>
      <i style={{ width: `${v}%` }} />
    </div>
  );
}
