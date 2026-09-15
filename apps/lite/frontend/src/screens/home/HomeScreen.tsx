import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useClient } from "@/api/ClientContext";
import type { BackupStatus, Health } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatInteger } from "@/i18n/numbers";
import { BackupStatusPanel } from "@/screens/backups/BackupStatusPanel";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Spinner } from "@/ui/Spinner";

type State = { kind: "loading" } | { kind: "loaded"; health: Health } | { kind: "error"; error: unknown };

/**
 * The application's status: proof the screen reached the Go backend, and what it reached.
 *
 * L0's only screen. It exists because a skeleton whose frontend never calls its backend proves
 * nothing about the bridge — Mizan's 10.17 shipped exactly that. L7 adds the backups' status (Q-L7.8).
 */
export function HomeScreen() {
  const client = useClient();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [state, setState] = useState<State>({ kind: "loading" });
  const [backups, setBackups] = useState<BackupStatus | null>(null);
  const [backupsError, setBackupsError] = useState<unknown>(null);

  const load = useCallback(async () => {
    setState({ kind: "loading" });
    try {
      setState({ kind: "loaded", health: await client.app.health() });
    } catch (error) {
      setState({ kind: "error", error });
    }
  }, [client]);

  useEffect(() => {
    void load();
  }, [load]);

  // The shop's safety (Q-L7.8): the only place a shop notices its backups stopped, so it is read on its own and a failure
  // to read it is said, not hidden behind the health check.
  useEffect(() => {
    client.backups
      .status()
      .then((st) => {
        setBackups(st);
        setBackupsError(null);
      })
      .catch(setBackupsError);
  }, [client]);

  return (
    <section className="mx-auto max-w-2xl space-y-4">
      <h2 className="text-xl font-semibold">{t("home.title")}</h2>

      {state.kind === "loading" ? <Spinner label={t("state.loading")} /> : null}

      {state.kind === "error" ? (
        <Alert tone="danger" title={errorText(state.error)}>
          <Button className="mt-2" onClick={() => void load()}>
            {t("action.retry")}
          </Button>
        </Alert>
      ) : null}

      {state.kind === "loaded" ? (
        <>
          <Alert tone="success" title={t("home.connected")} />
          <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 rounded-md border border-border bg-surface-raised p-4 text-sm">
            <dt className="text-text-muted">{t("home.version")}</dt>
            <dd>
              <bdi dir="ltr">{state.health.version}</bdi>
            </dd>
            <dt className="text-text-muted">{t("home.schema_version")}</dt>
            <dd>{formatInteger(state.health.schemaVersion, locale)}</dd>
            <dt className="text-text-muted">{t("home.platform")}</dt>
            <dd>{tDynamic(`platform.${state.health.platform}`)}</dd>
            <dt className="text-text-muted">{t("home.data_dir")}</dt>
            <dd>
              <bdi dir="ltr" className="break-all font-mono text-xs">
                {state.health.dataDir}
              </bdi>
            </dd>
          </dl>
        </>
      ) : null}

      <section className="space-y-2" aria-label={t("home.backups")}>
        <div className="flex items-center justify-between gap-2">
          <h3 className="font-semibold">{t("home.backups")}</h3>
          <Link to="/backups" className="text-sm text-primary hover:underline">
            {t("home.backups_open")}
          </Link>
        </div>
        {backupsError ? <Alert tone="danger" title={errorText(backupsError)} /> : null}
        {backups ? <BackupStatusPanel status={backups} /> : null}
      </section>
    </section>
  );
}
