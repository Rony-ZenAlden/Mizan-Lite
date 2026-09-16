import { useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { ImportPreview, ImportResult } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";

/** How many rows of a workbook the preview lists; the count above it is of every row. */
const PREVIEW_ROWS = 50;

/**
 * Loading a shop's products and opening stock from Excel (L8 Q-L8.9): save the template, fill it in, choose it. Go reads the
 * workbook and runs the whole import inside a transaction it rolls back, so the preview's rows and problems are exactly what
 * loading will do; loading is all or nothing, with the owner's PIN.
 */
export function ImportDialog({ onClose, onImported }: { onClose: () => void; onImported: (result: ImportResult) => void }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [templatePath, setTemplatePath] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const run = async (act: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await act();
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  const saveTemplate = () =>
    run(async () => {
      const saved = await client.catalog.importTemplate();
      if (!saved.cancelled) setTemplatePath(saved.path);
    });

  const choose = () =>
    run(async () => {
      const next = await client.catalog.importPreview();
      if (!next.cancelled) setPreview({ ...next, rows: next.rows ?? [], problems: next.problems ?? [] });
    });

  const apply = () =>
    run(async () => {
      if (!preview) return;
      onImported(await withOwner(() => client.catalog.importApply(preview.path, preview.digest)));
    });

  const withStock = preview ? preview.rows.filter((r) => r.quantity !== "").length : 0;
  const canApply = preview !== null && preview.problems.length === 0 && preview.rows.length > 0 && !busy;

  return (
    <Dialog title={t("import.title")} onClose={onClose} wide>
      <ol className="list-decimal space-y-2 ps-5 text-sm">
        <li>
          {t("import.step_template")}{" "}
          <Button onClick={() => void saveTemplate()} disabled={busy}>
            {t("import.template")}
          </Button>
        </li>
        <li>{t("import.step_fill")}</li>
        <li>
          {t("import.step_choose")}{" "}
          <Button onClick={() => void choose()} disabled={busy}>
            {t("import.choose")}
          </Button>
        </li>
      </ol>
      {templatePath ? (
        <Alert tone="success" title={t("export.saved")}>
          <bdi dir="ltr" className="break-all font-mono text-xs">
            {templatePath}
          </bdi>
        </Alert>
      ) : null}
      {error ? <Alert tone="danger" title={errorText(error)} /> : null}

      {preview ? (
        <div className="space-y-3" data-testid="import-preview">
          <p className="text-sm">
            <bdi dir="ltr" className="break-all font-mono text-xs">
              {preview.path}
            </bdi>
          </p>
          {preview.problems.length > 0 ? (
            <div role="alert" className="space-y-2 rounded-md border border-danger/40 bg-danger-subtle p-3 text-sm text-danger">
              <p className="font-semibold">{t("import.problems", { count: String(preview.problems.length) })}</p>
              <ul className="space-y-1" data-testid="import-problems">
                {preview.problems.map((p, i) => (
                  <li key={`${p.row}-${i}`}>
                    {t("import.problem_at", { row: String(p.row), column: p.column ? tDynamic(`import.col.${p.column}`) : "—" })}{" "}
                    {tDynamic(p.code, p.params)}
                  </li>
                ))}
              </ul>
            </div>
          ) : (
            <Alert tone="success" title={t("import.ready", { count: String(preview.rows.length), stock: String(withStock) })} />
          )}
          {preview.rows.length > 0 ? (
            <div className="max-h-72 overflow-auto rounded-md border border-border">
              <table className="w-full text-sm" data-testid="import-rows">
                <thead className="bg-surface text-text-muted">
                  <tr>
                    <th className="p-2 text-start">{t("import.col.row")}</th>
                    <th className="p-2 text-start">{t("import.col.name_ar")}</th>
                    <th className="p-2 text-start">{t("import.col.unit")}</th>
                    <th className="p-2 text-start">{t("import.col.price")}</th>
                    <th className="p-2 text-start">{t("import.col.quantity")}</th>
                  </tr>
                </thead>
                <tbody>
                  {preview.rows.slice(0, PREVIEW_ROWS).map((r) => (
                    <tr key={r.row} className="border-t border-border">
                      <td className="p-2">{r.row}</td>
                      <td className="p-2">{locale === "en" && r.nameEn ? r.nameEn : r.nameAr}</td>
                      <td className="p-2">{tDynamic(`uom.${r.unitCode}`)}</td>
                      <td className="p-2">
                        <bdi dir="ltr">
                          {formatDecimal(r.price, locale)} {tDynamic(`currency.short.${r.currency}`)}
                        </bdi>
                      </td>
                      <td className="p-2">
                        <bdi dir="ltr">{r.quantity ? formatDecimal(r.quantity, locale) : "—"}</bdi>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </div>
      ) : null}

      <div className="flex justify-end gap-2">
        <Button onClick={onClose}>{t("action.close")}</Button>
        <Button variant="primary" onClick={() => void apply()} disabled={!canApply}>
          {t("import.apply", { count: String(preview?.rows.length ?? 0) })}
        </Button>
      </div>
    </Dialog>
  );
}
