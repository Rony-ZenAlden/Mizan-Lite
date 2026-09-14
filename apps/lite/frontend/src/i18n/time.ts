import type { Locale } from "./messages";

const tag = (locale: Locale) => (locale === "ar" ? "ar-u-nu-latn" : "en-u-nu-latn");

/**
 * Words an age Go measured, in whole seconds: "3 hours ago", "منذ 3 ساعات". The plural forms are the platform's
 * (Intl.RelativeTimeFormat), never hand-written; digits are Latin (Q6). Under a minute is "just now", which the caller
 * supplies, because RelativeTimeFormat has no such unit.
 */
export function formatAge(seconds: number, locale: Locale, justNow: string): string {
  const s = Math.max(0, Math.floor(seconds));
  if (s < 60) return justNow;
  const format = new Intl.RelativeTimeFormat(tag(locale), { numeric: "auto" });
  if (s < 3600) return format.format(-Math.floor(s / 60), "minute");
  if (s < 86_400) return format.format(-Math.floor(s / 3600), "hour");
  return format.format(-Math.floor(s / 86_400), "day");
}

/** A UTC timestamp Go sent, shown in the machine's own time zone: "14 Sept 2026, 09:00". */
export function formatDateTime(iso: string, locale: Locale): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;
  return new Intl.DateTimeFormat(tag(locale), { dateStyle: "medium", timeStyle: "short" }).format(at);
}
