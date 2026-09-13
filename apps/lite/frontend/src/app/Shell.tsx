import { useEffect } from "react";
import { NavLink, Route, Routes } from "react-router-dom";
import { useClient } from "@/api/ClientContext";
import { useLocale } from "@/i18n/LocaleProvider";
import { LOCALES, isLocale } from "@/i18n/messages";
import { Alert } from "@/ui/Alert";
import { ROUTES } from "./routes";

/** The application frame: header, navigation, and the routed screen. Mounted only once boot is ready. */
export function Shell() {
  const client = useClient();
  const { t, locale, setLocale, adoptStored, saveError, errorText } = useLocale();

  // The document was served in the language read from the database before launch. If that read fell
  // back to the default (the file was locked, say), the real stored setting wins once it can be read.
  useEffect(() => {
    let cancelled = false;
    client.settings
      .get()
      .then((stored) => {
        if (!cancelled && isLocale(stored.locale)) adoptStored(stored.locale);
      })
      .catch(() => {
        // Keep the language already on screen; a settings read failing is not worth a banner here.
      });
    return () => {
      cancelled = true;
    };
    // adoptStored is stable in behaviour; re-running on its identity would re-read on every switch.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client]);

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center justify-between gap-4 border-b border-border bg-surface-raised px-6 py-3">
        <p className="text-lg font-semibold">{t("app.name")}</p>
        <div role="group" aria-label={t("language.label")} className="flex gap-1">
          {LOCALES.map((option) => (
            <button
              key={option}
              type="button"
              lang={option}
              aria-pressed={option === locale}
              onClick={() => void setLocale(option)}
              className={`rounded-md px-3 py-1 text-sm ${option === locale ? "bg-primary text-primary-fg" : "text-text-muted hover:bg-surface"}`}
            >
              {t(option === "ar" ? "language.ar" : "language.en")}
            </button>
          ))}
        </div>
      </header>

      <div className="flex min-h-0 flex-1">
        <nav aria-label={t("nav.label")} className="w-56 shrink-0 border-e border-border bg-surface-raised p-3">
          <ul className="space-y-1">
            {ROUTES.map((route) => (
              <li key={route.path}>
                <NavLink
                  to={route.path}
                  end
                  className={({ isActive }) =>
                    `block rounded-md px-3 py-2 text-sm ${isActive ? "bg-primary text-primary-fg" : "hover:bg-surface"}`
                  }
                >
                  {t(route.labelKey)}
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>

        <main className="min-w-0 flex-1 overflow-auto p-6">
          {saveError ? (
            <div className="mb-4">
              <Alert tone="danger" title={errorText(saveError)} />
            </div>
          ) : null}
          <Routes>
            {ROUTES.map(({ path, Screen }) => (
              <Route key={path} path={path} element={<Screen />} />
            ))}
          </Routes>
        </main>
      </div>
    </div>
  );
}
