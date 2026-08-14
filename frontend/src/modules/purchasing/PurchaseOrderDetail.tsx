import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { purchaseOrder, type PurchaseOrderLine } from "@/lib/wails";
import { Alert, Badge, Button, EmptyState, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "@/modules/accounting/money";
import { formatQuantity, isZeroQuantity } from "@/modules/inventory/quantity";

/**
 * One purchase order, and what is still outstanding on it.
 *
 * # Outstanding is per LINE, and that is the point
 *
 * An order is rarely late as a whole. It is late in one line — the item the supplier is waiting on
 * — and a document-level "partly received" tells a buyer nothing they can act on. The line's
 * outstanding figure is what a telephone call to the supplier is about.
 */
export function PurchaseOrderDetail({
  orderId,
  onBack,
}: {
  orderId: string;
  onBack: () => void;
}) {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const detail = useQuery({
    queryKey: ["purchasing", "order", orderId],
    queryFn: () => purchaseOrder(orderId),
  });

  if (detail.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (detail.isError) {
    return (
      <Alert tone="danger" title={t("purchasing.ordersFailed")}>{errorText(detail.error)}</Alert>
    );
  }

  const { order, lines } = detail.data;

  return (
    <section className="flex flex-col gap-4">
      <div>
        <Button variant="ghost" onClick={onBack}>
          {t("common.back")}
        </Button>
      </div>

      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h2 className="font-mono text-base font-medium text-text">
            {order.number || t("purchasing.unnumbered")}
          </h2>
          <p className="text-sm text-text-muted">
            {order.supplierName} · {order.orderDate}
            {order.supplierReference ? ` · ${order.supplierReference}` : ""}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Badge tone={order.status === "placed" ? "success" : "neutral"}>
            {t(`purchasing.status.${order.status}`)}
          </Badge>
          {order.fullyReceived && (
            <Badge tone="success">{t("purchasing.fullyReceived")}</Badge>
          )}
        </div>
      </header>

      <Table<PurchaseOrderLine>
        caption={t("purchasing.lines")}
        rowKey={(line) => line.id}
        rows={lines}
        empty={<EmptyState title={t("purchasing.noLines")} />}
        columns={[
          { key: "no", header: "#", cell: (line) => String(line.lineNumber) },
          { key: "sku", header: t("purchasing.sku"), cell: (line) => line.sku },
          {
            key: "item",
            header: t("purchasing.item"),
            cell: (line) => (
              <span className="flex flex-col">
                <span>{line.productName}</span>
                {/* What the SUPPLIER calls it. An order that goes out under our code gets
                    filled wrong; a delivery note under theirs cannot be matched. */}
                {line.supplierCode && (
                  <span className="font-mono text-xs text-text-muted">{line.supplierCode}</span>
                )}
              </span>
            ),
          },
          {
            key: "ordered",
            header: t("purchasing.ordered"),
            cell: (line) => `${formatQuantity(line.quantityMicro)} ${line.uomCode}`,
          },
          {
            key: "received",
            header: t("purchasing.received"),
            cell: (line) => formatQuantity(line.receivedMicro),
          },
          {
            key: "outstanding",
            header: t("purchasing.outstanding"),
            cell: (line) =>
              isZeroQuantity(line.outstandingMicro) ? (
                <Badge tone="success">{t("purchasing.complete")}</Badge>
              ) : (
                <span className="font-mono tabular-nums text-warning">
                  {formatQuantity(line.outstandingMicro)}
                </span>
              ),
          },
          {
            key: "price",
            header: t("purchasing.unitPrice"),
            cell: (line) =>
              // Six implied decimals on a unit price is more than anybody reads; two is what a
              // price looks like.
              isZeroQuantity(line.unitPriceMicro)
                ? "—"
                : formatQuantity(line.unitPriceMicro, 2),
          },
          {
            key: "total",
            header: t("purchasing.lineTotal"),
            cell: (line) => formatMinor(line.totalMinor),
          },
        ]}
      />

      <dl className="ms-auto flex w-full max-w-xs flex-col gap-1 text-sm">
        <Figure label={t("purchasing.net")} value={formatMinor(order.netMinor)} />
        <Figure label={t("purchasing.tax")} value={formatMinor(order.taxMinor)} />
        <div className="mt-1 flex items-baseline justify-between border-t border-border pt-2">
          <dt className="font-medium text-text">{t("purchasing.total")}</dt>
          <dd className="font-mono text-lg tabular-nums text-text">
            {formatMinor(order.totalMinor)}
          </dd>
        </div>
      </dl>
    </section>
  );
}

function Figure({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between">
      <dt className="text-text-muted">{label}</dt>
      <dd className="font-mono tabular-nums text-text">{value}</dd>
    </div>
  );
}
