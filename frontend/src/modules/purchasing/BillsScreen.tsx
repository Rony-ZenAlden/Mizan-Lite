import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { goodsReceipts, purchaseBills, type GoodsReceipt, type PurchaseBill } from "@/lib/wails";
import { Alert, Badge, EmptyState, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor, isZeroMinor } from "@/modules/accounting/money";

/**
 * What we owe suppliers, and what has arrived that nobody has billed us for.
 *
 * # The two halves belong on one screen
 *
 * A bills list answers "what do we owe". The GRNI list answers "what have we received and not
 * been invoiced for" — and a delivery sitting there for weeks is either a missing invoice or
 * goods somebody forgot to charge us for. Both are the same question asked from opposite ends,
 * and separating them means the second one is never opened.
 */
export function BillsScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const bills = useQuery({
    queryKey: ["purchasing", "bills"],
    queryFn: () => purchaseBills(""),
  });
  // Confirmed deliveries. The unbilled ones are what GRNI is holding.
  const receipts = useQuery({
    queryKey: ["purchasing", "receipts", "confirmed"],
    queryFn: () => goodsReceipts("", "confirmed"),
  });

  if (bills.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (bills.isError) {
    return (
      <Alert tone="danger" title={t("purchasing.billsFailed")}>{errorText(bills.error)}</Alert>
    );
  }

  const unbilled = (receipts.data ?? []).filter((receipt) => !receipt.billed);

  return (
    <section className="flex flex-col gap-6">
      <div className="flex flex-col gap-4">
        <header className="flex flex-col gap-1">
          <h2 className="text-base font-medium text-text">{t("purchasing.bills")}</h2>
          <p className="text-sm text-text-muted">{t("purchasing.billsHelp")}</p>
        </header>

        <Table<PurchaseBill>
          caption={t("purchasing.bills")}
          rowKey={(row) => row.id}
          rows={bills.data}
          empty={<EmptyState title={t("purchasing.noBills")} />}
          columns={[
            {
              key: "supplierInvoice",
              header: t("purchasing.supplierInvoice"),
              cell: (row) => (
                // THEIR number, not ours. It is what a payment reference quotes and what a
                // statement reconciliation matches on.
                <span className="font-mono text-xs">{row.supplierInvoiceNumber}</span>
              ),
            },
            {
              key: "supplier",
              header: t("purchasing.supplier"),
              cell: (row) => row.supplierName,
            },
            { key: "date", header: t("purchasing.billDate"), cell: (row) => row.billDate },
            {
              key: "due",
              header: t("purchasing.due"),
              cell: (row) => row.dueDate || "—",
            },
            {
              key: "total",
              header: t("purchasing.total"),
              cell: (row) => formatMinor(row.totalMinor),
            },
            {
              key: "variance",
              header: t("purchasing.variance"),
              cell: (row) =>
                // A price variance is ACCEPTED, which makes it invisible unless something
                // surfaces it. This is that something.
                isZeroMinor(row.varianceMinor) ? (
                  "—"
                ) : (
                  <Badge tone="warning">{formatMinor(row.varianceMinor)}</Badge>
                ),
            },
            {
              key: "outstanding",
              header: t("purchasing.outstandingAmount"),
              cell: (row) => {
                if (row.status !== "posted") return "—";
                return isZeroMinor(row.outstandingMinor) ? (
                  <Badge tone="success">{t("purchasing.paid")}</Badge>
                ) : (
                  <span className="font-mono tabular-nums text-warning">
                    {formatMinor(row.outstandingMinor)}
                  </span>
                );
              },
            },
          ]}
        />
      </div>

      {/* ── goods received not invoiced ─────────────────────────────────────── */}
      <div className="flex flex-col gap-4">
        <header className="flex flex-col gap-1">
          <h2 className="text-base font-medium text-text">{t("purchasing.grni")}</h2>
          <p className="text-sm text-text-muted">{t("purchasing.grniHelp")}</p>
        </header>

        <Table<GoodsReceipt>
          caption={t("purchasing.grni")}
          rowKey={(row) => row.id}
          rows={unbilled}
          empty={<EmptyState title={t("purchasing.grniEmpty")} />}
          columns={[
            {
              key: "number",
              header: t("purchasing.number"),
              cell: (row) => <span className="font-mono text-xs">{row.number}</span>,
            },
            {
              key: "supplier",
              header: t("purchasing.supplier"),
              cell: (row) => row.supplierName,
            },
            { key: "date", header: t("purchasing.receiptDate"), cell: (row) => row.receiptDate },
            {
              key: "note",
              header: t("purchasing.deliveryNote"),
              cell: (row) => row.deliveryNoteReference || "—",
            },
            {
              key: "value",
              header: t("purchasing.accrued"),
              cell: (row) => formatMinor(row.valueMinor),
            },
          ]}
        />
      </div>
    </section>
  );
}
