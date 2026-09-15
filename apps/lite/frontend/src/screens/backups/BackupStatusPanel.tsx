import type { BackupStatus } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatAge, formatDateTime } from "@/i18n/time";
import { Alert } from "@/ui/Alert";

/**
 * The shop's safety at a glance (L7 §6.2, Q-L7.8), on Home and the Backups screen: the last backup and the last outside copy with
 * their ages — Go's clock — a warning when the outside copy is more than two days old or the last copy failed, and the restore
 * applied at this start.
 */
export function BackupStatusPanel({ status }: { status: BackupStatus }) {
  const { t, tDynamic, locale } = useLocale();
  const age = (seconds: number) => formatAge(seconds, locale, t("age.just_now"));

  return (
    <div className="space-y-2" data-testid="backup-status">
      {status.restored ? (
        <Alert tone="success" title={t("backups.restored")}>
          <bdi dir="ltr" className="block break-all font-mono text-xs">
            {status.restored.from}
          </bdi>
          <p>{t("backups.restored_safety")}</p>
        </Alert>
      ) : null}
      {status.folder && (status.outsideStale || status.outsideFailed) ? (
        <Alert
          tone="danger"
          title={
            !status.outsideStale
              ? t("backups.outside_failed")
              : status.lastOutside
                ? t("backups.outside_stale", { age: age(status.lastOutside.ageSeconds) })
                : t("backups.outside_never")
          }
        >
          {status.outsideFailed ? <p>{tDynamic(status.outsideFailed)}</p> : null}
          <p>{t("backups.outside_stale_hint")}</p>
        </Alert>
      ) : null}
      <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 rounded-md border border-border bg-surface-raised p-4 text-sm">
        <dt className="text-text-muted">{t("backups.last")}</dt>
        <dd>
          {status.last ? (
            <>
              {age(status.last.ageSeconds)} · <bdi dir="ltr">{formatDateTime(status.last.takenAt, locale)}</bdi> · {tDynamic(`backups.reason.${status.last.reason}`)}
            </>
          ) : (
            t("backups.none")
          )}
        </dd>
        <dt className="text-text-muted">{t("backups.last_outside")}</dt>
        <dd>
          {!status.folder ? (
            <span className="text-text-muted">{t("backups.outside_none")}</span>
          ) : status.lastOutside ? (
            <>
              {age(status.lastOutside.ageSeconds)} · <bdi dir="ltr">{formatDateTime(status.lastOutside.takenAt, locale)}</bdi>
            </>
          ) : (
            t("backups.none")
          )}
        </dd>
      </dl>
    </div>
  );
}
