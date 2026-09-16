import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useClient } from "@/api/ClientContext";
import type { About, BackupStatus, ExportResult } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatInteger } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { BackupStatusPanel } from "@/screens/backups/BackupStatusPanel";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { Spinner } from "@/ui/Spinner";

type State = { kind: "loading" } | { kind: "loaded"; about: About } | { kind: "error"; error: unknown };

/**
 * About Mizan Lite (L8 §9.1) — what L0's Status screen was, for whoever supports the shop rather than the counter: the version and
 * database version, the data folder, the backups' state (Q-L7.8), the guides as PDFs to save and print, the support file, and the
 * licences of everything the application is built from.
 */
export function AboutScreen() {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [state, setState] = useState<State>({ kind: "loading" });
  const [backups, setBackups] = useState<BackupStatus | null>(null);
  const [backupsError, setBackupsError] = useState<unknown>(null);
  const [saved, setSaved] = useState<ExportResult | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [withDatabase, setWithDatabase] = useState(false);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setState({ kind: "loading" });
    try {
      setState({ kind: "loaded", about: await client.app.about() });
    } catch (e) {
      setState({ kind: "error", error: e });
    }
  }, [client]);

  useEffect(() => {
    void load();
    client.backups
      .status()
      .then((st) => {
        setBackups(st);
        setBackupsError(null);
      })
      .catch(setBackupsError);
  }, [client, load]);

  const save = async (act: () => Promise<ExportResult>) => {
    setBusy(true);
    setError(null);
    setSaved(null);
    try {
      const result = await withOwner(act);
      if (!result.cancelled) setSaved(result);
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  const show = async (path: string) => {
    try {
      await client.exports.showInFolder(path);
    } catch (e) {
      setError(e);
    }
  };

  return (
    <section className="mx-auto max-w-3xl space-y-6">
      <h2 className="text-xl font-semibold">{t("about.title")}</h2>

      {state.kind === "loading" ? <Spinner label={t("state.loading")} /> : null}
      {state.kind === "error" ? (
        <Alert tone="danger" title={errorText(state.error)}>
          <Button className="mt-2" onClick={() => void load()}>
            {t("action.retry")}
          </Button>
        </Alert>
      ) : null}
      {saved ? (
        <Alert tone="success" title={t("export.saved")}>
          <bdi dir="ltr" className="block break-all font-mono text-xs">
            {saved.path}
          </bdi>
          <Button className="mt-2" onClick={() => void show(saved.path)}>
            {t("export.show_in_folder")}
          </Button>
        </Alert>
      ) : null}
      {error ? <Alert tone="danger" title={errorText(error)} /> : null}

      {state.kind === "loaded" ? (
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 rounded-md border border-border bg-surface-raised p-4 text-sm">
          <dt className="text-text-muted">{t("home.version")}</dt>
          <dd>
            <bdi dir="ltr">{state.about.version}</bdi>
          </dd>
          <dt className="text-text-muted">{t("home.schema_version")}</dt>
          <dd>{formatInteger(state.about.schemaVersion, locale)}</dd>
          <dt className="text-text-muted">{t("home.platform")}</dt>
          <dd>{tDynamic(`platform.${state.about.platform}`)}</dd>
          <dt className="text-text-muted">{t("home.data_dir")}</dt>
          <dd className="space-y-1">
            <bdi dir="ltr" className="block break-all font-mono text-xs">
              {state.about.dataDir}
            </bdi>
            <Button onClick={() => void show(state.about.dataDir)}>{t("export.show_in_folder")}</Button>
          </dd>
        </dl>
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

      {state.kind === "loaded" ? (
        <section className="space-y-2" aria-label={t("about.guides")}>
          <h3 className="font-semibold">{t("about.guides")}</h3>
          <p className="text-sm text-text-muted">{t("about.guides_hint")}</p>
          <div className="flex flex-wrap gap-2">
            {state.about.guides.map((name) => (
              <Button key={name} onClick={() => void save(() => client.app.saveGuide(name))} disabled={busy}>
                {tDynamic(`about.guide.${name}`)}
              </Button>
            ))}
          </div>
        </section>
      ) : null}

      <section className="space-y-2 rounded-md border border-border bg-surface-raised p-4" aria-label={t("about.support")}>
        <h3 className="font-semibold">{t("about.support")}</h3>
        <p className="text-sm text-text-muted">{t("about.support_hint")}</p>
        <Checkbox label={t("about.support_database")} checked={withDatabase} onChange={(e) => setWithDatabase(e.target.checked)} />
        {withDatabase ? <p className="text-xs text-danger">{t("about.support_database_warning")}</p> : null}
        <Button onClick={() => void save(() => client.app.saveSupportFile(withDatabase))} disabled={busy}>
          {t("about.support_save")}
        </Button>
      </section>

      {state.kind === "loaded" ? (
        <details className="rounded-md border border-border bg-surface-raised p-4 text-sm">
          <summary className="cursor-pointer font-semibold">{t("about.licences")}</summary>
          <pre dir="ltr" className="mt-2 max-h-96 overflow-auto whitespace-pre-wrap text-xs" data-testid="notices">
            {state.about.notices}
          </pre>
        </details>
      ) : null}
    </section>
  );
}
