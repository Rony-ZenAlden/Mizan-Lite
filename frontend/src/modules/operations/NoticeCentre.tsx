import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { dismissNotice, notices, type Notice } from "@/lib/wails";
import { Alert, Badge, Button, EmptyState, PageHeader } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";

/**
 * What the application has noticed about itself.
 *
 * # Why there is no "mark all as read"
 *
 * These are not messages. Each one is a CONDITION that is true right now, and it disappears when
 * the condition does — so a button that cleared them all would be a button that silences a real
 * problem and calls it tidying up.
 *
 * Dismissing one is deliberate and per-notice, and the count of hidden ones is always shown, so
 * there is a way back.
 */
export function NoticeCentre() {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const centre = useQuery({ queryKey: ["ops", "notices"], queryFn: notices });

  const dismiss = useMutation({
    mutationFn: dismissNotice,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["ops", "notices"] }),
  });

  if (centre.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (centre.isError) {
    return <Alert tone="danger" title={t("notices.failed")}>{errorText(centre.error)}</Alert>;
  }

  return (
    <section className="flex flex-col gap-4">
      <PageHeader
        title={t("notices.title")}
        actions={
          <>
            {centre.data.dismissed > 0 && (
          <span className="text-xs text-text-muted">
            {t("notices.hidden", { count: String(centre.data.dismissed) })}
          </span>
        )}
          </>
        }
      />

      {/*
       * A rule that could not run is SHOWN, because silence reads as "nothing is wrong". A user
       * seeing an empty centre while the ledger check is broken would conclude the books are
       * fine.
       */}
      {centre.data.failed.length > 0 && (
        <Alert tone="warning" title={t("notices.someRulesFailed")}>
          {t("notices.someRulesFailedHelp", { rules: centre.data.failed.join(", ") })}
        </Alert>
      )}

      {centre.data.notices.length === 0 ? (
        <EmptyState title={t("notices.none")} />
      ) : (
        <ul className="flex flex-col gap-2">
          {centre.data.notices.map((notice) => (
            <NoticeRow
              key={notice.key}
              notice={notice}
              onDismiss={() => dismiss.mutate(notice.key)}
              dismissing={dismiss.isPending}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

function NoticeRow({
  notice,
  onDismiss,
  dismissing,
}: {
  notice: Notice;
  onDismiss: () => void;
  dismissing: boolean;
}) {
  const { t } = useTranslation();

  return (
    <li className="flex items-start justify-between gap-4 card p-3">
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          <Badge tone={notice.severity === "danger" ? "danger" : "warning"}>
            {t(`notices.severity.${notice.severity}`)}
          </Badge>
          <span className="text-xs text-text-muted">{notice.rule}</span>
        </div>
        {/*
         * Translated on the FRONTEND from a key and parameters. The backend has no locale, and a
         * rendered sentence crossing the boundary would put i18n in two places.
         */}
        <p className="text-sm text-text">{t(notice.messageKey, notice.params)}</p>
      </div>

      <Button variant="ghost" size="sm" onClick={onDismiss} disabled={dismissing}>
        {t("notices.dismiss")}
      </Button>
    </li>
  );
}
