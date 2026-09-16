// The catalogs, imported from the SAME files Go embeds (internal/lite/locales).
import arCommon from "@locales/ar/common.json";
import arErrors from "@locales/ar/errors.json";
import enCommon from "@locales/en/common.json";
import enErrors from "@locales/en/errors.json";

export type Locale = "ar" | "en";
export type Direction = "rtl" | "ltr";

/** Arabic first: the primary language of the product and a fresh installation's default. */
export const LOCALES: readonly Locale[] = ["ar", "en"];
export const DEFAULT_LOCALE: Locale = "ar";

/**
 * Keys typed from the ARABIC catalog, because Arabic is primary. The parity gate guarantees English
 * has exactly the same keys, so a key is either valid in both or a compile error.
 */
export type MessageKey = keyof typeof arCommon | keyof typeof arErrors;

const MESSAGES: Record<Locale, Record<string, string>> = {
  ar: { ...arCommon, ...arErrors },
  en: { ...enCommon, ...enErrors },
};

export function isLocale(value: unknown): value is Locale {
  return value === "ar" || value === "en";
}

export function directionOf(locale: Locale): Direction {
  return locale === "ar" ? "rtl" : "ltr";
}

/**
 * The locale the document was SERVED in. Go rewrites index.html's opening tag to the stored language
 * before the webview paints (apps/lite/middleware.go), so this is right from the first frame.
 */
export function documentLocale(): Locale {
  const lang = typeof document === "undefined" ? "" : document.documentElement.lang;
  return isLocale(lang) ? lang : DEFAULT_LOCALE;
}

/**
 * Resolves a key that is only known at runtime — an error code, a platform name.
 *
 * A missing key returns the key itself, as Go's catalog does: "lite.api.xyz" on screen is obviously
 * wrong and greppable, where a blank label is reported as "the text disappeared".
 */
export function translateDynamic(locale: Locale, key: string, params?: Record<string, string>): string {
  const template = MESSAGES[locale][key] ?? key;
  if (!params) return template;
  // `{name}`, the same syntax Go's catalog interpolates.
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    Object.prototype.hasOwnProperty.call(params, name) ? inSentence(locale, params[name]!) : match,
  );
}

const LRI = "\u2066";
const FSI = "\u2068";
const PDI = "\u2069";

/**
 * A value placed into an Arabic sentence, isolated so it cannot reorder the words around it (L8 S1–S5): a figure — anything that
 * starts with a digit, a sign or ≈ — left to right, as `Money` shows it; anything else (a name, a word) in its own first strong
 * direction. The same rule Go applies on paper (D-L7.3, D-L7.i5). English sentences are left as they are: a left-to-right line
 * keeps figures in order, and an isolate there changes nothing a reader sees.
 */
export function inSentence(locale: Locale, value: string): string {
  if (locale !== "ar" || value === "") return value;
  return (/^[\s]*[-+−≈0-9]/.test(value) ? LRI : FSI) + value + PDI;
}

/** Removes the isolates `inSentence` adds — for comparing what a reader sees. */
export function withoutIsolates(text: string): string {
  return text.replace(/[\u2066-\u2069]/g, "");
}

/** Resolves a key known at compile time. A mistyped key does not compile. */
export function translate(locale: Locale, key: MessageKey, params?: Record<string, string>): string {
  return translateDynamic(locale, key, params);
}

/** Exposed for the catalog gates. */
export const CATALOGS = { ar: { common: arCommon, errors: arErrors }, en: { common: enCommon, errors: enErrors } };
