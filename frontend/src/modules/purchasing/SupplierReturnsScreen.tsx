import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { supplierReturns, type SupplierReturn } from "@/lib/wails";
import { Alert, Badge, EmptyState, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "@/modules/accounting/money";

/**
 * Goods sent back to a supplier.
 *
 * # Why the credit and the cost are both columns
 *
 * They are different numbers and the difference is real. The supplier credits what they CHARGED;
 * the stock leaves at what the original delivery COST us. §D.3 requires the second, because
 * costing a return at today's average would invent a profit or a loss that never happened.
 *
 * A screen showing only the credit would leave somebody unable to explain why the inventory
 * account moved by a different figure — so both are shown, side by side, on every row.
 */
export function SupplierReturnsScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const returns = useQuery({
    queryKey: ["purchasing", "returns"],
    queryFn: () => supplierReturns(),
  });

  if (returns.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (returns.isError) {
    return (
      <Alert tone="danger" title={t("supplierReturns.failed")}>{errorText(returns.error)}</Alert>
    );
  }

  return (
    <section className="flex flex-col gap-6">
      <header className="flex flex-col gap-1">
        <h2 className="text-base font-medium text-text">{t("supplierReturns.title")}</h2>
        <p className="text-sm text-text-muted">{t("supplierReturns.help")}</p>
      </header>

      <Table<SupplierReturn>
        caption={t("supplierReturns.title")}
        rowKey={(row) => row.id}
        rows={returns.data}
        empty={<EmptyState title={t("supplierReturns.none")} />}
        columns={[
          {
            key: "number",
            header: t("supplierReturns.number"),
            cell: (row) => (
              <span className="font-mono text-xs">{row.number || t("supplierReturns.draft")}</span>
            ),
          },
          { key: "date", header: t("supplierReturns.date"), cell: (row) => row.returnDate },
          {
            key: "supplier",
            header: t("supplierReturns.supplier"),
            cell: (row) => row.supplierName,
          },
          {
            key: "reason",
            header: t("supplierReturns.reason"),
            cell: (row) => row.reason || "—",
          },
          {
            key: "credit",
            header: t("supplierReturns.credit"),
            cell: (row) => <span className="tabular-nums">{formatMinor(row.totalMinor)}</span>,
          },
          {
            key: "cost",
            /* What the goods cost US, at the original delivery's figure — not today's average. */
            header: t("supplierReturns.cost"),
            cell: (row) => (
              <span className="tabular-nums text-text-muted">{formatMinor(row.costMinor)}</span>
            ),
          },
          {
            key: "status",
            header: t("supplierReturns.status"),
            cell: (row) => (
              <Badge tone={row.status === "posted" ? "success" : "neutral"}>
                {t(`supplierReturns.status.${row.status}`)}
              </Badge>
            ),
          },
        ]}
      />
    </section>
  );
}
