import { ru } from "./ru";
import { en } from "./en";

export type Key = keyof typeof ru;
export type Lang = "ru" | "en";

const dicts: Record<Lang, Record<Key, string>> = { ru, en };

function initial(): Lang {
  try {
    const v = localStorage.getItem("rn.lang");
    if (v === "ru" || v === "en") return v;
  } catch {
    /* хранилище может быть недоступно */
  }
  return navigator.language.startsWith("ru") ? "ru" : "en";
}

let current: Lang = initial();
document.documentElement.lang = current;

export function lang(): Lang {
  return current;
}

export function setLang(l: Lang) {
  current = l;
  document.documentElement.lang = l;
  try {
    localStorage.setItem("rn.lang", l);
  } catch {
    /* не страшно */
  }
}

// t("rules.count", { n: 3 }) — подстановка {n}.
export function t(key: Key, vars?: Record<string, string | number>): string {
  let s = dicts[current][key] ?? key;
  if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
  return s;
}

// plural("правило", n) для русского: 1 правило, 2 правила, 5 правил.
export function plural(n: number, forms: [string, string, string]): string {
  const m10 = n % 10;
  const m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return forms[0];
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return forms[1];
  return forms[2];
}
