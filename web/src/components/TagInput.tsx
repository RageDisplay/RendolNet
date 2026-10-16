import { useMemo, useRef, useState } from "react";
import { cx } from "./ui";

export interface Suggestion {
  value: string;
  kind?: string;
}

// Список значений с подсказками: объекты, зоны, сервисы. Свободный ввод
// разрешён — литерал (адрес, сеть, tcp/443) проверит сервер.
export function TagInput(p: {
  value: string[];
  onChange: (v: string[]) => void;
  suggestions?: Suggestion[];
  placeholder?: string;
  strict?: boolean;
  mono?: boolean;
}) {
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [hi, setHi] = useState(0);
  const input = useRef<HTMLInputElement>(null);

  const options = useMemo(() => {
    const q = text.trim().toLowerCase();
    return (p.suggestions ?? []).filter((s) => !p.value.includes(s.value) && (!q || s.value.toLowerCase().includes(q))).slice(0, 40);
  }, [text, p.suggestions, p.value]);

  const add = (v: string) => {
    v = v.trim();
    if (!v || p.value.includes(v)) return;
    if (p.strict && !(p.suggestions ?? []).some((s) => s.value === v)) return;
    p.onChange([...p.value, v]);
    setText("");
    setHi(0);
  };

  return (
    <div style={{ position: "relative" }}>
      <div className="taglist" onClick={() => input.current?.focus()}>
        {p.value.map((v) => (
          <span key={v} className={cx("chip", p.mono !== false && "mono")}>
            {v}
            <button type="button" onClick={() => p.onChange(p.value.filter((x) => x !== v))} aria-label="×">
              ×
            </button>
          </span>
        ))}
        <input
          ref={input}
          value={text}
          placeholder={p.value.length === 0 ? p.placeholder : ""}
          onFocus={() => setOpen(true)}
          onBlur={() => setTimeout(() => setOpen(false), 120)}
          onChange={(e) => {
            setText(e.target.value);
            setOpen(true);
            setHi(0);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === "," || e.key === "Tab") {
              if (text.trim()) {
                e.preventDefault();
                add(open && options[hi] && (p.strict || options[hi].value.toLowerCase().startsWith(text.trim().toLowerCase())) ? options[hi].value : text);
              }
            } else if (e.key === "Backspace" && !text && p.value.length) {
              p.onChange(p.value.slice(0, -1));
            } else if (e.key === "ArrowDown") {
              e.preventDefault();
              setHi((h) => Math.min(h + 1, options.length - 1));
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setHi((h) => Math.max(h - 1, 0));
            }
          }}
        />
      </div>
      {open && options.length > 0 && (
        <div className="suggest">
          {options.map((o, i) => (
            <button
              type="button"
              key={o.value}
              className={cx(i === hi && "on")}
              onMouseDown={(e) => {
                e.preventDefault();
                add(o.value);
              }}
            >
              <span className="mono">{o.value}</span>
              {o.kind && <span className="kind">{o.kind}</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
