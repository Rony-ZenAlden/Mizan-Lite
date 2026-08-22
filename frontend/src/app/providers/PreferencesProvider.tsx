import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { DIRECTION, translate, type Locale } from "@/i18n/messages";
import {
  isBindingError,
  preferences as loadPreferences,
  setLocale as saveLocale,
  setTheme as saveTheme,
  type Preferences,
  type ThemePreference,
} from "@/lib/wails";
import { useToast } from "@/shared/ui";

/**
 * Locale and theme, held as SERVER state.
 *
 * Step 0.11 §1.4/D4: the shell used to keep locale in component state, which meant the
 * frontend's active language and the backend's `ui.locale` setting were two unrelated facts.
 * They had not disagreed only because nothing on the backend rendered text yet — the first
 * printed document would have exposed it as "the invoice printed in the wrong language".
 *
 * Locale and theme live in ONE provider because they come from one binding call. Splitting
 * them would mean two round trips for two fields of the same row.
 */
interface PreferencesApi {
  locale: Locale;
  theme: ThemePreference;
  /** The theme actually applied, after resolving "system". */
  resolvedTheme: "light" | "dark";
  availableLocales: Locale[];
  setLocale: (locale: Locale) => Promise<void>;
  setTheme: (theme: ThemePreference) => Promise<void>;
  /** Translates a key in the active locale. */
  t: (key: string, params?: Record<string, string>) => string;
}

const PreferencesContext = createContext<PreferencesApi | null>(null);

export function usePreferences(): PreferencesApi {
  const api = useContext(PreferencesContext);
  if (!api) throw new Error("usePreferences must be used inside <PreferencesProvider>");
  return api;
}

/** The translation hook every component uses. */
export function useTranslation() {
  const { t, locale } = usePreferences();
  return { t, locale };
}

/** Resolves "system" against the OS preference. */
function systemTheme(): "light" | "dark" {
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

const FALLBACK: Preferences = { locale: "en", theme: "system", availableLocales: ["en"] };

export function PreferencesProvider({ children }: { children: ReactNode }) {
  const [prefs, setPrefs] = useState<Preferences>(FALLBACK);
  const [osTheme, setOsTheme] = useState<"light" | "dark">(systemTheme);
  // Whether the stored preference has arrived. It gates the theme TRANSITION, not the theme.
  const [settled, setSettled] = useState(false);
  const toast = useToast();

  useEffect(() => {
    loadPreferences()
      .then(setPrefs)
      // A preferences read that fails leaves the defaults in place rather than blocking the
      // shell: an unreadable language preference must never stop a shop from opening.
      .catch(() => setPrefs(FALLBACK))
      // Either way startup is over. A failed read still settles, or a shop whose preferences
      // will not load would never get an animated theme change again.
      .finally(() => setSettled(true));
  }, []);

  // Track the OS theme so "system" stays live rather than being sampled once at startup.
  useEffect(() => {
    const query = window.matchMedia?.("(prefers-color-scheme: dark)");
    if (!query) return;
    const listener = (event: MediaQueryListEvent) => setOsTheme(event.matches ? "dark" : "light");
    query.addEventListener("change", listener);
    return () => query.removeEventListener("change", listener);
  }, []);

  const locale = prefs.locale as Locale;
  const resolvedTheme = prefs.theme === "system" ? osTheme : prefs.theme;

  // Drive the document. This is what makes a language change take effect with no reload:
  // <html lang/dir> change and React re-renders (§22.3).
  useEffect(() => {
    const root = document.documentElement;
    root.lang = locale;
    root.dir = DIRECTION[locale] ?? "ltr";
  }, [locale]);

  useEffect(() => {
    document.documentElement.dataset.theme = resolvedTheme;
  }, [resolvedTheme]);

  /*
   * Transitions are enabled only once the stored preference has arrived.
   *
   * Before that there are two theme applications that are STARTUP rather than choice: the first
   * paint resolving the OS preference, and the stored preference correcting it a moment later.
   * Animating either fades the whole application in from the wrong colours on every launch.
   *
   * So the CSS gates on this attribute, and it goes on exactly once — after which every change
   * is one a person asked for, and worth easing.
   */
  useEffect(() => {
    if (!settled) return;
    document.documentElement.dataset.themeTransitions = "";
  }, [settled]);

  const t = useCallback(
    (key: string, params?: Record<string, string>) => translate(locale, key, params),
    [locale],
  );

  // A rejected write must NOT leave the UI showing a value the backend refused — otherwise the
  // user believes they switched language and every printed document disagrees. The stored
  // preferences are the single source of truth, so we simply do not apply anything the write
  // did not return.
  const commit = useCallback(
    async (write: () => Promise<Preferences>) => {
      try {
        setPrefs(await write());
      } catch (error) {
        toast.show({
          title: translate(locale, isBindingError(error) ? error.messageKey : "app.unknown_error"),
          tone: "danger",
        });
      }
    },
    [locale, toast],
  );

  const api = useMemo<PreferencesApi>(
    () => ({
      locale,
      theme: prefs.theme,
      resolvedTheme,
      availableLocales: prefs.availableLocales as Locale[],
      setLocale: (next) => commit(() => saveLocale(next)),
      setTheme: (next) => commit(() => saveTheme(next)),
      t,
    }),
    [locale, prefs.theme, prefs.availableLocales, resolvedTheme, commit, t],
  );

  return <PreferencesContext.Provider value={api}>{children}</PreferencesContext.Provider>;
}
