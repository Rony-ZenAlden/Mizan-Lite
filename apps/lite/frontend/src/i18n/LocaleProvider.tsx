import { createContext, useCallback, useContext, useLayoutEffect, useMemo, useState, type ReactNode } from "react";
import { useClient } from "@/api/ClientContext";
import { BindingError, CODE_UNKNOWN } from "@/api/envelope";
import {
  directionOf,
  documentLocale,
  translate,
  translateDynamic,
  type Direction,
  type Locale,
  type MessageKey,
} from "./messages";

interface LocaleState {
  locale: Locale;
  direction: Direction;
  t: (key: MessageKey, params?: Record<string, string>) => string;
  tDynamic: (key: string, params?: Record<string, string>) => string;
  /** The translated text for any thrown value. */
  errorText: (error: unknown) => string;
  /** Switches language immediately and persists it. Resolves false if it could not be saved. */
  setLocale: (next: Locale) => Promise<boolean>;
  /** Applies a stored locale read from the backend, without writing it back. */
  adoptStored: (stored: Locale) => void;
  saveError: BindingError | null;
}

const LocaleContext = createContext<LocaleState | null>(null);

export function LocaleProvider({ children, initial }: { children: ReactNode; initial?: Locale }) {
  const client = useClient();
  const [locale, setLocaleState] = useState<Locale>(() => initial ?? documentLocale());
  const [saveError, setSaveError] = useState<BindingError | null>(null);
  const direction = directionOf(locale);

  // Layout effect: the attributes change in the same frame as the re-render, so no frame is ever
  // painted with Arabic text in a left-to-right layout or the reverse.
  useLayoutEffect(() => {
    const html = document.documentElement;
    html.lang = locale;
    html.dir = direction;
  }, [locale, direction]);

  const setLocale = useCallback(
    async (next: Locale) => {
      const previous = locale;
      setSaveError(null);
      setLocaleState(next); // immediate: the switch must not wait on the disk
      try {
        const saved = await client.settings.update({ locale: next });
        if (saved.locale === "ar" || saved.locale === "en") setLocaleState(saved.locale);
        return true;
      } catch (error) {
        // Reverted, so the screen never claims a language that will not survive a restart.
        setLocaleState(previous);
        setSaveError(error instanceof BindingError ? error : new BindingError({ code: CODE_UNKNOWN, messageKey: CODE_UNKNOWN }));
        return false;
      }
    },
    [client, locale],
  );

  const value = useMemo<LocaleState>(
    () => ({
      locale,
      direction,
      t: (key, params) => translate(locale, key, params),
      tDynamic: (key, params) => translateDynamic(locale, key, params),
      errorText: (error) =>
        error instanceof BindingError
          ? translateDynamic(locale, error.apiError.messageKey, error.apiError.params)
          : translateDynamic(locale, CODE_UNKNOWN),
      setLocale,
      adoptStored: (stored) => setLocaleState(stored),
      saveError,
    }),
    [locale, direction, setLocale, saveError],
  );

  return <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>;
}

export function useLocale(): LocaleState {
  const state = useContext(LocaleContext);
  if (!state) throw new Error("useLocale called outside LocaleProvider");
  return state;
}
