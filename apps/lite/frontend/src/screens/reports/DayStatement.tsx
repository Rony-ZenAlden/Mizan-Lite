import type { Amount, DayReport, Takings } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { useCurrencies } from "@/rates/useCurrencies";
import { formatDecimal, formatInteger } from "@/i18n/numbers";
import { Money, isZero } from "@/screens/sales/Money";
import { Alert } from "@/ui/Alert";

/** A row of the statement: a label, the dollar reading and the pounds reading. */
function Row({ label, usd, local, localCurrency, strong = false, sub = false, testId }: {
  label: string;
  usd: string;
  local: string;
  localCurrency: string;
  strong?: boolean;
  sub?: boolean;
  testId?: string;
}) {
  // A dollars-only shop reads its statement in dollars alone (0.10.0).
  const { usdOnly } = useCurrencies();
  return (
    <tr data-testid={testId} className={`border-t border-border ${strong ? "font-semibold" : ""}`}>
      <th scope="row" className={`p-2 text-start font-normal ${sub ? "ps-6 text-text-muted" : ""} ${strong ? "font-semibold" : ""}`}>
        {label}
      </th>
      <td className="p-2">
        <Money value={usd} currency="USD" />
      </td>
      {usdOnly ? null : (
        <td className="p-2">
          <Money value={local} currency={localCurrency} />
        </td>
      )}
    </tr>
  );
}

/**
 * A day's (or a range's) statement read top to bottom (L6 §10.2): revenue, cost, gross profit, losses, bad debts,
 * expenses, net profit — in dollars, and in pounds at each sale's own rate (Q-L6.6). Every figure is Go's.
 */
export function DayStatement({ report }: { report: DayReport }) {
  const { t, tDynamic, locale } = useLocale();
  const { usdOnly } = useCurrencies();
  const p = report.profit;
  const cur = report.localCurrency;
  const amountRow = (key: Parameters<typeof t>[0], a: Amount, sub = true) =>
    isZero(a.usd) && isZero(a.local) ? null : <Row key={key} label={t(key)} usd={a.usd} local={a.local} localCurrency={cur} sub={sub} />;
  const margin = (m: string) => (m ? t("reports.margin", { margin: formatDecimal(m, locale) }) : "");

  return (
    <div className="space-y-4">
      {p.unknownLines > 0 ? (
        <Alert
          tone="danger"
          title={
            usdOnly
              ? t("reports.unknown_cost_usd", { count: formatInteger(p.unknownLines, locale), usd: formatDecimal(p.unknownUsd, locale) })
              : t("reports.unknown_cost", {
                  count: formatInteger(p.unknownLines, locale),
                  usd: formatDecimal(p.unknownUsd, locale),
                  local: formatDecimal(p.unknownLocal, locale),
                  currency: tDynamic(`currency.short.${cur}`),
                })
          }
        >
          {t("reports.unknown_cost_hint")}
        </Alert>
      ) : null}
      {report.unconverted > 0 ? <Alert tone="danger" title={t("reports.unconverted", { count: formatInteger(report.unconverted, locale) })} /> : null}
      {p.openLines > 0 ? (
        // Not a warning: an open-priced item has no cost by design (2026-09-23). Said, so the day still adds up to the drawer.
        <p className="rounded-md border border-border bg-surface-raised p-3 text-sm" data-testid="open-items">
          {usdOnly
            ? t("reports.open_items_usd", { count: formatInteger(p.openLines, locale), usd: formatDecimal(p.openUsd, locale) })
            : t("reports.open_items", {
                count: formatInteger(p.openLines, locale),
                usd: formatDecimal(p.openUsd, locale),
                local: formatDecimal(p.openLocal, locale),
                currency: tDynamic(`currency.short.${cur}`),
              })}
        </p>
      ) : null}

      <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
        <table className="w-full text-sm" data-testid="statement">
          <thead className="bg-surface text-text-muted">
            <tr>
              <th className="p-2 text-start">{t("reports.col.line")}</th>
              <th className="p-2 text-start">{t("reports.col.usd")}</th>
              {usdOnly ? null : <th className="p-2 text-start">{t("reports.col.local")}</th>}
            </tr>
          </thead>
          <tbody>
            <Row label={t("reports.sales_count", { count: formatInteger(p.sales, locale) })} usd={p.revenueUsd} local={p.revenueLocal} localCurrency={cur} testId="revenue" />
            {!isZero(p.discountUsd) || !isZero(p.discountLocal) ? (
              <Row label={t("reports.sale_discounts")} usd={p.discountUsd} local={p.discountLocal} localCurrency={cur} sub />
            ) : null}
            {!isZero(p.roundingLocal) && !usdOnly ? <Row label={t("reports.rounding")} usd="0.00" local={p.roundingLocal} localCurrency={cur} sub /> : null}
            <Row label={t("reports.cost")} usd={p.costUsd} local={p.costLocal} localCurrency={cur} />
            <Row label={t("reports.gross_profit")} usd={p.profitUsd} local={p.profitLocal} localCurrency={cur} strong testId="gross" />
            {p.marginUsd || p.marginLocal ? (
              <tr className="text-text-muted">
                <th scope="row" className="p-2 ps-6 text-start font-normal">
                  {t("reports.col.margin")}
                </th>
                <td className="p-2">{margin(p.marginUsd)}</td>
                {usdOnly ? null : <td className="p-2">{margin(p.marginLocal)}</td>}
              </tr>
            ) : null}
            <Row label={t("reports.losses")} usd={report.losses.out.usd} local={report.losses.out.local} localCurrency={cur} testId="losses" />
            {amountRow("reports.losses.spoiled", report.losses.spoiled)}
            {amountRow("reports.losses.own_use", report.losses.ownUse)}
            {amountRow("reports.losses.other", report.losses.other)}
            {amountRow("reports.losses.shortfall", report.losses.shortfall)}
            {amountRow("reports.losses.surplus", report.losses.surplus, false)}
            <Row label={t("reports.bad_debts")} usd={report.badDebts.usd} local={report.badDebts.local} localCurrency={cur} />
            {/* What came back over the counter. The profit given up is the MARGIN, not the refund: the goods went
                back on the shelf, so the shop is out the margin and not the price (2026-09-20). */}
            {report.returns.count > 0 ? (
              <>
                <Row
                  label={t("zreport.returns")}
                  usd={report.returns.profit.usd}
                  local={report.returns.profit.local}
                  localCurrency={cur}
                  testId="returns"
                />
                <Row label={t("zreport.returns_refund")} usd={report.returns.refund.usd} local={report.returns.refund.local} localCurrency={cur} sub />
              </>
            ) : null}
            <Row label={t("reports.expenses")} usd={report.expenses.usd} local={report.expenses.local} localCurrency={cur} testId="expenses" />
            {/* The day's small change apart from rent and the bills: a day the rent was paid is not a bad day. */}
            <Row label={t("zreport.expenses_daily")} usd={report.dailyExpenses.usd} local={report.dailyExpenses.local} localCurrency={cur} sub />
            <Row label={t("zreport.expenses_periodic")} usd={report.periodicExpenses.usd} local={report.periodicExpenses.local} localCurrency={cur} sub testId="periodic-expenses" />
            {report.categories.map((c) => (
              <Row key={c.category} label={tDynamic(`cash.category.${c.category}`)} usd={c.amount.usd} local={c.amount.local} localCurrency={cur} sub />
            ))}
            <Row label={t("reports.net_profit")} usd={report.netUsd} local={report.netLocal} localCurrency={cur} strong testId="net" />
          </tbody>
        </table>
      </div>
      {/* The note says at what rates the pound column was converted; a dollars-only shop has none (0.10.1). */}
      {usdOnly ? null : (
        <p className="text-xs text-text-muted" data-testid="rate-note">
          {report.rate ? t("reports.rate_note", { rate: formatDecimal(report.rate, locale) }) : t("reports.rate_note_range")}
        </p>
      )}

      <h3 className="font-semibold">{t("reports.takings")}</h3>
      <div className="grid gap-3 sm:grid-cols-2">
        {report.takings.map((k) => (
          <TakingsCard key={k.currency} takings={k} />
        ))}
      </div>
    </div>
  );
}

function TakingsCard({ takings: k }: { takings: Takings }) {
  const { t, tDynamic, locale } = useLocale();
  const line = (label: string, value: string) => (
    <>
      <dt>{label}</dt>
      <dd>
        <Money value={value} currency={k.currency} />
      </dd>
    </>
  );
  return (
    <dl data-testid={`takings-${k.currency}`} className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-1 rounded-lg border border-border bg-surface-raised p-4 text-sm">
      <dt className="col-span-2 font-semibold">{tDynamic(`currency.${k.currency}`)}</dt>
      {line(t("reports.takings.sales", { count: formatInteger(k.sales, locale) }), k.charged)}
      {line(t("reports.takings.credit", { count: formatInteger(k.creditSales, locale) }), k.credit)}
      {line(t("reports.takings.discounts"), k.discounts)}
      {line(t("reports.takings.rounding"), k.rounding)}
      {line(t("reports.takings.voids", { count: formatInteger(k.voids, locale) }), k.voided)}
      {line(t("reports.takings.collected"), k.collected)}
      {line(t("reports.takings.refunded"), k.refunded)}
      {line(t("reports.takings.written_off"), k.writtenOff)}
    </dl>
  );
}
