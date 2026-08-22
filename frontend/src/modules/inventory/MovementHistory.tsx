import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { stockMovements, type MovementRow, type StockRow } from "@/lib/wails";
import { Alert, Badge, Button, EmptyState, PageHeader, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "@/modules/accounting/money";
import { formatQuantity } from "./quantity";

/**
 * One variant's movement ledger, in occurrence order.
 *
 * This is the audit story: every receipt, issue, adjustment, and count that produced the number
 * on the stock screen, with the balance each one left behind. A stock figure somebody cannot
 * explain is a stock figure they will override by hand.
 */
export function MovementHistory({ row, onBack }: { row: StockRow; onBack: () => void }) {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const movements = useQuery({
    queryKey: ["inventory", "movements", row.variantId],
    queryFn: () => stockMovements(row.variantId, ""),
  });

  return (
    <section className="flex flex-col gap-4">
      <div>
        <Button variant="ghost" onClick={onBack}>{t("stock.back")}</Button>
      </div>

      <PageHeader title={row.productName} />
      <p className="-mt-4 font-mono text-xs text-text-muted">{row.sku}</p>

      {movements.isPending && <p className="text-sm text-text-muted">{t("gate.checking")}</p>}
      {movements.isError && (
        <Alert tone="danger" title={t("stock.failed")}>{errorText(movements.error)}</Alert>
      )}

      {movements.data && (
        <Table<MovementRow>
          caption={t("stock.movements")}
          rowKey={(movement) => movement.id}
          rows={movements.data}
          empty={<EmptyState title={t("stock.noMovements")} />}
          columns={[
            {
              key: "when",
              header: t("stock.when"),
              // The date the stock actually moved, which is not when the row was written: a
              // delivery note entered on Monday for goods received on Friday belongs to Friday.
              cell: (movement) => movement.occurredAt.slice(0, 10),
            },
            {
              key: "type",
              header: t("stock.movementType"),
              cell: (movement) => t(`stock.type.${movement.movementType}`),
            },
            {
              key: "quantity",
              header: t("stock.quantity"),
              cell: (movement) => (
                <span className="flex items-center gap-2">
                  {/* The direction comes from the backend, which reads it from the movement
                      TYPE — the frontend does not re-derive it, and the quantity itself is
                      always positive. */}
                  <Badge tone={movement.isInward ? "success" : "neutral"}>
                    {movement.isInward ? t("stock.in") : t("stock.out")}
                  </Badge>
                  {formatQuantity(movement.quantityMicro)} {row.unit}
                </span>
              ),
            },
            {
              key: "balance",
              header: t("stock.balanceAfter"),
              // Recorded on the movement at write time, not recomputed here. It is what lets the
              // verifier say WHERE a drift began rather than only that one exists.
              cell: (movement) => formatQuantity(movement.balanceAfterMicro),
            },
            {
              key: "value",
              header: t("stock.value"),
              cell: (movement) => (movement.valueMinor ? formatMinor(movement.valueMinor) : "—"),
            },
            {
              key: "why",
              header: t("stock.reason"),
              cell: (movement) =>
                movement.reason || (movement.documentType ? movement.documentType : "—"),
            },
          ]}
        />
      )}
    </section>
  );
}
