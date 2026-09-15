import { useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { ExportFormat, ExportResult } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { TextField } from "@/ui/Field";

const FORMATS: ExportFormat[] = ["xlsx", "pdf"];

/**
 * *Excel* · *PDF* (L7 §9.2). Go builds the file from what this screen shows and asks where to save it in the system's Save
 * dialog; the screen never holds the file. A saved file is confirmed with its path and *Show in folder*; a closed dialog says
 * nothing. Owner-only exports ask for the PIN through withOwner, as any guarded act does.
 */
export function ExportButtons({ onExport, disabled = false }: { onExport: (format: ExportFormat) => Promise<ExportResult>; disabled?: boolean }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, errorText } = useLocale();
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState<ExportResult | null>(null);
  const [error, setError] = useState<unknown>(null);

  const run = async (format: ExportFormat) => {
    setBusy(true);
    setError(null);
    setSaved(null);
    try {
      const result = await withOwner(() => onExport(format));
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
    <div className="space-y-2" data-testid="export">
      <div role="group" aria-label={t("export.label")} className="flex flex-wrap items-center gap-2">
        {FORMATS.map((format) => (
          <Button key={format} onClick={() => void run(format)} disabled={busy || disabled}>
            {format === "xlsx" ? t("export.xlsx") : t("export.pdf")}
          </Button>
        ))}
      </div>
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
    </div>
  );
}

/**
 * An export over a range of days — the sales history, the debt ledger (L7 §4): from and to, blank for the month to date, which Go
 * resolves in the shop's time zone.
 */
export function RangeExport({ title, onExport }: { title: string; onExport: (from: string, to: string, format: ExportFormat) => Promise<ExportResult> }) {
  const { t } = useLocale();
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  return (
    <section className="space-y-2 rounded-md border border-border bg-surface-raised p-3" aria-label={title}>
      <h3 className="font-semibold">{title}</h3>
      <div className="flex flex-wrap items-end gap-3">
        <div className="w-44">
          <TextField label={t("reports.from")} type="date" value={from} onChange={(e) => setFrom(e.target.value)} dir="ltr" />
        </div>
        <div className="w-44">
          <TextField label={t("reports.to")} type="date" value={to} onChange={(e) => setTo(e.target.value)} dir="ltr" />
        </div>
      </div>
      <p className="text-xs text-text-muted">{t("export.range_hint")}</p>
      <ExportButtons onExport={(format) => onExport(from, to, format)} />
    </section>
  );
}
