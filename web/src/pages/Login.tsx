import { useEffect, useState } from "react";
import { api } from "../lib/api";
import type { User } from "../lib/types";
import { Logo } from "../components/icons";
import { t } from "../i18n";

// Слева — схема сегментов сети вместо картинки: узел посередине, зоны
// вокруг, потоки между ними. Рисуется один раз, без анимаций.
function Topology() {
  const zones = [
    { x: 18, y: 26, n: "wan", d: "203.0.113.0/24" },
    { x: 78, y: 22, n: "lan", d: "192.168.1.0/24" },
    { x: 24, y: 74, n: "dmz", d: "10.10.0.0/24" },
    { x: 80, y: 70, n: "mgmt", d: "10.0.0.0/24" },
  ];
  return (
    <svg viewBox="0 0 100 100" preserveAspectRatio="xMidYMid slice">
      <defs>
        <pattern id="g" width="4" height="4" patternUnits="userSpaceOnUse">
          <path d="M4 0H0V4" fill="none" stroke="var(--line)" strokeWidth="0.08" />
        </pattern>
      </defs>
      <rect width="100" height="100" fill="url(#g)" />
      {zones.map((z) => (
        <g key={z.n}>
          <path d={`M50 48 L${z.x} ${z.y}`} stroke="var(--line-strong)" strokeWidth="0.25" strokeDasharray="0.8 0.8" />
          <rect x={z.x - 7} y={z.y - 3.2} width="14" height="6.4" rx="0.5" fill="var(--surface)" stroke="var(--line-strong)" strokeWidth="0.2" />
          <text x={z.x} y={z.y - 0.2} textAnchor="middle" fontSize="1.9" fill="var(--text-2)" fontFamily="var(--font-mono)">
            {z.n}
          </text>
          <text x={z.x} y={z.y + 2.1} textAnchor="middle" fontSize="1.3" fill="var(--text-3)" fontFamily="var(--font-mono)">
            {z.d}
          </text>
        </g>
      ))}
      <rect x="44" y="44" width="12" height="8" rx="0.6" fill="var(--surface-2)" stroke="var(--accent)" strokeWidth="0.3" />
      <text x="50" y="48.9" textAnchor="middle" fontSize="2" fill="var(--accent)" fontFamily="var(--font-mono)">
        rendol
      </text>
    </svg>
  );
}

export function Login({ onLogin }: { onLogin: (u: User) => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [banner, setBanner] = useState("");

  useEffect(() => {
    api
      .get<{ banner: string }>("/v1/auth/banner")
      .then((b) => setBanner(b.banner))
      .catch(() => {});
  }, []);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      onLogin(await api.post<User>("/v1/auth/login", { username, password }));
    } catch (err: any) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="login">
      <div className="login-art">
        <Topology />
        <div className="caption">
          RendolNet
          <br />
          {t("login.tagline")}
        </div>
      </div>
      <form className="login-form" onSubmit={submit}>
        <Logo width={34} height={34} />
        <div>
          <h1>{t("login.title")}</h1>
          <p className="muted" style={{ marginTop: 6 }}>
            {t("login.hint")}
          </p>
        </div>
        {banner && <div className="note warn" style={{ whiteSpace: "pre-wrap" }}>{banner}</div>}
        <div className="field">
          <label htmlFor="u">{t("login.username")}</label>
          <input id="u" className="input" autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="p">{t("login.password")}</label>
          <input id="p" className="input" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </div>
        {error && <div className="note bad">{error}</div>}
        <button className="btn btn-primary" style={{ height: 34 }} disabled={busy || !username}>
          {busy ? t("login.busy") : t("login.submit")}
        </button>
      </form>
    </div>
  );
}
