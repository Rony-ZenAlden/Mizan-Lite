import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { balanceSheet, profitAndLoss, type StatementNode } from "@/lib/wails";
import { Alert, EmptyState, Input } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "@/modules/accounting/money";

/**
 * The profit and loss, and the balance sheet.
 *
 * # Two statements on one screen
 *
 * They answer different questions — what happened, and where we stand — and a shopkeeper asks
 * both in the same sitting. Splitting them would mean picking the date range twice.
 */
export function StatementsScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const [from, setFrom] = useState(() => firstOfThisMonth());
  const [to, setTo] = useState(() => today());

  const pnl = useQuery({
    queryKey: ["insight", "pnl", from, to],
    queryFn: () => profitAndLoss(from, to),
  });
  const sheet = useQuery({
    queryKey: ["insight", "balance-sheet", to],
    queryFn: () => balanceSheet(to),
  });

  return (
    <section className="flex flex-col gap-6">
      <header className="flex flex-col gap-1">
        <h2 className="text-base font-medium text-text">{t("statements.title")}</h2>
        <p className="text-sm text-text-muted">{t("statements.help")}</p>
      </header>

      <div className="flex flex-wrap gap-3">
        <Input
          type="date"
          label={t("statements.from")}
          value={from}
          onChange={(event) => setFrom(event.target.value)}
        />
        <Input
          type="date"
          label={t("statements.to")}
          value={to}
          onChange={(event) => setTo(event.target.value)}
        />
      </div>

      {/* ── profit and loss ──────────────────────────────────────────────────── */}
      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium text-text">{t("statements.pnl")}</h3>

        {pnl.isError && (
          <Alert tone="danger" title={t("statements.failed")}>{errorText(pnl.error)}</Alert>
        )}
        {pnl.data && (
          <>
            {/*
             * The covered range, shown WHENEVER it differs from what was asked for.
             *
             * A statement is built on per-period balances, so a request for the first fortnight
             * of March is answered with the whole of March. Saying so is the difference between
             * a figure a reader can trust and one that makes their sales look doubled.
             */}
            {(pnl.data.coveredFrom !== pnl.data.requestedFrom ||
              pnl.data.coveredTo !== pnl.data.requestedTo) && (
              <Alert tone="info" title={t("statements.widened")}>
                {t("statements.widenedHelp", {
                  from: pnl.data.coveredFrom || "—",
                  to: pnl.data.coveredTo || "—",
                })}
              </Alert>
            )}

            <StatementSection
              title={t("statements.revenue")}
              nodes={pnl.data.revenue}
              totalMinor={pnl.data.revenueMinor}
            />
            <StatementSection
              title={t("statements.expense")}
              nodes={pnl.data.expense}
              totalMinor={pnl.data.expenseMinor}
            />

            {/*
             * "Net profit", never "profit".
             *
             * The sales analysis reports a GROSS MARGIN, which excludes rent and wages. Labelling
             * either one "profit" is how a shopkeeper concludes they are doing well while losing
             * money — so both are named in full, everywhere they appear.
             */}
            <div className="flex items-center justify-between rounded-lg border border-border bg-surface-muted px-4 py-3">
              <span className="text-sm font-medium text-text">{t("statements.netProfit")}</span>
              <span className="text-lg font-semibold tabular-nums text-text">
                {formatMinor(pnl.data.netProfitMinor)}
              </span>
            </div>
          </>
        )}
      </div>

      {/* ── balance sheet ────────────────────────────────────────────────────── */}
      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium text-text">{t("statements.balanceSheet")}</h3>

        {sheet.isError && (
          <Alert tone="danger" title={t("statements.failed")}>{errorText(sheet.error)}</Alert>
        )}
        {sheet.data && (
          <>
            {/*
             * Out of balance is REPORTED, not hidden.
             *
             * A sheet that silently rendered wrong figures would hide the one fact worth showing.
             * The books being wrong is not a reason to refuse to draw them — it is the reason to.
             */}
            {!sheet.data.balanced && (
              <Alert tone="danger" title={t("statements.outOfBalance")}>
                {t("statements.outOfBalanceHelp", {
                  amount: formatMinor(sheet.data.outOfBalanceMinor),
                })}
              </Alert>
            )}

            <StatementSection
              title={t("statements.asset")}
              nodes={sheet.data.asset}
              totalMinor={sheet.data.assetMinor}
            />
            <StatementSection
              title={t("statements.liability")}
              nodes={sheet.data.liability}
              totalMinor={sheet.data.liabilityMinor}
            />
            <StatementSection
              title={t("statements.equity")}
              nodes={sheet.data.equity}
              totalMinor={sheet.data.equityMinor}
            />

            {/*
             * The unclosed result, shown separately although it is already inside equity.
             *
             * Until the year is closed, the profit so far sits in accounts a balance sheet does
             * not list. A reader who does not see it named cannot reconcile equity with anything.
             */}
            <div className="flex items-center justify-between px-4 text-sm text-text-muted">
              <span>{t("statements.unclosedResult")}</span>
              <span className="tabular-nums">{formatMinor(sheet.data.resultMinor)}</span>
            </div>
          </>
        )}
      </div>
    </section>
  );
}

function StatementSection({
  title,
  nodes,
  totalMinor,
}: {
  title: string;
  nodes: StatementNode[];
  totalMinor: string;
}) {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-1 rounded-lg border border-border bg-surface p-4">
      <div className="flex items-center justify-between pb-2">
        <h4 className="text-sm font-medium text-text">{title}</h4>
        <span className="text-sm font-semibold tabular-nums text-text">
          {formatMinor(totalMinor)}
        </span>
      </div>

      {nodes.length === 0 ? (
        <EmptyState title={t("statements.emptySection")} />
      ) : (
        <ul className="flex flex-col">
          {nodes.map((node) => (
            <StatementRow key={node.accountId} node={node} />
          ))}
        </ul>
      )}
    </div>
  );
}

/**
 * One line, with its children beneath it.
 *
 * Indented by DEPTH rather than by nesting depth in the DOM, because the tree crosses the boundary
 * with its own depths — and rebuilding them from nesting is the one place an off-by-one silently
 * reparents a subtotal.
 */
function StatementRow({ node }: { node: StatementNode }) {
  return (
    <>
      <li
        className="flex items-center justify-between gap-4 py-1 text-sm"
        style={{ paddingInlineStart: `${node.depth * 12}px` }}
      >
        <span className={node.isPostable ? "text-text-muted" : "font-medium text-text"}>
          <span className="font-mono text-xs text-text-muted">{node.code}</span> {node.name}
        </span>
        <span className="tabular-nums">{formatMinor(node.amountMinor)}</span>
      </li>
      {node.children.map((child) => (
        <StatementRow key={child.accountId} node={child} />
      ))}
    </>
  );
}

function today(): string {
  return new Date().toISOString().slice(0, 10);
}

function firstOfThisMonth(): string {
  return `${new Date().toISOString().slice(0, 7)}-01`;
}
