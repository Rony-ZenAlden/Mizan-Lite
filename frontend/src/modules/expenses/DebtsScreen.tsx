import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { debtPositions, debts, type Debt, type DebtPosition } from "@/lib/wails";
import { Alert, Badge, EmptyState, PageHeader, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { DualAmount } from "@/modules/money/DualAmount";
import { formatMinor } from "@/modules/accounting/money";

/**
 * Loans, advances, and the owner's own money.
 *
 * # Why the positions come first
 *
 * A list of movements answers "what happened". The three positions answer "where do we stand",
 * which is the question somebody has before agreeing to anything — and it is one line each,
 * so putting it below a list of forty movements would bury it.
 */
export function DebtsScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const positions = useQuery({
    queryKey: ["expenses", "debt-positions"],
    queryFn: debtPositions,
  });
  const movements = useQuery({ queryKey: ["expenses", "debts"], queryFn: () => debts("") });

  if (positions.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (positions.isError) {
    return <Alert tone="danger" title={t("debts.failed")}>{errorText(positions.error)}</Alert>;
  }

  return (
    <section className="flex flex-col gap-6">
      <PageHeader title={t("debts.title")} description={t("debts.help")} />

      {/* ── where we stand ───────────────────────────────────────────────────── */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        {positions.data.map((position) => (
          <Position key={position.kind} position={position} />
        ))}
      </div>

      {/* ── what happened ────────────────────────────────────────────────────── */}
      <Table<Debt>
        caption={t("debts.movements")}
        rowKey={(row) => row.id}
        rows={movements.data ?? []}
        empty={<EmptyState title={t("debts.none")} />}
        columns={[
          {
            key: "number",
            header: t("debts.number"),
            cell: (row) => <span className="font-mono text-xs">{row.number}</span>,
          },
          { key: "date", header: t("debts.date"), cell: (row) => row.debtDate },
          {
            key: "counterparty",
            header: t("debts.counterparty"),
            cell: (row) => row.counterpartyName,
          },
          { key: "kind", header: t("debts.kind"), cell: (row) => t(`debts.kind.${row.kind}`) },
          {
            key: "direction",
            header: t("debts.direction"),
            cell: (row) => (
              // In and out read very differently and must not look alike. A list where money
              // leaving and money arriving share a colour is one somebody misreads once.
              <Badge tone={row.direction === "received" ? "success" : "warning"}>
                {t(`debts.direction.${row.direction}`)}
              </Badge>
            ),
          },
          {
            key: "amount",
            header: t("debts.amount"),
            cell: (row) => formatMinor(row.amountMinor),
          },
        ]}
      />
    </section>
  );
}

function Position({ position }: { position: DebtPosition }) {
  const { t } = useTranslation();
  // The sign says which way it stands. A negative loan-payable position means the business has
  // repaid more than it borrowed, which is unusual enough to be worth showing plainly rather
  // than as an absolute value with a word beside it.
  const negative = position.netMinor.startsWith("-");

  return (
    <div className="flex flex-col gap-1 card p-4">
      <span className="text-xs uppercase text-text-muted">
        {t(`debts.kind.${position.kind}`)}
      </span>
      <span
        className={`font-mono text-2xl tabular-nums ${negative ? "text-warning" : "text-text"}`}
      >
        <DualAmount minor={position.netMinor} />
      </span>
      <span className="text-xs text-text-muted">
        {t(negative ? `debts.position.out.${position.kind}` : `debts.position.in.${position.kind}`)}
      </span>
    </div>
  );
}
