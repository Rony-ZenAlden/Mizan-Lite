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
  const [whole = "0", fraction] = value.split(".");
  const grouped = formatInteger(whole, locale);
  return fraction === undefined ? grouped : `${grouped}.${fraction}`;
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
