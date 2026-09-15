import { useCallback, useEffect, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { BackupInfo, BackupStatus, Loss, RestoreResult } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatInteger } from "@/i18n/numbers";
import { formatAge, formatDateTime } from "@/i18n/time";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { Spinner } from "@/ui/Spinner";
import { BackupStatusPanel } from "./BackupStatusPanel";

type Notice = { kind: "taken"; backup: BackupInfo } | { kind: "copied"; path: string } | { kind: "imported"; backup: BackupInfo } | null;

/**
 * Backups (L7 §6.3): every backup, newest first, and the outside folder they are copied to (Q-L7.8). Anyone may back up now;
 * restoring, restoring from a file, saving a copy and choosing the folder are the owner's. A restore first says what it removes
 * — the sales and other records since the backup was taken (§6.4) — then the PIN, a safety snapshot, and a restart.
 */
export function BackupsScreen() {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, errorText, locale } = useLocale();
  const [list, setList] = useState<BackupInfo[] | null>(null);
  const [status, setStatus] = useState<BackupStatus | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);
  const [restoring, setRestoring] = useState<Loss | null>(null);

  const load = useCallback(async () => {
    try {
      const [found, st] = await Promise.all([client.backups.list(), client.backups.status()]);
      setList(found);
      setStatus(st);
      setError(null);
    } catch (e) {
      setError(e);
    }
  }, [client]);

  useEffect(() => {
    void load();
  }, [load]);

  const act = async (run: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await run();
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  const takeNow = () =>
    act(async () => {
      const backup = await client.backups.takeNow();
      setNotice({ kind: "taken", backup });
      await load();
    });

  const chooseFolder = (clear: boolean) =>
    act(async () => {
      setStatus(await withOwner(() => client.backups.setOutsideFolder(clear)));
      await load();
    });

  const saveCopy = (name: string) =>
    act(async () => {
      const result = await withOwner(() => client.backups.saveCopy(name));
      if (!result.cancelled) setNotice({ kind: "copied", path: result.path });
    });

  const askRestore = (name: string) =>
    act(async () => {
      setRestoring(await withOwner(() => client.backups.lossPreview(name)));
    });

  const fromFile = () =>
    act(async () => {
      const backup = await withOwner(() => client.backups.restoreFromFile());
      if (backup.name === "") return; // the dialog was closed
      setNotice({ kind: "imported", backup });
      await load();
      setRestoring(await withOwner(() => client.backups.lossPreview(backup.name)));
    });

  return (
    <section className="mx-auto max-w-4xl space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <h2 className="text-xl font-semibold">{t("backups.title")}</h2>
        <div className="flex flex-wrap gap-2">
          <Button variant="primary" onClick={() => void takeNow()} disabled={busy}>
            {t("backups.take_now")}
          </Button>
          <Button onClick={() => void fromFile()} disabled={busy}>
            {t("backups.from_file")}
          </Button>
        </div>
      </header>

      {error ? <Alert tone="danger" title={errorText(error)} /> : null}
      {notice?.kind === "taken" ? (
        <Alert tone="success" title={notice.backup.outside ? t("backups.taken_outside") : t("backups.taken")} />
      ) : null}
      {notice?.kind === "imported" ? <Alert tone="success" title={t("backups.imported")} /> : null}
      {notice?.kind === "copied" ? (
        <Alert tone="success" title={t("backups.copied")}>
          <bdi dir="ltr" className="break-all font-mono text-xs">
            {notice.path}
          </bdi>
        </Alert>
      ) : null}

      {status ? <BackupStatusPanel status={status} /> : null}

      <div className="space-y-2 rounded-md border border-border bg-surface-raised p-4">
        <h3 className="font-semibold">{t("backups.outside")}</h3>
        {status?.folder ? (
          <p className="text-sm">
            <bdi dir="ltr" className="break-all font-mono text-xs">
              {status.folder}
            </bdi>
          </p>
        ) : (
          <p className="text-sm text-text-muted">{t("backups.outside_none")}</p>
        )}
        <p className="text-xs text-text-muted">{t("backups.outside_hint")}</p>
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => void chooseFolder(false)} disabled={busy}>
            {status?.folder ? t("backups.outside_change") : t("backups.outside_choose")}
          </Button>
          {status?.folder ? (
            <Button onClick={() => void chooseFolder(true)} disabled={busy}>
              {t("backups.outside_clear")}
            </Button>
          ) : null}
        </div>
      </div>

      {list === null && !error ? <Spinner label={t("state.loading")} /> : null}
      {list && list.length === 0 ? <p className="text-text-muted">{t("backups.empty")}</p> : null}
      {list && list.length > 0 ? (
        <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
          <table className="w-full text-sm" data-testid="backups">
            <thead className="bg-surface text-text-muted">
              <tr>
                <th className="p-2 text-start">{t("backups.col.when")}</th>
                <th className="p-2 text-start">{t("backups.col.reason")}</th>
                <th className="p-2 text-start">{t("backups.col.size")}</th>
                <th className="p-2 text-start">{t("backups.col.outside")}</th>
                <th className="p-2 text-start">{t("backups.col.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {list.map((b) => (
                <tr key={b.name} className="border-t border-border" data-testid="backup-row">
                  <td className="p-2">
                    <bdi dir="ltr">{formatDateTime(b.takenAt, locale)}</bdi>
                    <span className="block text-xs text-text-muted">{formatAge(b.ageSeconds, locale, t("age.just_now"))}</span>
                  </td>
                  <td className="p-2">
                    <ReasonLabel reason={b.reason} />
                  </td>
                  <td className="p-2">
                    <bdi dir="ltr">{t("backups.size_mb", { size: megabytes(b.sizeBytes, locale) })}</bdi>
                  </td>
                  <td className="p-2">{b.outside ? t("backups.outside_yes") : t("backups.outside_no")}</td>
                  <td className="p-2">
                    <div className="flex flex-wrap gap-2">
                      <Button onClick={() => void askRestore(b.name)} disabled={busy}>
                        {t("backups.restore")}
                      </Button>
                      <Button onClick={() => void saveCopy(b.name)} disabled={busy}>
                        {t("backups.save_copy")}
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {restoring ? <RestoreDialog loss={restoring} onClose={() => setRestoring(null)} /> : null}
    </section>
  );
}

/** Why a backup was taken, in words. */
export function ReasonLabel({ reason }: { reason: string }) {
  const { tDynamic } = useLocale();
  return <>{tDynamic(`backups.reason.${reason}`)}</>;
}

/** A size Go sent in bytes, as megabytes with one decimal. */
function megabytes(bytes: string, locale: "ar" | "en"): string {
  const mb = Math.max(0.1, Math.round((Number(bytes) / 1_048_576) * 10) / 10);
  return new Intl.NumberFormat(locale === "ar" ? "ar-u-nu-latn" : "en-u-nu-latn", { minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(mb);
}

/**
 * The restore's one confirmation (§6.4): the backup's date and what restoring it removes, counted by Go from the live database.
 * The PIN was asked for to read this; Restore asks again only if owner mode ended meanwhile.
 */
function RestoreDialog({ loss, onClose }: { loss: Loss; onClose: () => void }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, errorText, locale } = useLocale();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [result, setResult] = useState<RestoreResult | null>(null);

  const count = (n: number) => formatInteger(n, locale);
  const nothingLost = loss.sales + loss.voids + loss.debtEntries + loss.cashEntries + loss.stockMovements === 0;

  const restore = async () => {
    setBusy(true);
    setError(null);
    try {
      setResult(await withOwner(() => client.backups.restore(loss.backup.name)));
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t("restore.title")} onClose={onClose}>
      {result ? (
        <Alert tone="success" title={result.restarting ? t("restore.restarting") : t("restore.staged")} />
      ) : (
        <div className="space-y-3">
          <p data-testid="restore-when">
            {t("restore.from", { when: formatDateTime(loss.backup.takenAt, locale) })} · <ReasonLabel reason={loss.backup.reason} />
          </p>
          {nothingLost ? (
            <p data-testid="restore-loss">{t("restore.nothing_lost")}</p>
          ) : (
            <div data-testid="restore-loss" className="rounded-md border border-danger/40 bg-danger-subtle p-3 text-sm text-danger">
              <p className="font-semibold">{t("restore.lost")}</p>
              <ul className="list-disc ps-5">
                {loss.sales > 0 ? <li>{t("restore.lost_sales", { count: count(loss.sales) })}</li> : null}
                {loss.voids > 0 ? <li>{t("restore.lost_voids", { count: count(loss.voids) })}</li> : null}
                {loss.debtEntries > 0 ? <li>{t("restore.lost_debts", { count: count(loss.debtEntries) })}</li> : null}
                {loss.cashEntries > 0 ? <li>{t("restore.lost_cash", { count: count(loss.cashEntries) })}</li> : null}
                {loss.stockMovements > 0 ? <li>{t("restore.lost_stock", { count: count(loss.stockMovements) })}</li> : null}
              </ul>
            </div>
          )}
          <p className="text-sm text-text-muted">{t("restore.safety")}</p>
          {error ? <Alert tone="danger" title={errorText(error)} /> : null}
          <div className="flex justify-end gap-2">
            <Button onClick={onClose}>{t("action.cancel")}</Button>
            <Button variant="primary" onClick={() => void restore()} disabled={busy}>
              {t("restore.confirm")}
            </Button>
          </div>
        </div>
      )}
    </Dialog>
  );
}
