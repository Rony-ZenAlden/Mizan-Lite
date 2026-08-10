import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { fiscalPeriods, trialBalance, type TrialBalanceRow } from "@/lib/wails";
import { Alert, EmptyState, Select, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "./money";

/**
 * The trial balance for one period (§20.6 tier v1.1).
 *
 * Every amount is formatted from a STRING of minor units. Nothing here adds anything up: the
 * totals and the balanced flag are computed in Go, where they are exact integers, because a
 * report that disagreed with the ledger by one minor unit would be worse than no report.
 */
export function TrialBalanceScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const [periodID, setPeriodID] = useState("");

  const periods = useQuery({ queryKey: ["accounting", "periods"], queryFn: fiscalPeriods });

  // The first open period is the one an accountant is usually looking at.
  const selected = periodID || periods.data?.find((p) => p.status === "open")?.id || periods.data?.[0]?.id || "";

  const balance = useQuery({
    queryKey: ["accounting", "trialBalance", selected],
    queryFn: () => trialBalance(selected),
    enabled: selected !== "",
  });

  if (periods.isError) {
    return <Alert tone="danger" title={t("accounting.trial.failed")}>{errorText(periods.error)}</Alert>;
  }

  return (
    <section className="flex flex-col gap-4">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h2 className="text-base font-medium text-text">{t("accounting.trial.title")}</h2>
          <p className="text-sm text-text-muted">{t("accounting.trial.help")}</p>
        </div>
        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium text-text">{t("accounting.trial.period")}</span>
          <Select
            value={selected}
            onValueChange={setPeriodID}
            ariaLabel={t("accounting.trial.period")}
            options={(periods.data ?? []).map((period) => ({
              value: period.id,
              label: `${period.start} — ${t(`accounting.period.${period.status}`)}`,
            }))}
          />
        </div>
      </header>

      {balance.isError ? (
        <Alert tone="danger" title={t("accounting.trial.failed")}>{errorText(balance.error)}</Alert>
      ) : null}

      {balance.data && !balance.data.balanced ? (
        // The one thing on this screen that must never be quiet. An unbalanced trial balance
        // means the books are wrong, and a report that showed it as an ordinary number would
        // let somebody read past it.
        <Alert tone="danger" title={t("accounting.trial.unbalanced")}>
          {t("accounting.trial.unbalanced.help")}
        </Alert>
      ) : null}

      <Table<TrialBalanceRow>
        caption={t("accounting.trial.title")}
        rowKey={(row) => row.accountId}
        rows={balance.data?.rows ?? []}
        empty={<EmptyState title={t("accounting.trial.none")} />}
        columns={[
          { key: "code", header: t("accounting.chart.code"), cell: (row) => (
            <span className="font-mono text-xs">{row.code}</span>
          ) },
          { key: "name", header: t("accounting.chart.name"), cell: (row) => row.name },
          {
            key: "opening", header: t("accounting.trial.opening"), numeric: true,
            cell: (row) => formatMinor(row.openingMinor),
          },
          {
            key: "debit", header: t("accounting.trial.debit"), numeric: true,
            cell: (row) => formatMinor(row.debitMinor),
          },
          {
            key: "credit", header: t("accounting.trial.credit"), numeric: true,
            cell: (row) => formatMinor(row.creditMinor),
          },
          {
            key: "closing", header: t("accounting.trial.closing"), numeric: true,
            cell: (row) => formatMinor(row.closingMinor),
          },
        ]}
      />

      {balance.data ? (
        <dl className="flex flex-wrap gap-6 border-t border-border pt-3 text-sm">
          <div className="flex gap-2">
            <dt className="text-text-muted">{t("accounting.trial.totalDebit")}</dt>
            <dd className="font-mono text-text">{formatMinor(balance.data.totalDebitMinor)}</dd>
          </div>
          <div className="flex gap-2">
            <dt className="text-text-muted">{t("accounting.trial.totalCredit")}</dt>
            <dd className="font-mono text-text">{formatMinor(balance.data.totalCreditMinor)}</dd>
          </div>
        </dl>
      ) : null}
    </section>
  );
}
