import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { goodsReceipts, type GoodsReceipt } from "@/lib/wails";
import { Alert, Badge, EmptyState, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "@/modules/accounting/money";

/**
 * Goods that have arrived.
 *
 * # Why "billed" is a column and not a filter
 *
 * A delivery nobody has invoiced yet sits in GRNI — goods received, not invoiced — and that
 * balance is what an accountant reconciles at every month end. Showing the flag on every row is
 * how somebody finds the three deliveries holding the balance up; a filter would make them go
 * looking for it.
 */
export function ReceiptsScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const receipts = useQuery({
    queryKey: ["purchasing", "receipts"],
    queryFn: () => goodsReceipts(),
  });

  if (receipts.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (receipts.isError) {
    return (
      <Alert tone="danger" title={t("receipts.failed")}>{errorText(receipts.error)}</Alert>
    );
  }

  const unbilled = receipts.data.filter((row) => !row.billed);

  return (
    <section className="flex flex-col gap-6">
      <header className="flex flex-col gap-1">
        <h2 className="text-base font-medium text-text">{t("receipts.title")}</h2>
        <p className="text-sm text-text-muted">{t("receipts.help")}</p>
      </header>

      {/*
       * The GRNI total, above the list. It is the figure an accountant is looking for, and
       * making them add up a column to find it is how a screen fails somebody at month end.
       */}
      {unbilled.length > 0 && (
        <Alert tone="info" title={t("receipts.awaitingInvoices")}>
          {t("receipts.awaitingInvoicesHelp", {
            count: String(unbilled.length),
            value: formatMinor(
              unbilled
                .reduce((total, row) => total + BigInt(row.valueMinor || "0"), 0n)
                .toString(),
            ),
          })}
        </Alert>
      )}

      <Table<GoodsReceipt>
        caption={t("receipts.title")}
        rowKey={(row) => row.id}
        rows={receipts.data}
        empty={<EmptyState title={t("receipts.none")} />}
        columns={[
          {
            key: "number",
            header: t("receipts.number"),
            cell: (row) => <span className="font-mono text-xs">{row.number}</span>,
          },
          { key: "date", header: t("receipts.date"), cell: (row) => row.receiptDate },
          {
            key: "supplier",
            header: t("receipts.supplier"),
            cell: (row) => row.supplierName,
          },
          {
            key: "reference",
            header: t("receipts.deliveryNote"),
            cell: (row) => row.deliveryNoteReference || "—",
          },
          {
            key: "value",
            header: t("receipts.value"),
            cell: (row) => <span className="tabular-nums">{formatMinor(row.valueMinor)}</span>,
          },
          {
            key: "billed",
            header: t("receipts.invoiced"),
            cell: (row) => (
              <Badge tone={row.billed ? "success" : "warning"}>
                {t(row.billed ? "receipts.billed" : "receipts.awaitingInvoice")}
              </Badge>
            ),
          },
        ]}
      />
    </section>
  );
}
