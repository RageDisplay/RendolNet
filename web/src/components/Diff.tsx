import { t } from "../i18n";

export interface DiffLine {
  op: " " | "+" | "-";
  text: string;
}

// Показывает изменённые строки с тремя строками контекста; длинные
// неизменённые куски сворачиваются.
export function DiffView({ lines }: { lines: DiffLine[] }) {
  const ctx = 3;
  const keep = new Array(lines.length).fill(false);
  lines.forEach((l, i) => {
    if (l.op !== " ") for (let j = Math.max(0, i - ctx); j <= Math.min(lines.length - 1, i + ctx); j++) keep[j] = true;
  });
  const out: JSX.Element[] = [];
  let skipped = 0;
  lines.forEach((l, i) => {
    if (!keep[i]) {
      skipped++;
      return;
    }
    if (skipped) {
      out.push(<div key={`g${i}`} className="gap">{t("diff.skipped", { n: skipped })}</div>);
      skipped = 0;
    }
    out.push(
      <div key={i} data-op={l.op === " " ? "" : l.op} className={l.op === "+" ? "add" : l.op === "-" ? "del" : ""}>
        {l.text}
      </div>,
    );
  });
  if (skipped) out.push(<div key="gend" className="gap">{t("diff.skipped", { n: skipped })}</div>);
  if (!lines.some((l) => l.op !== " ")) return <div className="empty-state">{t("diff.none")}</div>;
  return <div className="diff">{out}</div>;
}
