import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { checkLedger, stockOnHand, type StockRow } from "@/lib/wails";
import { Alert, Badge, EmptyState, Input, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "@/modules/accounting/money";
import { formatQuantity, isNegativeQuantity, isZeroQuantity } from "./quantity";
import { MovementHistory } from "./MovementHistory";

/**
 * What is on the shelf, and whether the books agree with the ledger.
 *
 * # The ledger check is on this screen deliberately
 *
 * A projection that has drifted from its movement ledger is invisible until somebody counts —
 * which may be a year. Putting the verifier's answer at the top of the stock screen is what makes
 * it visible before then, and it costs one query on a screen somebody opens anyway.
 */
export function StockScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<StockRow | null>(null);

  const stock = useQuery({ queryKey: ["inventory", "stock"], queryFn: () => stockOnHand("") });
  const check = useQuery({ queryKey: ["inventory", "check"], queryFn: checkLedger });

  if (selected) {
    return <MovementHistory row={selected} onBack={() => setSelected(null)} />;
  }

  if (stock.isPending) return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  if (stock.isError) {
    return <Alert tone="danger" title={t("stock.failed")}>{errorText(stock.error)}</Alert>;
  }

  const term = search.trim().toLowerCase();
  const visible = stock.data.filter(
    (row) =>
      !term ||
      row.productCode.toLowerCase().includes(term) ||
      row.productName.toLowerCase().includes(term) ||
      row.sku.toLowerCase().includes(term),
  );

  // Costs are absent, not zero, for a caller without the permission — so the columns are dropped
  // rather than filled with figures that would read as free stock.
  const showsCost = stock.data.some((row) => row.averageCostMicro !== undefined);

  return (
    <section className="flex flex-col gap-4">
      <header className="flex flex-col gap-1">
        <h2 className="text-base font-medium text-text">{t("stock.title")}</h2>
        <p className="text-sm text-text-muted">{t("stock.help")}</p>
      </header>

      {check.data && !check.data.healthy && (
        <Alert tone="warning" title={t("stock.driftFound")}>
          {t("stock.driftHelp", { count: String(check.data.discrepancies.length) })}
        </Alert>
      )}

      <Input
        label={t("stock.search")}
        value={search}
        onChange={(event) => setSearch(event.target.value)}
      />

      <Table<StockRow>
        caption={t("stock.title")}
        rowKey={(row) => row.variantId}
        rows={visible}
        empty={<EmptyState title={t("stock.none")} />}
        columns={[
          {
            key: "sku",
            header: t("stock.sku"),
            cell: (row) => (
              <button
                type="button"
                className="font-mono text-xs text-accent underline-offset-2 hover:underline"
                onClick={() => setSelected(row)}
              >
                {row.sku}
              </button>
            ),
          },
          { key: "name", header: t("stock.product"), cell: (row) => row.productName },
          {
            key: "onHand",
            header: t("stock.onHand"),
            cell: (row) => (
              <span className="flex items-center gap-2">
                <span className={isNegativeQuantity(row.onHandMicro) ? "text-danger" : undefined}>
                  {formatQuantity(row.onHandMicro)} {row.unit}
                </span>
                {/* Negative stock is a real state that some warehouses permit, and it must LOOK
                    like the exception it is rather than blending into the column. */}
                {isNegativeQuantity(row.onHandMicro) && (
                  <Badge tone="danger">{t("stock.negative")}</Badge>
                )}
              </span>
            ),
          },
          {
            key: "reserved",
            header: t("stock.reserved"),
            cell: (row) =>
              isZeroQuantity(row.reservedMicro) ? "—" : formatQuantity(row.reservedMicro),
          },
          {
            key: "available",
            header: t("stock.available"),
            cell: (row) => formatQuantity(row.availableMicro),
          },
          ...(showsCost
            ? [
                {
                  key: "cost",
                  header: t("stock.averageCost"),
                  cell: (row: StockRow) =>
                    // Six implied decimals on a cost is more than anybody reads; two is what a
                    // price looks like.
                    row.averageCostMicro ? formatQuantity(row.averageCostMicro, 2) : "—",
                },
                {
                  key: "value",
                  header: t("stock.value"),
                  cell: (row: StockRow) =>
                    row.valueMinor ? formatMinor(row.valueMinor) : "—",
                },
              ]
            : []),
        ]}
      />
    </section>
  );
}
