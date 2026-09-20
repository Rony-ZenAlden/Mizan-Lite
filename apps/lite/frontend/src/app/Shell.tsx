import { useEffect, useState } from "react";
import { Link, NavLink, Route, Routes } from "react-router-dom";
import { useClient } from "@/api/ClientContext";
import type { BackupStatus } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { isLocale } from "@/i18n/messages";
import { formatCountdown } from "@/i18n/numbers";
import { formatAge } from "@/i18n/time";
import { useRate } from "@/rates/RateProvider";
import { useOwner } from "@/owner/OwnerProvider";
import { Button } from "@/ui/Button";
import { Alert } from "@/ui/Alert";
import { ratePair } from "@/i18n/figures";
import { ROUTES } from "./routes";

/** The application frame: header, navigation, and the routed screen. Mounted only once boot is ready. */
export function Shell() {
  const client = useClient();
  const { t, tDynamic, locale, adoptStored, saveError, errorText } = useLocale();
  const { status, lock } = useOwner();
  const { rate } = useRate();
  const [shopName, setShopName] = useState("");
  const [safety, setSafety] = useState<BackupStatus | null>(null);

  // Proof, in the log, that the packaged webview reached its own backend (L0) — the smoke test waits for this line — and the
  // backups' state, warned on every screen when it needs attention, since the counter no longer lands on a status page (L8 A-L8.3).
  useEffect(() => {
    let cancelled = false;
    client.app.health().catch(() => {});
    client.backups
      .status()
      .then((st) => {
        if (!cancelled) setSafety(st);
      })
      .catch(() => {
        // The Backups and About screens say why; a failed read here is not worth a banner of its own.
      });
    return () => {
      cancelled = true;
    };
  }, [client]);
  const backupWarning = needsAttention(safety);

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
                {t("header.rate", ratePair(rate.rate, rate.localCurrency, locale, tDynamic))}
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
          {backupWarning ? (
            <div className="mb-4" data-testid="backup-warning">
              <Alert tone="danger" title={t(backupWarning)}>
                <Link to="/backups" className="underline">
                  {t("home.backups_open")}
                </Link>
              </Alert>
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

/** A day and a bit: a daily backup missed once is worth saying. */
const LAST_BACKUP_TOO_OLD_SECONDS = 36 * 3600;

/**
 * What, if anything, the shop must be told about its backups on every screen.
 *
 * # Why the outside folder is no longer among them (the owner's request, 2026-09-20)
 *
 * The banner used to say "the last copy in the outside folder is more than two days old — is the USB drive plugged
 * in?" on every screen, the till included. A shop that unplugs its USB drive during the day — which is most days —
 * got a red banner across the counter while serving customers, about something the cashier cannot act on and the
 * owner already knows. A warning that appears when nothing is wrong teaches people to ignore warnings, and the two
 * below are ones they should not ignore.
 *
 * What is left is about the shop's OWN backups: there is no backup at all, or the last one is a day and a half old.
 * Those say the shop's data is unprotected right now, which is not the same as a drive being out of its socket.
 *
 * The outside folder is still copied to, still checked, and still reported in full — with its staleness and any
 * failure — on the Backups screen and in About. It simply no longer interrupts the counter.
 */
export function needsAttention(st: BackupStatus | null): "backups.warning_none" | "backups.warning_old" | null {
  if (!st) return null;
  if (!st.last) return "backups.warning_none";
  if (st.last.ageSeconds > LAST_BACKUP_TOO_OLD_SECONDS) return "backups.warning_old";
  return null;
}
