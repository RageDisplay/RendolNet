import type { Config } from "./types";
import type { Suggestion } from "../components/TagInput";
import { t } from "../i18n";

export function zoneSuggestions(c: Config, withSelf: boolean, withAny = true): Suggestion[] {
  const out: Suggestion[] = [];
  if (withAny) out.push({ value: "any", kind: t("kind.any") });
  for (const z of c.zones) out.push({ value: z.name, kind: t("kind.zone") });
  if (withSelf) out.push({ value: "self", kind: t("kind.self") });
  return out;
}

export function addressSuggestions(c: Config): Suggestion[] {
  return [
    ...c.objects.addresses.map((a) => ({ value: a.name, kind: a.prefixes.slice(0, 2).join(", ") + (a.prefixes.length > 2 ? "…" : "") })),
    ...c.objects.groups.map((g) => ({ value: g.name, kind: t("kind.group") })),
  ];
}

export function serviceSuggestions(c: Config): Suggestion[] {
  return [
    ...c.objects.services.map((s) => ({ value: s.name, kind: `${s.proto}${s.ports ? "/" + s.ports : ""}` })),
    { value: "icmp", kind: t("kind.literal") },
    { value: "icmpv6", kind: t("kind.literal") },
  ];
}

export function ifaceSuggestions(c: Config): Suggestion[] {
  return c.interfaces.map((i) => ({ value: i.name, kind: i.zone }));
}

// Ошибки валидации для пути: rules[3] → все ошибки rules[3].*
export function errorsFor(errors: { path: string; message: string }[], prefix: string): string[] {
  return errors.filter((e) => e.path === prefix || e.path.startsWith(prefix + ".") || e.path.startsWith(prefix + "[")).map((e) => e.message);
}

export function fieldError(errors: { path: string; message: string }[], path: string): string | undefined {
  return errors.find((e) => e.path === path || e.path.startsWith(path + "["))?.message;
}
