import { useEffect, useState } from "react";

// Маршрутизация по hash: интерфейс раздаётся как статика, и сервер не
// обязан знать про пути страниц.

export function currentPath(): string {
  const h = window.location.hash.replace(/^#/, "");
  return h || "/";
}

export function navigate(path: string) {
  if (currentPath() !== path) window.location.hash = path;
}

export function useRoute(): string {
  const [path, setPath] = useState(currentPath());
  useEffect(() => {
    const on = () => setPath(currentPath());
    window.addEventListener("hashchange", on);
    return () => window.removeEventListener("hashchange", on);
  }, []);
  return path;
}

export function usePoll<T>(fn: () => Promise<T>, ms: number, deps: unknown[] = []) {
  const [data, setData] = useState<T | undefined>();
  const [error, setError] = useState<string | undefined>();
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let alive = true;
    let timer: number | undefined;
    const run = async () => {
      try {
        const d = await fn();
        if (alive) {
          setData(d);
          setError(undefined);
        }
      } catch (e: any) {
        if (alive) setError(e?.message ?? String(e));
      }
      if (alive && ms > 0) timer = window.setTimeout(run, document.hidden ? ms * 4 : ms);
    };
    run();
    return () => {
      alive = false;
      if (timer) clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick]);
  return { data, error, reload: () => setTick((t) => t + 1), setData };
}
