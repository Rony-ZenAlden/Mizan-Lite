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

const two = (n: number) => String(n).padStart(2, "0");

/**
 * A UTC timestamp Go sent, in the machine's own time zone, as the shop reads a date: "15/09/2026 15:15" (Q-L8.6). Digits and
 * separators only — no month names, no ص/م, no direction marks — so the same text reads the same inside Arabic and English and
 * cannot be reordered by the bidirectional algorithm (L8 S1). The same format as paper (Go's `words.when`).
 */
export function formatDateTime(iso: string): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;
  return `${two(at.getDate())}/${two(at.getMonth() + 1)}/${at.getFullYear()} ${two(at.getHours())}:${two(at.getMinutes())}`;
}

/** A business date Go sent ("2026-09-15") as "15/09/2026"; a month ("2026-09") as "09/2026"; anything else as it came. */
export function formatDate(date: string): string {
  const day = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  if (day) return `${day[3]}/${day[2]}/${day[1]}`;
  const month = /^(\d{4})-(\d{2})$/.exec(date);
  if (month) return `${month[2]}/${month[1]}`;
  return date;
}
