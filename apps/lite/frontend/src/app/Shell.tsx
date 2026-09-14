import { useEffect, useState } from "react";
import { Link, NavLink, Route, Routes } from "react-router-dom";
import { useClient } from "@/api/ClientContext";
import { useLocale } from "@/i18n/LocaleProvider";
import { LOCALES, isLocale } from "@/i18n/messages";
import { formatCountdown, formatDecimal } from "@/i18n/numbers";
import { formatAge } from "@/i18n/time";
import { useRate } from "@/rates/RateProvider";
import { useOwner } from "@/owner/OwnerProvider";
import { Button } from "@/ui/Button";
import { Alert } from "@/ui/Alert";
import { ROUTES } from "./routes";

/** The application frame: header, navigation, and the routed screen. Mounted only once boot is ready. */
export function Shell() {
  const client = useClient();
  const { t, tDynamic, locale, setLocale, adoptStored, saveError, errorText } = useLocale();
  const { status, lock } = useOwner();
  const { rate } = useRate();
  const [shopName, setShopName] = useState("");

  // The document was served in the language read from the database before launch. If that read fell
  // back to the default (the file was locked, say), the real stored setting wins once it can be read.
  useEffect(() => {
    let cancelled = false;
    client.settings
      .get()
      .then((stored) => {
        if (cancelled) return;
        if (isLocale(stored.locale)) adoptStored(stored.locale);
        setShopName(stored.shopName);
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
        <div>
          <p className="text-lg font-semibold">{shopName || t("app.name")}</p>
          {shopName ? <p className="text-xs text-text-muted">{t("app.name")}</p> : null}
        </div>
        {rate ? (
          <Link
            to="/rates"
            data-testid="header-rate"
            className={`rounded-md px-3 py-1 text-sm ${!rate.set || rate.stale ? "bg-danger-subtle text-danger" : "hover:bg-surface"}`}
          >
            {rate.set ? (
              <>
                <bdi dir="ltr">{t("header.rate", { rate: formatDecimal(rate.rate, locale), currency: tDynamic(`currency.${rate.localCurrency}`) })}</bdi>
                {" · "}
                {rate.stale ? t("header.rate_stale") : formatAge(rate.ageSeconds, locale, t("age.just_now"))}
              </>
            ) : (
              t("header.no_rate")
            )}
          </Link>
        ) : null}
        {status.elevatedSeconds > 0 ? (
          <div role="status" className="flex items-center gap-2 rounded-md bg-primary/10 px-3 py-1">
            <span className="text-sm font-medium text-primary">
              {t("header.owner_mode", { time: formatCountdown(status.elevatedSeconds) })}
            </span>
            <Button onClick={() => void lock()}>{t("header.lock")}</Button>
          </div>
        ) : null}
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
