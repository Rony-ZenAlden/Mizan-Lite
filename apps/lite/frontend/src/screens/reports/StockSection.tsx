import type { StockReport } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { useCurrencies } from "@/rates/useCurrencies";
import { formatDecimal, formatInteger } from "@/i18n/numbers";
import { Money, isZero } from "@/screens/sales/Money";
import { formatDate } from "@/i18n/time";
import { Alert } from "@/ui/Alert";

type Named = { nameAr: string; nameEn: string };

/**
 * The stock on a date and what it was worth, the period's movements at cost reconciled from opening to closing value with
 * any difference named, and the profit waiting on the shelf with what it leaves out (L6 §6).
 */
export function StockSection({ report }: { report: StockReport }) {
  const { t, tDynamic, locale } = useLocale();
  // A dollars-only shop reads its stock in dollars alone (0.10.0).
  const { usdOnly } = useCurrencies();
  const cur = report.localCurrency;
  const name = (p: Named) => (locale === "en" && p.nameEn ? p.nameEn : p.nameAr);
  const c = report.reconciliation;
  const line = (key: Parameters<typeof t>[0], value: string, testId?: string, strong = false) => (
    <>
      <dt className={strong ? "font-semibold" : ""}>{t(key)}</dt>
      <dd data-testid={testId} className={strong ? "font-semibold" : ""}>
        <Money value={value} currency="USD" />
      </dd>
    </>
  );
  const differs = !isZero(c.negativeStock) || !isZero(c.rounding);

  return (
    <div className="space-y-6">
      <section className="space-y-2">
        <h3 className="font-semibold">{t("reports.stock.value_on", { date: formatDate(report.to) })}</h3>
        <p data-testid="stock-total">
          <Money value={report.totalUsd} currency="USD" />
          {usdOnly ? null : " · "}
          {usdOnly ? null : report.valueRate ? (
            t("reports.stock.local_at", { value: formatDecimal(report.totalLocal, locale), currency: tDynamic(`currency.short.${cur}`), rate: formatDecimal(report.valueRate, locale) })
          ) : (
            t("reports.stock.no_rate")
          )}
        </p>
        {/* The same stock at its selling prices, beside what it cost (the owner's request, 2026-09-17). */}
        {report.retailTotalUsd ? (
          <p className="text-sm text-text-muted" data-testid="stock-retail">
            {t("reports.retail_value")}: <Money value={report.retailTotalUsd} currency="USD" />
            {report.valueRate && !usdOnly ? (
              <>
                {" · "}
                <bdi dir="ltr">{formatDecimal(report.retailTotalLocal, locale)}</bdi> {tDynamic(`currency.short.${cur}`)}
              </>
            ) : null}
          </p>
        ) : null}
        <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
          <table className="w-full text-sm">
            <thead className="bg-surface text-text-muted">
              <tr>
                <th className="p-2 text-start">{t("reports.col.product")}</th>
                <th className="p-2 text-start">{t("reports.col.on_hand")}</th>
                <th className="p-2 text-start">{t("reports.col.average_cost")}</th>
                <th className="p-2 text-start">{t("reports.col.value_usd")}</th>
                {usdOnly ? null : <th className="p-2 text-start">{t("reports.col.value_local")}</th>}
              </tr>
            </thead>
            <tbody>
              {report.lines.map((l) => (
                <tr key={l.productId} className="border-t border-border">
                  <td className="p-2">{name(l)}</td>
                  <td className="p-2">
                    <bdi dir="ltr">
                      {formatDecimal(l.onHand, locale)} {tDynamic(`uom.${l.unitCode}`)}
                    </bdi>
                  </td>
                  <td className="p-2">
                    <Money value={l.averageCost} currency="USD" />
                  </td>
                  <td className="p-2">
                    <Money value={l.valueUsd} currency="USD" />
                  </td>
                  {usdOnly ? null : (
                    <td className="p-2">
                      <Money value={l.valueLocal} currency={cur} />
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {report.belowZero.length > 0 ? (
          <Alert tone="danger" title={t("reports.stock.below_zero", { count: formatInteger(report.belowZero.length, locale) })}>
            {report.belowZero.map(name).join("، ")}
          </Alert>
        ) : null}
        {report.unknownCost.length > 0 ? (
          <Alert tone="danger" title={t("reports.stock.unknown_cost", { count: formatInteger(report.unknownCost.length, locale) })}>
            {report.unknownCost.map(name).join("، ")}
          </Alert>
        ) : null}
      </section>

      <section className="space-y-2">
        <h3 className="font-semibold">{t("reports.stock.movements", { from: formatDate(c.from), to: formatDate(c.to) })}</h3>
        <dl data-testid="reconciliation" className="grid max-w-md grid-cols-[1fr_auto] gap-x-4 gap-y-1 rounded-lg border border-border bg-surface-raised p-4 text-sm">
          {line("reports.stock.opening", c.opening)}
          {line("reports.stock.received", c.received)}
          {line("reports.stock.sold", c.sold)}
          {line("reports.stock.losses", c.losses)}
          {line("reports.stock.gains", c.gains)}
          {line("reports.stock.revaluation", c.revaluation)}
          {line("reports.stock.packages", c.packages)}
          {!isZero(c.negativeStock) ? line("reports.stock.negative", c.negativeStock, "negative-stock") : null}
          {!isZero(c.rounding) ? line("reports.stock.rounding", c.rounding, "rounding") : null}
          {line("reports.stock.closing", c.closing, "closing", true)}
        </dl>
        <p className="text-xs text-text-muted">{differs ? t("reports.stock.differs") : t("reports.stock.balanced")}</p>
      </section>

      <section className="space-y-2">
        <h3 className="font-semibold">{t("reports.shelf.title")}</h3>
        <p data-testid="shelf-total">
          <Money value={report.shelfTotalUsd} currency="USD" />
          {usdOnly ? null : " · "}
          {usdOnly ? null : report.shelfRate ? t("reports.stock.local_at", { value: formatDecimal(report.shelfTotalLocal, locale), currency: tDynamic(`currency.short.${cur}`), rate: formatDecimal(report.shelfRate, locale) }) : t("reports.stock.no_rate")}
        </p>
        {report.belowCost > 0 ? <Alert tone="danger" title={t("reports.shelf.below_cost", { count: formatInteger(report.belowCost, locale) })} /> : null}
        <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
          <table className="w-full text-sm">
            <thead className="bg-surface text-text-muted">
              <tr>
                <th className="p-2 text-start">{t("reports.col.product")}</th>
                <th className="p-2 text-start">{t("reports.col.on_hand")}</th>
                <th className="p-2 text-start">{t("reports.col.average_cost")}</th>
                <th className="p-2 text-start">{t("reports.col.price")}</th>
                <th className="p-2 text-start">{t("reports.col.expected_usd")}</th>
                {usdOnly ? null : <th className="p-2 text-start">{t("reports.col.expected_local")}</th>}
              </tr>
            </thead>
            <tbody>
              {report.shelf.map((l) => (
                <tr key={l.productId} className={`border-t border-border ${l.belowCost ? "text-danger" : ""}`}>
                  <td className="p-2">
                    {name(l)}
                    {l.belowCost ? <span className="block text-xs">{t("reports.shelf.priced_below_cost")}</span> : null}
                  </td>
                  <td className="p-2">
                    <bdi dir="ltr">
                      {formatDecimal(l.onHand, locale)} {tDynamic(`uom.${l.unitCode}`)}
                    </bdi>
                  </td>
                  <td className="p-2">
                    <Money value={l.averageCost} currency="USD" />
                  </td>
                  <td className="p-2">
                    <Money value={l.price} currency={l.priceCurrency} />
                  </td>
                  <td className="p-2">
                    <Money value={l.profitUsd} currency="USD" />
                  </td>
                  {usdOnly ? null : (
                    <td className="p-2">
                      <Money value={l.profitLocal} currency={cur} />
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {report.leftOut.length > 0 ? (
          <details data-testid="shelf-excluded" className="text-sm">
            <summary>{t("reports.shelf.left_out", { count: formatInteger(report.leftOut.length, locale) })}</summary>
            <ul className="list-disc ps-5">
              {report.leftOut.map((l) => (
                <li key={l.productId}>
                  {name(l)} — {tDynamic(`reports.left_out.${l.reason}`)}
                </li>
              ))}
            </ul>
          </details>
        ) : null}
      </section>
    </div>
  );
}
