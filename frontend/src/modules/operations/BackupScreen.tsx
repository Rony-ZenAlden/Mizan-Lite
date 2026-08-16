import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import {
  backups,
  cancelRestore,
  pendingRestore,
  prepareRestore,
  takeBackup,
  type BackupFile,
} from "@/lib/wails";
import { Alert, Badge, Button, Dialog, EmptyState, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";

/**
 * Backups, and restoring from one.
 *
 * # Restoring is the most dangerous button in the application
 *
 * Everything else here can be undone. A restore replaces the shop's data with an older copy, and
 * the screen's job is to make sure nobody presses it by accident and everybody understands what
 * happens next.
 *
 * Three things do that, and none of them is a warning triangle: the confirmation names the backup
 * and its date, the result says a RESTART is required, and the safety snapshot is named on screen
 * so the way back is visible before it is needed.
 */
export function BackupScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const [confirming, setConfirming] = useState<BackupFile | null>(null);

  const list = useQuery({ queryKey: ["ops", "backups"], queryFn: backups });
  const pending = useQuery({ queryKey: ["ops", "pending-restore"], queryFn: pendingRestore });

  const take = useMutation({
    mutationFn: takeBackup,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["ops", "backups"] }),
  });
  const restore = useMutation({
    mutationFn: prepareRestore,
    onSuccess: () => {
      setConfirming(null);
      void queryClient.invalidateQueries({ queryKey: ["ops"] });
    },
  });
  const cancel = useMutation({
    mutationFn: cancelRestore,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["ops"] }),
  });

  return (
    <section className="flex flex-col gap-6">
      <header className="flex items-center justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h2 className="text-base font-medium text-text">{t("backups.title")}</h2>
          <p className="text-sm text-text-muted">{t("backups.help")}</p>
        </div>
        <Button onClick={() => take.mutate()} disabled={take.isPending}>
          {t("backups.takeNow")}
        </Button>
      </header>

      {take.isError && (
        <Alert tone="danger" title={t("backups.takeFailed")}>{errorText(take.error)}</Alert>
      )}
      {restore.isError && (
        <Alert tone="danger" title={t("backups.restoreFailed")}>{errorText(restore.error)}</Alert>
      )}

      {/*
       * A staged restore is the loudest thing on the screen, because nothing has happened yet and
       * the user must know that closing the application is what applies it.
       */}
      {pending.data?.from && (
        <Alert tone="warning" title={t("backups.restartToFinish")}>
          <div className="flex flex-col gap-2">
            <p>
              {t("backups.restartToFinishHelp", {
                from: pending.data.from,
                safety: pending.data.safetyBackup,
              })}
            </p>
            <div>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => cancel.mutate()}
                disabled={cancel.isPending}
              >
                {t("backups.cancelRestore")}
              </Button>
            </div>
          </div>
        </Alert>
      )}

      {list.isError && (
        <Alert tone="danger" title={t("backups.listFailed")}>{errorText(list.error)}</Alert>
      )}

      <Table<BackupFile>
        caption={t("backups.title")}
        rowKey={(row) => row.name}
        rows={list.data ?? []}
        empty={<EmptyState title={t("backups.none")} />}
        columns={[
          { key: "takenAt", header: t("backups.takenAt"), cell: (row) => row.takenAt },
          {
            key: "reason",
            header: t("backups.reason"),
            cell: (row) => <Badge tone="neutral">{t(`backups.reason.${row.reason}`)}</Badge>,
          },
          {
            key: "size",
            header: t("backups.size"),
            cell: (row) => <span className="tabular-nums">{kilobytes(row.sizeBytes)}</span>,
          },
          {
            key: "restore",
            header: "",
            cell: (row) =>
              row.restorable ? (
                <Button variant="ghost" size="sm" onClick={() => setConfirming(row)}>
                  {t("backups.restore")}
                </Button>
              ) : (
                /*
                 * A backup from a newer build is LISTED and marked, never hidden. Hiding it
                 * would leave a user hunting for a file they know they made — and the reason it
                 * cannot be used is something they can act on: update, then restore.
                 */
                <span className="text-xs text-text-muted">{t("backups.tooNew")}</span>
              ),
          },
        ]}
      />

      <Dialog
        open={confirming !== null}
        onOpenChange={(open) => !open && setConfirming(null)}
        title={t("backups.confirmTitle")}
      >
        <div className="flex flex-col gap-4">
          {/*
           * The confirmation NAMES the backup and its date. "Are you sure?" is a question nobody
           * reads; "restore the backup taken on 16 August at 02:00, replacing everything since"
           * is one they answer.
           */}
          <p className="text-sm text-text">
            {t("backups.confirmHelp", {
              takenAt: confirming?.takenAt ?? "",
            })}
          </p>
          <p className="text-sm text-text-muted">{t("backups.confirmSafety")}</p>

          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setConfirming(null)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="danger"
              onClick={() => confirming && restore.mutate(confirming.name)}
              disabled={restore.isPending}
            >
              {t("backups.confirmRestore")}
            </Button>
          </div>
        </div>
      </Dialog>
    </section>
  );
}

/** Rendered without float arithmetic, because a size is a count of bytes. */
function kilobytes(bytes: number): string {
  return `${Math.round(bytes / 1024).toLocaleString()} KB`;
}
