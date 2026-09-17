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

/**
 * Formats a decimal string that Go already formatted — "45000", "3.25" — with grouping in the integer part.
 *
 * No arithmetic: the integer part is grouped through BigInt and the fraction is copied as it is, so the
 * frontend can never round, truncate or float a price (DESIGN D9).
 */
export function formatDecimal(value: string, locale: Locale): string {
  // A shop reading both currencies receives two figures in one string — "150 (15000)" (L10). Each is grouped on its
  // own and the brackets are kept, so a caller that knows nothing about dual readings still draws "150 (15,000)".
  const dual = splitDual(value);
  if (dual) {
    return `${formatDecimal(dual[0], locale)}${DUAL_OPEN}${formatDecimal(dual[1], locale)}${DUAL_CLOSE}`;
  }
  const [whole = "0", fraction] = value.split(".");
  const grouped = formatInteger(whole, locale);
  return fraction === undefined ? grouped : `${grouped}.${fraction}`;
}

/**
 * A dual reading of the currency is the new figure with the old one after it in brackets — "150 (15000)", matching
 * moneyfmt.DualOpen and moneyfmt.DualClose.
 *
 * It used to be divided by an ASCII unit separator, which read as a broken box on every screen that drew the figure
 * without knowing to split it. Brackets need nobody's cooperation: the worst a screen can do with them is show the
 * reading a person wanted anyway.
 */
export const DUAL_OPEN = " (";
export const DUAL_CLOSE = ")";

/**
 * A figure this application produced is digits, at most one point, at most one leading minus, and — once grouped —
 * separators from the locale. Never a bracket, so a bracketed pair is unambiguous.
 */
const DUAL_PATTERN = /^\s*(-?[\d.,\u066b\u066c\u0660-\u0669\u06f0-\u06f9\s]+?)\s*\(\s*(-?[\d.,\u066b\u066c\u0660-\u0669\u06f0-\u06f9\s]+?)\s*\)\s*$/u;

/** splitDual is the new figure and the old one, or null when the value is the single figure most shops read. */
export function splitDual(value: string): [string, string] | null {
  const found = DUAL_PATTERN.exec(value);
  return found ? [found[1]!, found[2]!] : null;
}

/** isDual reports whether a figure carries both readings. */
export function isDual(value: string): boolean {
  return DUAL_PATTERN.test(value);
}

export const CODE_NUMBER_INVALID = "lite.number.invalid";
export const CODE_NUMBER_GROUPING = "lite.number.grouping";

export type Normalised = { ok: true; value: string } | { ok: false; code: string };

const MAX_LENGTH = 32;

/** Maps Arabic-Indic and Extended Arabic-Indic digits to Latin, leaving everything else. */
function latinDigit(ch: string): string {
  const code = ch.codePointAt(0)!;
  if (code >= 0x0660 && code <= 0x0669) return String(code - 0x0660);
  if (code >= 0x06f0 && code <= 0x06f9) return String(code - 0x06f0);
  return ch;
}

/**
 * The TypeScript half of internal/lite/numinput.Normalise — held to the SAME fixture file
 * (internal/lite/numinput/testdata/cases.json), so the two cannot disagree (L1 §6, D-L1.4).
 *
 * It exists so an input can say "not a number" as it is typed. Go is authoritative: whatever this
 * accepts is sent as typed and normalised again on the Go side.
 */
export function normaliseNumber(raw: string): Normalised {
  const s = raw.trim();
  if (s === "" || [...s].length > MAX_LENGTH) return { ok: false, code: CODE_NUMBER_INVALID };

  let out = "";
  let points = 0;
  let digits = 0;
  for (const ch of s) {
    const d = latinDigit(ch);
    if (d >= "0" && d <= "9" && d.length === 1) {
      out += d;
      digits++;
    } else if (ch === "." || ch === "٫") {
      points++;
      if (points > 1) return { ok: false, code: CODE_NUMBER_INVALID };
      out += ".";
    } else if (ch === "," || ch === "٬" || ch === "،") {
      return { ok: false, code: CODE_NUMBER_GROUPING };
    } else {
      return { ok: false, code: CODE_NUMBER_INVALID };
    }
  }
  if (digits === 0) return { ok: false, code: CODE_NUMBER_INVALID };
  if (out.startsWith(".")) out = `0${out}`;
  if (out.endsWith(".")) out = out.slice(0, -1);
  return { ok: true, value: out };
}

export const CODE_QUANTITY_DECIMALS = "lite.stock.quantity_decimals";

/**
 * Why a typed quantity would be refused for a unit of `decimals` input decimals, or null when Go would take it — the
 * TypeScript half of stock/domain.ParseQuantity's checks, held to the fixture's `quantities` cases.
 */
export function quantityProblem(raw: string, decimals: number): string | null {
  const typed = normaliseNumber(raw);
  if (!typed.ok) return typed.code;
  const point = typed.value.indexOf(".");
  const places = point < 0 ? 0 : typed.value.length - point - 1;
  return places > decimals ? CODE_QUANTITY_DECIMALS : null;
}

/** The TypeScript half of numinput.LatinDigits: digits to Latin, surrounding whitespace trimmed. */
export function latinDigits(raw: string): string {
  return [...raw.trim()].map(latinDigit).join("");
}

/** Formats whole seconds as m:ss for a countdown. */
export function formatCountdown(totalSeconds: number): string {
  const safe = Math.max(0, Math.floor(totalSeconds));
  const minutes = Math.floor(safe / 60);
  const seconds = safe % 60;
  return `${minutes}:${seconds.toString().padStart(2, "0")}`;
}
