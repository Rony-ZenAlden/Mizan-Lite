import type { MonthReport } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatInteger } from "@/i18n/numbers";
import { Money } from "@/screens/sales/Money";
import { formatDate } from "@/i18n/time";
import { Button } from "@/ui/Button";

/** A month: a row per day that had anything in it, and the month's totals — the sum of its days (Q-L6.8). */
export function MonthTable({ report, onOpenDay }: { report: MonthReport; onOpenDay: (date: string) => void }) {
  const { t, locale } = useLocale();
  const cur = report.localCurrency;
  return (
    <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
      <table className="w-full text-sm" data-testid="month">
        <thead className="bg-surface text-text-muted">
          <tr>
            <th className="p-2 text-start">{t("reports.col.date")}</th>
            <th className="p-2 text-start">{t("reports.col.sales")}</th>
            <th className="p-2 text-start">{t("reports.col.revenue_usd")}</th>
            <th className="p-2 text-start">{t("reports.col.gross_usd")}</th>
            <th className="p-2 text-start">{t("reports.col.net_usd")}</th>
            <th className="p-2 text-start">{t("reports.col.gross_local")}</th>
            <th className="p-2 text-start">{t("reports.col.net_local")}</th>
          </tr>
        </thead>
        <tbody>
          {report.days.length === 0 ? (
            <tr>
              <td colSpan={7} className="p-2 text-text-muted">
                {t("reports.month_empty")}
              </td>
            </tr>
          ) : null}
          {report.days.map((d) => (
            <tr key={d.date} className="border-t border-border">
              <td className="p-2">
                <Button onClick={() => onOpenDay(d.date)}>
                  <bdi dir="ltr">{formatDate(d.date)}</bdi>
                </Button>
              </td>
              <td className="p-2">{formatInteger(d.profit.sales, locale)}</td>
              <td className="p-2">
                <Money value={d.profit.revenueUsd} currency="USD" />
              </td>
              <td className="p-2">
                <Money value={d.profit.profitUsd} currency="USD" />
              </td>
              <td className="p-2">
                <Money value={d.netUsd} currency="USD" />
              </td>
              <td className="p-2">
                <Money value={d.profit.profitLocal} currency={cur} />
              </td>
              <td className="p-2">
                <Money value={d.netLocal} currency={cur} />
              </td>
            </tr>
          ))}
          <tr data-testid="month-total" className="border-t-2 border-border font-semibold">
            <td className="p-2">{t("reports.month_total")}</td>
            <td className="p-2">{formatInteger(report.total.profit.sales, locale)}</td>
            <td className="p-2">
              <Money value={report.total.profit.revenueUsd} currency="USD" />
            </td>
            <td className="p-2">
              <Money value={report.total.profit.profitUsd} currency="USD" />
            </td>
            <td className="p-2">
              <Money value={report.total.netUsd} currency="USD" />
            </td>
            <td className="p-2">
              <Money value={report.total.profit.profitLocal} currency={cur} />
            </td>
            <td className="p-2">
              <Money value={report.total.netLocal} currency={cur} />
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  );
}
