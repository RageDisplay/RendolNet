import { useEffect, useState } from "react";
import { api, setUnauthorizedHandler } from "./lib/api";
import type { User } from "./lib/types";
import { AppProvider } from "./lib/state";
import { ToastProvider } from "./components/ui";
import { Login } from "./pages/Login";
import { Shell } from "./Shell";

export function App() {
  const [user, setUser] = useState<User | null | undefined>(undefined);
  const [, setLangTick] = useState(0);

  useEffect(() => {
    setUnauthorizedHandler(() => setUser(null));
    api
      .get<User>("/v1/auth/me")
      .then(setUser)
      .catch(() => setUser(null));
  }, []);

  if (user === undefined) return null;
  return (
    <ToastProvider>
      {user === null ? (
        <Login onLogin={setUser} />
      ) : (
        <AppProvider user={user} setUser={setUser}>
          <Shell onLang={() => setLangTick((x) => x + 1)} />
        </AppProvider>
      )}
    </ToastProvider>
  );
}
