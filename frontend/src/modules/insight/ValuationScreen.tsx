import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { stockValuation, type ValuedLine } from "@/lib/wails";
import { Alert, Badge, EmptyState, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "@/modules/accounting/money";
import { formatQuantity } from "@/modules/inventory/quantity";

/**
 * What the stock is worth, and whether the books agree.
 *
 * # The reconciliation is the point, not the list
 *
 * A valuation on its own is a sum of levels that agrees with itself and with nothing else. The
 * two figures here are computed by completely different paths — one multiplies each level's
 * quantity by the average cost, the other sums journal lines written by posting rules — and when
 * they disagree, nothing else in the system will say so.
 */
export function ValuationScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const valuation = useQuery({
    queryKey: ["insight", "valuation"],
    queryFn: stockValuation,
  });

  if (valuation.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (valuation.isError) {
    return (
      <Alert tone="danger" title={t("valuation.failed")}>{errorText(valuation.error)}</Alert>
    );
  }

  return (
    <section className="flex flex-col gap-6">
      <header className="flex flex-col gap-1">
        <h2 className="text-base font-medium text-text">{t("valuation.title")}</h2>
        <p className="text-sm text-text-muted">{t("valuation.help")}</p>
      </header>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <Figure label={t("valuation.onTheShelf")} value={formatMinor(valuation.data.totalMinor)} />
        {/*
         * "Nobody asked the books" is shown as its own state, not as agreement.
         *
         * A difference of zero rendered in both cases would claim a check that never happened —
         * which is the failure a scheduled verifier makes silently and a screen must not.
         */}
        {valuation.data.hasLedger ? (
          <>
            <Figure
              label={t("valuation.inTheBooks")}
              value={formatMinor(valuation.data.ledgerMinor)}
            />
            <Figure
              label={t("valuation.difference")}
              value={formatMinor(valuation.data.differenceMinor)}
              tone={valuation.data.reconciled ? "success" : "danger"}
            />
          </>
        ) : (
          <Alert tone="info" title={t("valuation.noLedger")}>
            {t("valuation.noLedgerHelp")}
          </Alert>
        )}
      </div>

      {valuation.data.hasLedger && !valuation.data.reconciled && (
        <Alert tone="danger" title={t("valuation.disagrees")}>
          {t("valuation.disagreesHelp", {
            amount: formatMinor(valuation.data.differenceMinor),
          })}
        </Alert>
      )}

      <Table<ValuedLine>
        caption={t("valuation.lines")}
        rowKey={(row) => `${row.warehouseId}:${row.variantId}`}
        rows={valuation.data.lines}
        empty={<EmptyState title={t("valuation.none")} />}
        columns={[
          { key: "product", header: t("valuation.product"), cell: (row) => row.productName },
          {
            key: "sku",
            header: t("valuation.sku"),
            cell: (row) => <span className="font-mono text-xs">{row.variantSku}</span>,
          },
          {
            key: "warehouse",
            header: t("valuation.warehouse"),
            cell: (row) => row.warehouseName,
          },
          {
            key: "quantity",
            header: t("valuation.quantity"),
            cell: (row) => (
              <span className="tabular-nums">{formatQuantity(row.quantityMicro)}</span>
            ),
          },
          {
            key: "value",
            header: t("valuation.value"),
            cell: (row) => <span className="tabular-nums">{formatMinor(row.valueMinor)}</span>,
          },
        ]}
      />
    </section>
  );
}

function Figure({
  label,
  value,
  tone,
}: {
  label: string;
  value: string;
  tone?: "success" | "danger";
}) {
  return (
    <article className="flex flex-col gap-1 rounded-lg border border-border bg-surface p-4">
      <span className="text-sm text-text-muted">{label}</span>
      <span className="text-xl font-semibold tabular-nums text-text">{value}</span>
      {tone && (
        <Badge tone={tone === "success" ? "success" : "danger"}>
          {tone === "success" ? "✓" : "!"}
        </Badge>
      )}
    </article>
  );
}
