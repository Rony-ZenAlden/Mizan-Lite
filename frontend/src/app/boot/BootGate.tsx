import { useCallback, useEffect, useState, type ReactNode } from "react";
import { bootStatus, onBootEvent, type BootStatus } from "@/lib/wails";
import { translate, type Locale } from "@/i18n/messages";
import { Alert, Spinner } from "@/shared/ui";

/**
 * Decides what the window shows while the object graph is being built.
 *
 * Step 0.11 D2 inverted the boot sequence: the window now opens BEFORE the graph exists, so a
 * long migration has somewhere to show progress and a failed start has somewhere to explain
 * itself. Step 0.10's ordering guarantee is preserved by this component and not by the process
 * order — the application shell does not mount until the backend reports ready, so nothing
 * queries a database that may be mid-restore.
 */
export function BootGate({ locale, children }: { locale: Locale; children: ReactNode }) {
  const [status, setStatus] = useState<BootStatus | null>(null);

  const refresh = useCallback(() => {
    bootStatus()
      .then(setStatus)
      // A failing status probe means the bridge itself is unreachable. Staying on the boot
      // screen is the honest response: we genuinely do not know whether the app is ready.
      .catch(() => undefined);
  }, []);

  useEffect(() => {
    // Poll once on mount AND subscribe. Events are an optimisation, not the source of truth:
    // a boot that finishes before the webview subscribes would never deliver one, and on a
    // warm database that is the common case rather than the edge.
    refresh();
    return onBootEvent(refresh);
  }, [refresh]);

  if (status?.state === "ready") return <>{children}</>;
  if (status?.state === "failed") return <BootFailure locale={locale} status={status} />;
  return <BootScreen locale={locale} status={status} />;
}

/** The progress screen shown while the graph builds. */
export function BootScreen({ locale, status }: { locale: Locale; status: BootStatus | null }) {
  const t = (key: string, params?: Record<string, string>) => translate(locale, key, params);
  const phase = status?.phase ?? "checking";

  return (
    <div className="flex h-full flex-col items-center justify-center gap-4 p-8">
      <Spinner size="lg" />
      <p className="text-lg font-medium text-text">{t("app.title")}</p>
      <p className="text-sm text-text-muted">{t(`boot.phase.${phase}`)}</p>

      {/* Only shown while migrations are actually running — a progress bar reading 0 of 0 on
          every launch would be noise that trains the user to ignore it. */}
      {status && status.total > 0 ? (
        <div className="flex w-64 flex-col gap-1">
          <div
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={status.total}
            aria-valuenow={status.current}
            aria-label={t("boot.updating")}
            className="h-1.5 w-full overflow-hidden rounded-full bg-surface-sunken"
          >
            <div
              className="h-full rounded-full bg-primary transition-[width]"
              style={{ width: `${(status.current / status.total) * 100}%` }}
            />
          </div>
          <p className="text-center text-xs text-text-muted">
            {t("boot.progress", {
              current: String(status.current),
              total: String(status.total),
            })}
          </p>
        </div>
      ) : null}
    </div>
  );
}

/**
 * The failure screen.
 *
 * Rendered from the error's CODE and parameters, never from prose (§22.2) — so the one message
 * a customer is most likely to read, and most likely to read under stress, is in their own
 * language. The backup path is shown verbatim because it is the single piece of information
 * they actually need: their data is safe, and here is where.
 */
export function BootFailure({ locale, status }: { locale: Locale; status: BootStatus }) {
  const t = (key: string, params?: Record<string, string>) => translate(locale, key, params);
  const code = status.error?.messageKey ?? "bootstrap.startup_failed";

  return (
    <div className="flex h-full items-center justify-center p-8">
      <div className="flex w-full max-w-lg flex-col gap-4">
        <Alert tone="danger" title={t("boot.failed.title")}>
          {t(code, status.error?.params)}
        </Alert>

        {status.backupPath ? (
          <div className="flex flex-col gap-1 rounded border border-border bg-surface-raised p-3">
            <p className="text-sm font-medium text-text">{t("boot.failed.backup")}</p>
            {/* Selectable so the user can copy it into a support conversation. */}
            <code className="select-all break-all text-xs text-text-muted">
              {status.backupPath}
            </code>
          </div>
        ) : null}

        <p className="text-sm text-text-muted">{t("boot.failed.next")}</p>
      </div>
    </div>
  );
}
