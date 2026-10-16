import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { api, ApiError } from "./api";
import type { AgentStatus, Candidate, Config, FieldError, Revision, User } from "./types";

// Общее состояние: пользователь, кандидат конфига, действующая ревизия и
// статус узла (нужен баннер подтверждения на любой странице).

interface AppState {
  user: User;
  candidate?: Candidate;
  running?: Revision;
  agent?: AgentStatus;
  dirty: boolean;
  validation: FieldError[];
  saving: boolean;
  edit: (fn: (c: Config) => void) => Promise<boolean>;
  refresh: () => Promise<void>;
  setUser: (u: User | null) => void;
  canEdit: boolean;
  isAdmin: boolean;
}

const Ctx = createContext<AppState | null>(null);

export function useApp(): AppState {
  const v = useContext(Ctx);
  if (!v) throw new Error("useApp вне AppProvider");
  return v;
}

export function useConfig(): Config | undefined {
  return useApp().candidate?.body;
}

function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v));
}

export function AppProvider({ user, setUser, children }: { user: User; setUser: (u: User | null) => void; children: ReactNode }) {
  const [candidate, setCandidate] = useState<Candidate>();
  const [running, setRunning] = useState<Revision>();
  const [agent, setAgent] = useState<AgentStatus>();
  const [dirty, setDirty] = useState(false);
  const [validation, setValidation] = useState<FieldError[]>([]);
  const [saving, setSaving] = useState(false);
  const candRef = useRef<Candidate>();
  candRef.current = candidate;

  const refresh = useCallback(async () => {
    const [c, r, d] = await Promise.allSettled([
      api.get<Candidate>("/v1/config/candidate"),
      api.get<Revision>("/v1/config/running"),
      api.get<{ changed: boolean }>("/v1/config/diff"),
    ]);
    if (c.status === "fulfilled") setCandidate(c.value);
    if (r.status === "fulfilled") setRunning(r.value);
    if (d.status === "fulfilled") setDirty(d.value.changed);
  }, []);

  useEffect(() => {
    refresh();
    let alive = true;
    const poll = async () => {
      try {
        const s = await api.get<{ agent?: AgentStatus }>("/v1/system");
        if (alive) setAgent(s.agent);
        const r = await api.get<Revision>("/v1/config/running");
        if (alive) setRunning(r);
      } catch {
        /* следующая попытка */
      }
      if (alive) setTimeout(poll, 4000);
    };
    poll();
    return () => {
      alive = false;
    };
  }, [refresh]);

  const edit = useCallback(async (fn: (c: Config) => void) => {
    const cur = candRef.current;
    if (!cur) return false;
    const body = clone(cur.body);
    fn(body);
    setSaving(true);
    try {
      const res = await api.put<{ candidate: Candidate; validation?: { errors: FieldError[] } }>("/v1/config/candidate", {
        body,
        base_revision: cur.base_revision,
        if_unmodified: cur.updated_at,
      });
      setCandidate(res.candidate);
      setValidation(res.validation?.errors ?? []);
      const d = await api.get<{ changed: boolean }>("/v1/config/diff");
      setDirty(d.changed);
      return true;
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) await refresh();
      throw e;
    } finally {
      setSaving(false);
    }
  }, [refresh]);

  const value: AppState = {
    user,
    candidate,
    running,
    agent,
    dirty,
    validation,
    saving,
    edit,
    refresh,
    setUser,
    canEdit: user.role === "admin" || user.role === "operator",
    isAdmin: user.role === "admin",
  };
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
