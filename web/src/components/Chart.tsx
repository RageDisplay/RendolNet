import { useMemo, useRef, useState } from "react";
import { bps, time } from "../lib/format";

export interface Series {
  label: string;
  color: string;
  values: number[];
}

function niceMax(v: number): number {
  if (v <= 0) return 1000;
  const p = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 2, 2.5, 5, 10]) if (m * p >= v) return m * p;
  return 10 * p;
}

// Двухсерийный график скорости: приём — заливка, передача — линия.
export function TrafficChart({ times, series, height = 180 }: { times: string[]; series: Series[]; height?: number }) {
  const ref = useRef<SVGSVGElement>(null);
  const [hover, setHover] = useState<number | null>(null);
  const W = 800;
  const H = height;
  const padL = 64;
  const padB = 20;
  const padT = 8;
  const n = times.length;
  const max = useMemo(() => niceMax(Math.max(0, ...series.flatMap((s) => s.values))), [series]);
  const x = (i: number) => padL + (n <= 1 ? 0 : (i / (n - 1)) * (W - padL - 4));
  const y = (v: number) => padT + (1 - v / max) * (H - padT - padB);

  const path = (vals: number[]) => vals.map((v, i) => `${i ? "L" : "M"}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join("");
  const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => f * max);
  const xTicks = n > 1 ? [0, Math.floor(n / 3), Math.floor((2 * n) / 3), n - 1] : [];

  const onMove = (e: React.MouseEvent) => {
    const r = ref.current!.getBoundingClientRect();
    const px = ((e.clientX - r.left) / r.width) * W;
    if (px < padL || n < 2) return setHover(null);
    setHover(Math.round(((px - padL) / (W - padL - 4)) * (n - 1)));
  };

  return (
    <div style={{ position: "relative" }}>
      <svg ref={ref} className="chart" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" style={{ height }} onMouseMove={onMove} onMouseLeave={() => setHover(null)}>
        {ticks.map((v) => (
          <g key={v}>
            <line className="grid-line" x1={padL} x2={W} y1={y(v)} y2={y(v)} />
            <text className="axis" x={padL - 8} y={y(v) + 3} textAnchor="end">
              {bps(v)}
            </text>
          </g>
        ))}
        {xTicks.map((i) => (
          <text key={i} className="axis" x={x(i)} y={H - 5} textAnchor={i === 0 ? "start" : i === n - 1 ? "end" : "middle"}>
            {time(times[i]).slice(0, 5)}
          </text>
        ))}
        {series.map((s, si) =>
          si === 0 ? (
            <g key={s.label}>
              <path d={`${path(s.values)}L${x(n - 1)},${y(0)}L${x(0)},${y(0)}Z`} fill={s.color} opacity={0.14} />
              <path d={path(s.values)} fill="none" stroke={s.color} strokeWidth={1.5} vectorEffect="non-scaling-stroke" />
            </g>
          ) : (
            <path key={s.label} d={path(s.values)} fill="none" stroke={s.color} strokeWidth={1.5} strokeDasharray="4 3" vectorEffect="non-scaling-stroke" />
          ),
        )}
        {hover !== null && hover >= 0 && hover < n && (
          <line x1={x(hover)} x2={x(hover)} y1={padT} y2={H - padB} stroke="var(--text-3)" strokeWidth={1} vectorEffect="non-scaling-stroke" />
        )}
      </svg>
      {hover !== null && hover >= 0 && hover < n && (
        <div
          className="panel"
          style={{
            position: "absolute",
            top: 4,
            left: `${(x(hover) / W) * 100}%`,
            transform: x(hover) > W * 0.7 ? "translateX(calc(-100% - 10px))" : "translateX(10px)",
            padding: "6px 10px",
            pointerEvents: "none",
            fontSize: 12,
            boxShadow: "var(--shadow)",
          }}
        >
          <div className="faint mono">{time(times[hover])}</div>
          {series.map((s) => (
            <div key={s.label} className="row" style={{ gap: 6 }}>
              <i style={{ width: 8, height: 2, background: s.color, display: "inline-block" }} />
              <span className="muted">{s.label}</span>
              <span className="mono" style={{ marginLeft: "auto", paddingLeft: 12 }}>
                {bps(s.values[hover] ?? 0)}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export function Sparkline({ values, color = "var(--text-2)", width = 90, height = 22 }: { values: number[]; color?: string; width?: number; height?: number }) {
  if (values.length < 2) return <svg width={width} height={height} />;
  const max = Math.max(1, ...values);
  const pts = values.map((v, i) => `${((i / (values.length - 1)) * width).toFixed(1)},${(height - 1 - (v / max) * (height - 2)).toFixed(1)}`);
  return (
    <svg width={width} height={height} style={{ display: "block" }}>
      <polyline points={pts.join(" ")} fill="none" stroke={color} strokeWidth={1.25} />
    </svg>
  );
}
