import { useMemo, useState } from "react";
import type { ProductRow, ProductsReport } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal, formatInteger } from "@/i18n/numbers";
import { Money } from "@/screens/sales/Money";
import { SelectField } from "@/ui/Field";

type SortKey = "profit" | "quantity" | "margin";

/** Go's figure as a number for ORDERING rows only — never for arithmetic shown to anyone (DESIGN D9). */
function order(value: string): number {
  const n = Number(value);
  return Number.isFinite(n) ? n : Number.NEGATIVE_INFINITY;
}

/**
 * Per-product profit over a range, sortable, and the one reconciling row — whole-sale discounts and cash rounding — that
 * makes the products add up to the range exactly (L6 §3.4).
 */
export function ProductsTable({ report }: { report: ProductsReport }) {
  const { t, tDynamic, locale } = useLocale();
  const [sort, setSort] = useState<SortKey>("profit");
  const cur = report.localCurrency;
  const rows = useMemo(() => {
    const key = (r: ProductRow) => (sort === "profit" ? order(r.profitUsd) : sort === "quantity" ? order(r.quantity) : order(r.marginUsd));
    return [...report.rows].sort((a, b) => key(b) - key(a));
  }, [report.rows, sort]);
  const name = (r: ProductRow) => (locale === "en" && r.nameEn ? r.nameEn : r.nameAr);

  return (
    <div className="space-y-3">
      <div className="w-56">
        <SelectField label={t("reports.sort")} value={sort} onChange={(e) => setSort(e.target.value as SortKey)}>
          <option value="profit">{t("reports.sort.profit")}</option>
          <option value="quantity">{t("reports.sort.quantity")}</option>
          <option value="margin">{t("reports.sort.margin")}</option>
        </SelectField>
      </div>
      <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
        <table className="w-full text-sm" data-testid="products">
          <thead className="bg-surface text-text-muted">
            <tr>
              <th className="p-2 text-start">{t("reports.col.product")}</th>
              <th className="p-2 text-start">{t("reports.col.quantity")}</th>
              <th className="p-2 text-start">{t("reports.col.revenue_usd")}</th>
              <th className="p-2 text-start">{t("reports.col.cost_usd")}</th>
              <th className="p-2 text-start">{t("reports.col.gross_usd")}</th>
              <th className="p-2 text-start">{t("reports.col.margin")}</th>
              <th className="p-2 text-start">{t("reports.col.gross_local")}</th>
              <th className="p-2 text-start">{t("reports.col.no_cost")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr>
                <td colSpan={8} className="p-2 text-text-muted">
                  {t("reports.products_empty")}
                </td>
              </tr>
            ) : null}
            {rows.map((r) => (
              <tr key={r.productId} className="border-t border-border">
                <td className="p-2">{name(r)}</td>
                <td className="p-2">
                  <bdi dir="ltr">
                    {formatDecimal(r.quantity, locale)} {tDynamic(`uom.${r.unitCode}`)}
                  </bdi>
                </td>
                <td className="p-2">
                  <Money value={r.revenueUsd} currency="USD" />
                </td>
                <td className="p-2">
                  <Money value={r.costUsd} currency="USD" />
                </td>
                <td className="p-2">
                  <Money value={r.profitUsd} currency="USD" />
                </td>
                <td className="p-2">{r.marginUsd ? t("reports.margin", { margin: formatDecimal(r.marginUsd, locale) }) : ""}</td>
                <td className="p-2">
                  <Money value={r.profitLocal} currency={cur} />
                </td>
                <td className={`p-2 ${r.openLines > 0 ? "text-text-muted" : "text-danger"}`}>
                  {r.openLines > 0
                    ? t("reports.open_item_lines", { count: formatInteger(r.openLines, locale), usd: formatDecimal(r.openUsd, locale) })
                    : r.unknownLines > 0
                      ? t("reports.no_cost_lines", { count: formatInteger(r.unknownLines, locale), usd: formatDecimal(r.unknownUsd, locale) })
                      : ""}
                </td>
              </tr>
            ))}
            <tr data-testid="reconciling" className="border-t border-border text-text-muted">
              <td className="p-2" colSpan={2}>
                {t("reports.reconciling")}
              </td>
              <td className="p-2">
                <Money value={report.discountUsd} currency="USD" />
              </td>
              <td className="p-2" colSpan={3} />
              <td className="p-2" colSpan={2}>
                {t("reports.reconciling_local", {
                  discount: formatDecimal(report.discountLocal, locale),
                  rounding: formatDecimal(report.roundingLocal, locale),
                  currency: tDynamic(`currency.short.${cur}`),
                })}
              </td>
            </tr>
            <tr data-testid="products-total" className="border-t-2 border-border font-semibold">
              <td className="p-2" colSpan={2}>
                {t("reports.products_total", { count: formatInteger(report.total.sales, locale) })}
              </td>
              <td className="p-2">
                <Money value={report.total.revenueUsd} currency="USD" />
              </td>
              <td className="p-2">
                <Money value={report.total.costUsd} currency="USD" />
              </td>
              <td className="p-2">
                <Money value={report.total.profitUsd} currency="USD" />
              </td>
              <td className="p-2">{report.total.marginUsd ? t("reports.margin", { margin: formatDecimal(report.total.marginUsd, locale) }) : ""}</td>
              <td className="p-2">
                <Money value={report.total.profitLocal} currency={cur} />
              </td>
              <td className="p-2" />
            </tr>
          </tbody>
        </table>
      </div>
      <p className="text-xs text-text-muted">{t("reports.products_hint")}</p>
    </div>
  );
}
