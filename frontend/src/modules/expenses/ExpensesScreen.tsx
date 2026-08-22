import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { NewExpenseForm } from "./NewExpenseForm";
import { Can } from "@/app/session/Can";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { PERMISSIONS, expenses, unsettledExpenses, type Expense } from "@/lib/wails";
import { Alert, Badge, Button, EmptyState, Input, PageHeader, Select, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor, isZeroMinor } from "@/modules/accounting/money";

const STATUSES = ["", "draft", "posted", "cancelled"] as const;

/**
 * What the business spends, and what of it is still owed.
 *
 * # The two lists are one screen for the same reason bills and GRNI are
 *
 * "What did we spend" is a record. "What do we still owe" is the list somebody acts on before
 * the end of the week — and separated, the second is never opened.
 */
export function ExpensesScreen() {
  const [adding, setAdding] = useState(false);
  const { t } = useTranslation();
  const errorText = useErrorText();

  const [status, setStatus] = useState<string>("");
  const [search, setSearch] = useState("");

  const all = useQuery({
    queryKey: ["expenses", "list", status],
    queryFn: () => expenses(status),
  });
  const owed = useQuery({
    queryKey: ["expenses", "unsettled"],
    queryFn: unsettledExpenses,
  });

  if (all.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (all.isError) {
    return <Alert tone="danger" title={t("expenses.failed")}>{errorText(all.error)}</Alert>;
  }

  const term = search.trim().toLowerCase();
  const visible = all.data.filter(
    (row) =>
      !term ||
      row.payeeName.toLowerCase().includes(term) ||
      row.number.toLowerCase().includes(term) ||
      row.reference.toLowerCase().includes(term),
  );

  return (
    <section className="flex flex-col gap-6">
      <div className="flex flex-col gap-4">
        <PageHeader
        title={t("expenses.title")}
        description={t("expenses.help")}
        actions={
          <Can permission={PERMISSIONS.expenseDraft}>
            <Button onClick={() => setAdding((open) => !open)}>
              {adding ? t("catalog.done") : t("expenses.new")}
            </Button>
          </Can>
        }
      />

      {adding ? <NewExpenseForm onRecorded={() => undefined} /> : null}

        <div className="flex flex-wrap items-end gap-3">
          <Input
            label={t("expenses.search")}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          <Select
            label={t("expenses.status")}
            value={status}
            onValueChange={setStatus}
            options={STATUSES.map((code) => ({
              value: code,
              label: code === "" ? t("expenses.anyStatus") : t(`expenses.status.${code}`),
            }))}
          />
        </div>

        <Table<Expense>
          caption={t("expenses.title")}
          rowKey={(row) => row.id}
          rows={visible}
          empty={<EmptyState title={t("expenses.none")} />}
          columns={[
            {
              key: "number",
              header: t("expenses.number"),
              cell: (row) => (
                <span className="font-mono text-xs">
                  {row.number || t("expenses.unnumbered")}
                </span>
              ),
            },
            { key: "date", header: t("expenses.date"), cell: (row) => row.expenseDate },
            { key: "payee", header: t("expenses.payee"), cell: (row) => row.payeeName },
            {
              key: "settlement",
              header: t("expenses.settlement"),
              cell: (row) => (
                // Paid now versus owed is the distinction the whole document turns on, and it is
                // the first thing somebody scanning this list is looking for.
                <Badge tone={row.settlement === "immediate" ? "success" : "warning"}>
                  {t(`expenses.settlement.${row.settlement}`)}
                </Badge>
              ),
            },
            {
              key: "total",
              header: t("expenses.total"),
              cell: (row) => formatMinor(row.totalMinor),
            },
            {
              key: "outstanding",
              header: t("expenses.outstanding"),
              cell: (row) => {
                // Absent, not zero. An expense paid on the spot owes nothing and never did —
                // showing "0.00" would read as a debt that was settled.
                if (!row.outstandingMinor) return "—";
                return isZeroMinor(row.outstandingMinor) ? (
                  <Badge tone="success">{t("expenses.settled")}</Badge>
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

      {/* ── what is still to pay ─────────────────────────────────────────────── */}
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <h3 className="text-sm font-medium text-text">{t("expenses.owed")}</h3>
          <p className="text-xs text-text-muted">{t("expenses.owedHelp")}</p>
        </div>

        <Table<Expense>
          caption={t("expenses.owed")}
          rowKey={(row) => row.id}
          rows={owed.data ?? []}
          empty={<EmptyState title={t("expenses.owedNone")} />}
          columns={[
            {
              key: "due",
              header: t("expenses.due"),
              // Oldest due first, which is the order the service returns them in and the order a
              // week's payments get decided in.
              cell: (row) => row.dueDate || row.expenseDate,
            },
            { key: "payee", header: t("expenses.payee"), cell: (row) => row.payeeName },
            {
              key: "reference",
              header: t("expenses.reference"),
              cell: (row) => row.reference || "—",
            },
            {
              key: "outstanding",
              header: t("expenses.outstanding"),
              cell: (row) => (
                <span className="font-mono tabular-nums">
                  {formatMinor(row.outstandingMinor)}
                </span>
              ),
            },
          ]}
        />
      </div>
    </section>
  );
}
