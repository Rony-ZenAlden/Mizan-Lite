import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { supplierPayments, type SupplierPayment } from "@/lib/wails";
import { Alert, Badge, EmptyState, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "@/modules/accounting/money";

/**
 * What has been paid to suppliers.
 *
 * # Why the METHOD is a column
 *
 * 6.7 found a defect worth remembering: one posting rule credited cash whatever the method, so a
 * bank transfer would have reduced the till — and the till would have been short at every close
 * with no transaction to explain it.
 *
 * Showing the method on every row is what lets somebody reconciling a till see immediately that
 * the payment they are looking for went out of the bank.
 */
export function SupplierPaymentsScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const payments = useQuery({
    queryKey: ["purchasing", "payments"],
    queryFn: supplierPayments,
  });

  if (payments.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (payments.isError) {
    return (
      <Alert tone="danger" title={t("supplierPayments.failed")}>
        {errorText(payments.error)}
      </Alert>
    );
  }

  return (
    <section className="flex flex-col gap-6">
      <header className="flex flex-col gap-1">
        <h2 className="text-base font-medium text-text">{t("supplierPayments.title")}</h2>
        <p className="text-sm text-text-muted">{t("supplierPayments.help")}</p>
      </header>

      <Table<SupplierPayment>
        caption={t("supplierPayments.title")}
        rowKey={(row) => row.id}
        rows={payments.data}
        empty={<EmptyState title={t("supplierPayments.none")} />}
        columns={[
          {
            key: "number",
            header: t("supplierPayments.number"),
            cell: (row) => <span className="font-mono text-xs">{row.number}</span>,
          },
          { key: "date", header: t("supplierPayments.date"), cell: (row) => row.paymentDate },
          {
            key: "supplier",
            header: t("supplierPayments.supplier"),
            cell: (row) => row.supplierName,
          },
          {
            key: "method",
            header: t("supplierPayments.method"),
            cell: (row) => (
              <Badge tone="neutral">{t(`supplierPayments.method.${row.method}`)}</Badge>
            ),
          },
          {
            key: "reference",
            header: t("supplierPayments.reference"),
            cell: (row) => row.reference || "—",
          },
          {
            key: "amount",
            header: t("supplierPayments.amount"),
            cell: (row) => <span className="tabular-nums">{formatMinor(row.amountMinor)}</span>,
          },
        ]}
      />
    </section>
  );
}
