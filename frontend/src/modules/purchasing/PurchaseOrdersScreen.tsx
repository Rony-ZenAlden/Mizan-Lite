import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { purchaseOrders, type PurchaseOrder } from "@/lib/wails";
import { Alert, Badge, EmptyState, Input, Select, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "@/modules/accounting/money";
import { PurchaseOrderDetail } from "./PurchaseOrderDetail";

const STATUSES = ["", "draft", "placed", "closed", "cancelled"] as const;

/**
 * What has been ordered, and what has still to arrive.
 *
 * # The column this screen exists for is EXPECTED
 *
 * A list of orders is a record. A list of orders with what is late on it is the report a
 * purchasing manager opens first — and lateness is the whole reason `expected_date` is on the
 * order and indexed.
 */
export function PurchaseOrdersScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const [status, setStatus] = useState<string>("");
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<string | null>(null);

  const orders = useQuery({
    queryKey: ["purchasing", "orders", status],
    queryFn: () => purchaseOrders(status),
  });

  if (selected) {
    return <PurchaseOrderDetail orderId={selected} onBack={() => setSelected(null)} />;
  }

  if (orders.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (orders.isError) {
    return (
      <Alert tone="danger" title={t("purchasing.ordersFailed")}>{errorText(orders.error)}</Alert>
    );
  }

  const term = search.trim().toLowerCase();
  const visible = orders.data.filter(
    (row) =>
      !term ||
      row.number.toLowerCase().includes(term) ||
      row.supplierName.toLowerCase().includes(term),
  );

  // Today, for the "is this late" comparison. A date string comparison, because both sides are
  // ISO dates — parsing them into Date objects would introduce a timezone nobody asked for.
  const today = new Date().toISOString().slice(0, 10);

  return (
    <section className="flex flex-col gap-4">
      <header className="flex flex-col gap-1">
        <h2 className="text-base font-medium text-text">{t("purchasing.orders")}</h2>
        <p className="text-sm text-text-muted">{t("purchasing.ordersHelp")}</p>
      </header>

      <div className="flex flex-wrap items-end gap-3">
        <Input
          label={t("purchasing.search")}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
        <Select
          label={t("purchasing.status")}
          value={status}
          onValueChange={setStatus}
          options={STATUSES.map((code) => ({
            value: code,
            label: code === "" ? t("purchasing.anyStatus") : t(`purchasing.status.${code}`),
          }))}
        />
      </div>

      <Table<PurchaseOrder>
        caption={t("purchasing.orders")}
        rowKey={(row) => row.id}
        rows={visible}
        empty={<EmptyState title={t("purchasing.noOrders")} />}
        columns={[
          {
            key: "number",
            header: t("purchasing.number"),
            cell: (row) => (
              <button
                type="button"
                className="font-mono text-xs text-accent underline-offset-2 hover:underline"
                onClick={() => setSelected(row.id)}
              >
                {/* A draft has no number: §9.4 allocates one at PLACEMENT, so an abandoned
                    order leaves no hole an auditor asks about. */}
                {row.number || t("purchasing.unnumbered")}
              </button>
            ),
          },
          { key: "supplier", header: t("purchasing.supplier"), cell: (row) => row.supplierName },
          { key: "date", header: t("purchasing.orderDate"), cell: (row) => row.orderDate },
          {
            key: "expected",
            header: t("purchasing.expected"),
            cell: (row) => {
              if (!row.expectedDate) return "—";
              // Late is only meaningful for an order still waiting for goods.
              const late = row.status === "placed" && row.expectedDate < today;
              return (
                <span className="flex items-center gap-2">
                  <span className={late ? "text-danger" : undefined}>{row.expectedDate}</span>
                  {late && <Badge tone="danger">{t("purchasing.late")}</Badge>}
                </span>
              );
            },
          },
          {
            key: "status",
            header: t("purchasing.status"),
            cell: (row) => (
              <Badge tone={statusTone(row.status)}>{t(`purchasing.status.${row.status}`)}</Badge>
            ),
          },
          {
            key: "total",
            header: t("purchasing.total"),
            cell: (row) => formatMinor(row.totalMinor),
          },
        ]}
      />
    </section>
  );
}

function statusTone(status: string): "success" | "danger" | "neutral" {
  if (status === "placed") return "success";
  return status === "cancelled" ? "danger" : "neutral";
}
