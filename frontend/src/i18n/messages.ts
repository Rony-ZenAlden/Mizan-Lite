// The message catalogs, imported from the SINGLE source shared with the Go backend.
//
// ARCHITECTURE_v1 §22.1: "locales/{en,ar}/*.json is consumed by both Go (via go:embed) and
// React (imported at build time). One set of files, one key namespace, no drift between
// backend error messages and frontend labels."
//
// These files are deliberately NOT copied into src/. An earlier placeholder hardcoded a
// dictionary here, which meant two catalogs that would have drifted the moment the backend
// rendered its first string. A Go test asserts this file's replacement never reappears.
import arCommon from "../../../locales/ar/common.json";
import arErrors from "../../../locales/ar/errors.json";
import enCommon from "../../../locales/en/common.json";
import enErrors from "../../../locales/en/errors.json";

export type Locale = "en" | "ar";
export type Direction = "ltr" | "rtl";

export const DIRECTION: Record<Locale, Direction> = {
  en: "ltr",
  ar: "rtl",
};

// Keys are typed from the English catalog, which is the one guaranteed to define every key.
// A mistyped key is therefore a compile error rather than a label that silently renders as
// its own key — the same structural guarantee the Go side gets from typed setting handles,
// with no code generation.
export type MessageKey = keyof typeof enCommon | keyof typeof enErrors;

export const MESSAGES: Record<Locale, Record<string, string>> = {
  en: { ...enCommon, ...enErrors },
  ar: { ...arCommon, ...arErrors },
};

/**
 * Resolves a key for a locale, falling back through `en` and finally to the key itself.
 *
 * Returning the key rather than an empty string matches the Go resolver: a visible
 * "app.title" is obviously wrong and greppable, whereas a blank label gets reported as
 * "the text disappeared".
 */
export function translate(
  locale: Locale,
  key: MessageKey | string,
  params?: Record<string, string>,
): string {
  const template = MESSAGES[locale][key] ?? MESSAGES.en[key] ?? key;
  if (!params) return template;
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    Object.prototype.hasOwnProperty.call(params, name) ? params[name]! : match,
  );
}
