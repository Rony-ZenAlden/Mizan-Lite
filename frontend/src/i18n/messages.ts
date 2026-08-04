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
 * The locale to use before the backend can be asked.
 *
 * The boot screen renders while the object graph is still being built, so `ui.locale` is not
 * readable yet — the Config binding is guarded until boot completes. The OS language is the
 * best available signal at that moment, and on a genuine first launch it is also the RIGHT
 * one: no preference has been stored, and a Syrian shop opening Mizan for the first time
 * should not be greeted in English while it migrates.
 *
 * Once boot succeeds, PreferencesProvider replaces this with the stored setting.
 */
export function initialLocale(): Locale {
  const tag = typeof navigator !== "undefined" ? navigator.language : "";
  const language = tag.split("-")[0]?.toLowerCase();
  return language && language in MESSAGES ? (language as Locale) : "en";
}

/**
 * The locale currently applied to the document.
 *
 * Read from <html lang> rather than from React context so that components which cannot depend
 * on the provider — the error boundary, which may be catching the provider's own failure —
 * still render translated text.
 */
export function documentLocale(): Locale {
  const lang = typeof document !== "undefined" ? document.documentElement.lang : "";
  return lang in MESSAGES ? (lang as Locale) : "en";
}

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
