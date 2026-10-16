import { useEffect, useState, type ComponentType, type ReactNode } from "react";
import { navigate, useRoute } from "./lib/router";
import { useApp } from "./lib/state";
import { api } from "./lib/api";
import { lang, setLang, t, type Key } from "./i18n";
import { cx, useToast } from "./components/ui";
import * as I from "./components/icons";
import { Overview } from "./pages/Overview";
import { Interfaces } from "./pages/Interfaces";
import { Routes } from "./pages/Routes";
import { Policy } from "./pages/Policy";
import { Nat } from "./pages/Nat";
import { Objects } from "./pages/Objects";
import { Protection } from "./pages/Protection";
import { Zapret } from "./pages/Zapret";
import { Sessions } from "./pages/Sessions";
import { Events } from "./pages/Events";
import { ConfigPage } from "./pages/Config";
import { Update } from "./pages/Update";
import { Users } from "./pages/Users";
import { SystemPage } from "./pages/System";

interface Page {
  path: string;
  title: Key;
  icon: ComponentType<React.SVGProps<SVGSVGElement>>;
  group: Key;
  view: ComponentType;
  admin?: boolean;
}

const pages: Page[] = [
  { path: "/", title: "nav.overview", icon: I.IconOverview, group: "nav.g.monitor", view: Overview },
  { path: "/sessions", title: "nav.sessions", icon: I.IconSessions, group: "nav.g.monitor", view: Sessions },
  { path: "/events", title: "nav.events", icon: I.IconLog, group: "nav.g.monitor", view: Events },
  { path: "/policy", title: "nav.policy", icon: I.IconShield, group: "nav.g.policy", view: Policy },
  { path: "/nat", title: "nav.nat", icon: I.IconNat, group: "nav.g.policy", view: Nat },
  { path: "/objects", title: "nav.objects", icon: I.IconObjects, group: "nav.g.policy", view: Objects },
  { path: "/protection", title: "nav.protection", icon: I.IconGuard, group: "nav.g.policy", view: Protection },
  { path: "/interfaces", title: "nav.interfaces", icon: I.IconPort, group: "nav.g.network", view: Interfaces },
  { path: "/routes", title: "nav.routes", icon: I.IconRoute, group: "nav.g.network", view: Routes },
  { path: "/zapret", title: "nav.zapret", icon: I.IconTunnel, group: "nav.g.network", view: Zapret },
  { path: "/config", title: "nav.config", icon: I.IconConfig, group: "nav.g.system", view: ConfigPage },
  { path: "/update", title: "nav.update", icon: I.IconUpdate, group: "nav.g.system", view: Update },
  { path: "/users", title: "nav.users", icon: I.IconUsers, group: "nav.g.system", view: Users },
  { path: "/system", title: "nav.system", icon: I.IconSystem, group: "nav.g.system", view: SystemPage },
];

function Countdown({ until }: { until: string }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 500);
    return () => clearInterval(id);
  }, []);
  const left = Math.max(0, Math.floor((new Date(until).getTime() - now) / 1000));
  return (
    <span className="countdown">
      {String(Math.floor(left / 60)).padStart(2, "0")}:{String(left % 60).padStart(2, "0")}
    </span>
  );
}

function Banners() {
  const app = useApp();
  const toast = useToast();
  const items: ReactNode[] = [];

  if (app.running?.state === "pending" && app.running.confirm_deadline) {
    items.push(
      <div key="pending" className="strip warn">
        <span className="dot warn pulse" />
        <span>
          {t("banner.pending", { id: app.running.id })} <Countdown until={app.running.confirm_deadline} />
        </span>
        <div className="actions">
          {app.canEdit && (
            <button
              className="btn btn-primary btn-sm"
              onClick={async () => {
                try {
                  await api.post("/v1/config/confirm");
                  toast(t("banner.confirmed"));
                  app.refresh();
                } catch (e: any) {
                  toast(e.message, true);
                }
              }}
            >
              {t("banner.confirm")}
            </button>
          )}
        </div>
      </div>,
    );
  } else if (app.dirty) {
    items.push(
      <div key="dirty" className="strip accent">
        <span className="dot" style={{ background: "var(--accent)" }} />
        <span>{t("banner.dirty")}</span>
        <div className="actions">
          <button className="btn btn-sm" onClick={() => navigate("/config")}>
            {t("banner.review")}
          </button>
        </div>
      </div>,
    );
  }
  const rb = app.agent?.rollback;
  if (rb && Date.now() - new Date(rb.time).getTime() < 15 * 60 * 1000) {
    items.push(
      <div key="rb" className="strip bad">
        <span className="dot bad" />
        <span>{t("banner.rolledback", { id: rb.revision })}</span>
      </div>,
    );
  }
  if (app.agent?.last_error) {
    items.push(
      <div key="err" className="strip bad">
        <span className="dot bad" />
        <span>
          {t("banner.agenterr")} <span className="mono">{app.agent.last_error}</span>
        </span>
      </div>,
    );
  }
  return <div className="banner">{items}</div>;
}

export function Shell({ onLang }: { onLang: () => void }) {
  const route = useRoute();
  const app = useApp();
  const [navOpen, setNavOpen] = useState(false);
  const [theme, setTheme] = useState(document.documentElement.dataset.theme ?? "dark");
  const visible = pages.filter((p) => !p.admin || app.isAdmin);
  const page = visible.find((p) => p.path === route) ?? visible[0];
  const View = page.view;

  useEffect(() => setNavOpen(false), [route]);
  useEffect(() => {
    document.title = `${t(page.title)} · ${app.candidate?.body.system.hostname || "RendolNet"}`;
  }, [page, app.candidate]);

  const toggleTheme = () => {
    const next = theme === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    try {
      localStorage.setItem("rn.theme", next);
    } catch {
      /* без сохранения */
    }
    setTheme(next);
  };

  let lastGroup = "";
  return (
    <div className={cx("shell", navOpen && "nav-open")}>
      <aside className="side">
        <div className="brand">
          <I.Logo className="brand-mark" />
          <span className="brand-name">
            Rendol<b>Net</b>
          </span>
        </div>
        <nav className="nav">
          {visible.map((p) => {
            const head = p.group !== lastGroup ? <div className="nav-group">{t(p.group)}</div> : null;
            lastGroup = p.group;
            const Icon = p.icon;
            return (
              <div key={p.path} style={{ display: "contents" }}>
                {head}
                <a href={`#${p.path}`} className={cx(p === page && "active")}>
                  <Icon />
                  {t(p.title)}
                  {p.path === "/config" && app.dirty && <span className="count">●</span>}
                </a>
              </div>
            );
          })}
        </nav>
        <div className="side-foot">
          <span className="mono">{app.candidate?.body.system.hostname || "—"}</span>
          <span>
            {t("side.revision")} <span className="mono">{app.running ? `r${app.running.id}` : "—"}</span>
            {app.agent && <> · {app.agent.version}</>}
          </span>
        </div>
      </aside>

      <header className="top">
        <button className="btn btn-ghost btn-icon only-mobile" onClick={() => setNavOpen((v) => !v)} aria-label="menu">
          <I.IconMenu />
        </button>
        <div className="top-title">{t(page.title)}</div>
        <div className="top-right">
          <button
            className="btn btn-ghost btn-sm"
            onClick={() => {
              setLang(lang() === "ru" ? "en" : "ru");
              onLang();
            }}
            title="Language"
          >
            {lang() === "ru" ? "EN" : "RU"}
          </button>
          <button className="btn btn-ghost btn-icon" onClick={toggleTheme} aria-label="theme">
            {theme === "dark" ? <I.IconSun /> : <I.IconMoon />}
          </button>
          <span className="muted" style={{ margin: "0 4px 0 8px" }}>
            {app.user.username}
            <span className="faint"> · {t(`role.${app.user.role}` as Key)}</span>
          </span>
          <button
            className="btn btn-ghost btn-icon"
            title={t("top.logout")}
            onClick={async () => {
              await api.post("/v1/auth/logout").catch(() => {});
              app.setUser(null);
            }}
          >
            <I.IconLogout />
          </button>
        </div>
      </header>

      <Banners />

      <main className="main">{app.candidate ? <View /> : null}</main>
    </div>
  );
}
