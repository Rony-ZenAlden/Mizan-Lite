import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { importPartners, importProducts, type ImportReport, type ImportRow } from "@/lib/wails";
import { Alert, Button, EmptyState, Select, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";

type Kind = "products" | "partners";

/**
 * Bringing a product or customer list in from a spreadsheet.
 *
 * # The dry run is not optional, it is the default
 *
 * Importing four thousand products into a live shop is not a thing to do twice. The screen checks
 * first, shows exactly what would happen, and only then offers to commit — and the commit button
 * does not appear until a check has run.
 */
export function ImportScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const [kind, setKind] = useState<Kind>("products");
  const [content, setContent] = useState<string>("");
  const [filename, setFilename] = useState<string>("");
  const [report, setReport] = useState<ImportReport | null>(null);

  const run = useMutation({
    mutationFn: ({ dryRun }: { dryRun: boolean }) =>
      kind === "products" ? importProducts(content, dryRun) : importPartners(content, dryRun),
    onSuccess: setReport,
  });

  async function chooseFile(file: File) {
    setFilename(file.name);
    setReport(null);
    const buffer = await file.arrayBuffer();
    // Base64 in chunks: a single spread of a large array overflows the call stack, and a
    // four-thousand-row file is large enough to reach it.
    const bytes = new Uint8Array(buffer);
    let binary = "";
    for (let i = 0; i < bytes.length; i += 8192) {
      binary += String.fromCharCode(...bytes.subarray(i, i + 8192));
    }
    setContent(btoa(binary));
  }

  const checked = report !== null && report.dryRun;

  return (
    <section className="flex flex-col gap-6">
      <header className="flex flex-col gap-1">
        <h2 className="text-base font-medium text-text">{t("imports.title")}</h2>
        <p className="text-sm text-text-muted">{t("imports.help")}</p>
      </header>

      <div className="flex flex-wrap items-end gap-3">
        <Select
          label={t("imports.kind")}
          value={kind}
          onValueChange={(value) => {
            setKind(value as Kind);
            setReport(null);
          }}
          options={[
            { value: "products", label: t("imports.products") },
            { value: "partners", label: t("imports.partners") },
          ]}
        />
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-text-muted">{t("imports.file")}</span>
          <input
            type="file"
            accept=".csv,text/csv"
            onChange={(event) => {
              const file = event.target.files?.[0];
              if (file) void chooseFile(file);
            }}
            className="text-sm"
          />
        </label>
      </div>

      {filename && <p className="text-xs text-text-muted">{filename}</p>}

      <div className="flex gap-2">
        <Button
          onClick={() => run.mutate({ dryRun: true })}
          disabled={!content || run.isPending}
        >
          {t("imports.check")}
        </Button>
        {/*
         * The commit button appears only AFTER a check. A user who has not seen what would happen
         * cannot agree to it, and an import is not something to discover the shape of afterwards.
         */}
        {checked && report.failed < report.total && (
          <Button
            variant="danger"
            onClick={() => run.mutate({ dryRun: false })}
            disabled={run.isPending}
          >
            {t("imports.commit", { count: String(report.total - report.failed) })}
          </Button>
        )}
      </div>

      {run.isError && (
        <Alert tone="danger" title={t("imports.importFailed")}>{errorText(run.error)}</Alert>
      )}

      {report && (
        <>
          <Alert tone={report.failed === 0 ? "info" : "warning"} title={
            report.dryRun ? t("imports.checkedTitle") : t("imports.doneTitle")
          }>
            {t(report.dryRun ? "imports.checked" : "imports.done", {
              total: String(report.total),
              succeeded: String(report.succeeded),
              failed: String(report.failed),
            })}
          </Alert>

          {/*
           * EVERY row, not only the failures. A user who imported 4,000 products and got 12
           * failures wants to see which 12; one who got none wants to see the file was read.
           */}
          <Table<ImportRow>
            caption={t("imports.rows")}
            rowKey={(row) => `${row.line}`}
            rows={report.rows}
            empty={<EmptyState title={t("imports.noRows")} />}
            columns={[
              {
                key: "line",
                header: t("imports.line"),
                cell: (row) => <span className="tabular-nums">{row.line}</span>,
              },
              { key: "key", header: t("imports.key"), cell: (row) => row.key },
              {
                key: "outcome",
                header: t("imports.outcome"),
                cell: (row) =>
                  row.ok ? (
                    <span className="text-success">{t("imports.ok")}</span>
                  ) : (
                    <span className="text-danger">{row.message}</span>
                  ),
              },
            ]}
          />
        </>
      )}
    </section>
  );
}
