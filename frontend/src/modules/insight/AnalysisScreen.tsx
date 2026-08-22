import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import {
  salesByPartner,
  salesByPeriod,
  salesByProduct,
  spendByPeriod,
  spendByProduct,
  spendBySupplier,
  type Analysis,
  type AnalysisRow,
} from "@/lib/wails";
import { Alert, EmptyState, Input, PageHeader, Select, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "@/modules/accounting/money";

type Side = "sales" | "spend";
type Cut = "period" | "product" | "partner";

/**
 * Sales and spend, cut three ways each.
 *
 * # Why the two sides share a screen and not a table shape
 *
 * They answer the mirrored halves of one question, and a shopkeeper comparing them wants the same
 * date range on both. What they do NOT share is columns: a sale has a cost and therefore a
 * margin, a purchase does not — the cost IS the purchase. So the margin columns are absent on the
 * spend side rather than rendered as zero, because zero would claim it broke even.
 */
export function AnalysisScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const [side, setSide] = useState<Side>("sales");
  const [cut, setCut] = useState<Cut>("period");
  const [from, setFrom] = useState(() => firstOfThisMonth());
  const [to, setTo] = useState(() => today());

  const analysis = useQuery({
    queryKey: ["insight", "analysis", side, cut, from, to],
    queryFn: () => fetchAnalysis(side, cut, from, to),
  });

  const showsMargin = side === "sales";

  return (
    <section className="flex flex-col gap-6">
      <PageHeader title={t("analysis.title")} description={t("analysis.help")} />

      <div className="flex flex-wrap items-end gap-3">
        <Select
          label={t("analysis.side")}
          value={side}
          onValueChange={(value) => setSide(value as Side)}
          options={[
            { value: "sales", label: t("analysis.sales") },
            { value: "spend", label: t("analysis.spend") },
          ]}
        />
        <Select
          label={t("analysis.cut")}
          value={cut}
          onValueChange={(value) => setCut(value as Cut)}
          options={[
            { value: "period", label: t("analysis.byPeriod") },
            { value: "product", label: t("analysis.byProduct") },
            {
              value: "partner",
              label: side === "sales" ? t("analysis.byCustomer") : t("analysis.bySupplier"),
            },
          ]}
        />
        <Input
          type="date"
          label={t("analysis.from")}
          value={from}
          onChange={(event) => setFrom(event.target.value)}
        />
        <Input
          type="date"
          label={t("analysis.to")}
          value={to}
          onChange={(event) => setTo(event.target.value)}
        />
      </div>

      {analysis.isError && (
        <Alert tone="danger" title={t("analysis.failed")}>{errorText(analysis.error)}</Alert>
      )}

      {analysis.data && (
        <>
          <Table<AnalysisRow>
            caption={t("analysis.title")}
            rowKey={(row) => row.key}
            rows={analysis.data.rows}
            empty={<EmptyState title={t("analysis.none")} />}
            columns={[
              { key: "label", header: t("analysis.label"), cell: (row) => row.label },
              {
                key: "documents",
                header: t("analysis.documents"),
                cell: (row) => <span className="tabular-nums">{row.documents}</span>,
              },
              {
                key: "revenue",
                header: showsMargin ? t("analysis.revenue") : t("analysis.spendAmount"),
                cell: (row) => (
                  <span className="tabular-nums">{formatMinor(row.revenueMinor)}</span>
                ),
              },
              ...(showsMargin
                ? [
                    {
                      key: "cost",
                      header: t("analysis.cost"),
                      cell: (row: AnalysisRow) => (
                        <span className="tabular-nums">{formatMinor(row.costMinor)}</span>
                      ),
                    },
                    {
                      key: "margin",
                      /* Gross margin, named in full. The ledger owns the word "profit". */
                      header: t("analysis.grossMargin"),
                      cell: (row: AnalysisRow) => (
                        <span className="tabular-nums">{formatMinor(row.grossMarginMinor)}</span>
                      ),
                    },
                    {
                      key: "marginPercent",
                      header: t("analysis.marginPercent"),
                      cell: (row: AnalysisRow) => (
                        <span className="tabular-nums">{percent(row.marginPercentMicro)}</span>
                      ),
                    },
                  ]
                : []),
            ]}
          />

          {/*
           * The total, from the backend rather than summed here.
           *
           * Summing the rows in the browser would be a second implementation, and one that would
           * silently be wrong the day a report is paginated.
           */}
          <div className="flex flex-wrap items-center justify-end gap-6 rounded-lg border border-border bg-surface-muted px-4 py-3 text-sm">
            <span className="text-text-muted">{t("analysis.total")}</span>
            <span className="tabular-nums font-semibold">
              {formatMinor(analysis.data.total.revenueMinor)}
            </span>
            {showsMargin && (
              <span className="tabular-nums text-text-muted">
                {t("analysis.grossMargin")}: {formatMinor(analysis.data.total.grossMarginMinor)}
              </span>
            )}
          </div>
        </>
      )}
    </section>
  );
}

function fetchAnalysis(side: Side, cut: Cut, from: string, to: string): Promise<Analysis> {
  if (side === "sales") {
    if (cut === "period") return salesByPeriod(from, to, "day");
    if (cut === "product") return salesByProduct(from, to);
    return salesByPartner(from, to);
  }
  if (cut === "period") return spendByPeriod(from, to, "day");
  if (cut === "product") return spendByProduct(from, to);
  return spendBySupplier(from, to);
}

/** A percent at §E's Percent scale (×10⁶), rendered without float arithmetic on the value. */
function percent(micro: string): string {
  if (micro === "" || micro === "0") return "—";
  const negative = micro.startsWith("-");
  const digits = (negative ? micro.slice(1) : micro).padStart(5, "0");
  const whole = digits.slice(0, -4) || "0";
  const fraction = digits.slice(-4, -2);
  return `${negative ? "-" : ""}${whole}.${fraction}%`;
}

function today(): string {
  return new Date().toISOString().slice(0, 10);
}

function firstOfThisMonth(): string {
  return `${new Date().toISOString().slice(0, 7)}-01`;
}
