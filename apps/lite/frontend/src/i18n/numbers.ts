import type { Locale } from "./messages";

/**
 * Formats a whole number for display.
 *
 * Digits are LATIN (0–9) in both languages — the owner's decision (Q6). `ar` on its own would give
 * Arabic-Indic digits (١٢٣) by default, so the numbering system is pinned in the tag rather than left
 * to the platform.
 *
 * Takes a string or bigint as well as a number, because amounts cross the boundary as strings of
 * integers and a JavaScript Number is exact only up to 2^53.
 */
export function formatInteger(value: number | bigint | string, locale: Locale): string {
  const tag = locale === "ar" ? "ar-u-nu-latn" : "en-u-nu-latn";
  const asNumberOrBig = typeof value === "string" ? BigInt(value) : value;
  return new Intl.NumberFormat(tag, { useGrouping: true, maximumFractionDigits: 0 }).format(asNumberOrBig);
}
