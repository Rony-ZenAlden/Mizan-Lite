import { useCallback, useEffect, useRef, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { Day, Sale, SaleFinding } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatInteger } from "@/i18n/numbers";
import { formatDateTime } from "@/i18n/time";
import { RangeExport } from "@/exports/ExportButtons";
import { useOwner } from "@/owner/OwnerProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { TextField } from "@/ui/Field";
import { Money } from "./Money";
import { ReceiptView } from "./ReceiptView";

/**
 * The day's sales (L4 §10.3): today by default, any earlier day by date; per currency what was charged, what came into
 * the drawer and went out as change, and the voids. Opening a sale shows its receipt, with Void. In owner mode the sales
 * verifier runs, and its findings are shown if it has any. Every figure is Go's.
 */
export function SalesScreen() {
  const client = useClient();
  const { status } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [date, setDate] = useState("");
  const [day, setDay] = useState<Day | null>(null);
  const [open, setOpen] = useState<Sale | null>(null);
  const [findings, setFindings] = useState<SaleFinding[]>([]);
  const [error, setError] = useState<unknown>(null);

  const load = useCallback(
    async (businessDate: string) => {
      try {
        const next = await client.sales.list(businessDate);
        setDay(next);
        setError(null);
      } catch (e) {
        setError(e);
      }
    },
    [client],
  );

  useEffect(() => {
    void load(date);
  }, [date, load]);

  const ownerMode = status.elevatedSeconds > 0;
  const wasOwner = useRef(false);
  useEffect(() => {
    if (ownerMode && !wasOwner.current) {
      client.sales
        .verify()
        .then(setFindings)
        .catch(() => setFindings([]));
    }
    if (!ownerMode) setFindings([]);
    wasOwner.current = ownerMode;
  }, [ownerMode, client]);

  const openSale = async (saleId: string) => {
    try {
      setOpen(await client.sales.receipt(saleId));
    } catch (e) {
      setError(e);
    }
  };

  const voided = (sale: Sale) => {
    setOpen(sale);
    void load(date);
  };

  return (
    <section className="space-y-4">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <h2 className="text-xl font-semibold">{t("sales.title")}</h2>
        <div className="w-48">
          <TextField label={t("sales.date")} type="date" value={date || day?.businessDate || ""} onChange={(e) => setDate(e.target.value)} dir="ltr" />
        </div>
      </header>

      {error ? <Alert tone="danger" title={errorText(error)} /> : null}
      {findings.length > 0 ? (
        <Alert tone="danger" title={t("sales.verify_findings", { count: String(findings.length) })}>
          <ul className="list-disc ps-5">
            {findings.map((f, i) => (
              <li key={`${f.code}-${f.saleId}-${i}`}>
                {tDynamic(f.code)} {f.receiptNo ? t("sales.finding_receipt", { number: String(f.receiptNo) }) : ""}
              </li>
            ))}
          </ul>
        </Alert>
      ) : null}

      {day ? (
        <div className="grid gap-3 sm:grid-cols-2">
          {day.totals.map((total) => (
            <dl
              key={total.currency}
              data-testid={`totals-${total.currency}`}
              className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-1 rounded-lg border border-border bg-surface-raised p-4 text-sm"
            >
              <dt className="col-span-2 font-semibold">{tDynamic(`currency.${total.currency}`)}</dt>
              <dt>{t("sales.totals.charged", { count: formatInteger(total.sales, locale) })}</dt>
              <dd>
                <Money value={total.charged} currency={total.currency} />
              </dd>
              <dt>{t("sales.totals.cash_in")}</dt>
              <dd>
                <Money value={total.cashIn} currency={total.currency} />
              </dd>
              <dt>{t("sales.totals.change_out")}</dt>
              <dd>
                <Money value={total.changeOut} currency={total.currency} />
              </dd>
              <dt>{t("sales.totals.on_credit")}</dt>
              <dd>
                <Money value={total.onCredit} currency={total.currency} />
              </dd>
              <dt>{t("sales.totals.voids", { count: formatInteger(total.voids, locale) })}</dt>
              <dd>
                <Money value={total.voided} currency={total.currency} />
              </dd>
            </dl>
          ))}
        </div>
      ) : null}

      {day && day.sales.length === 0 ? <p className="text-text-muted">{t("sales.empty")}</p> : null}
      {day && day.sales.length > 0 ? (
        <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
          <table className="w-full text-sm">
            <thead className="bg-surface text-text-muted">
              <tr>
                <th className="p-2 text-start">{t("sales.col.number")}</th>
                <th className="p-2 text-start">{t("sales.col.time")}</th>
                <th className="p-2 text-start">{t("sales.col.total")}</th>
                <th className="p-2 text-start">{t("sales.col.tendered")}</th>
                <th className="p-2 text-start">{t("sales.col.change")}</th>
                <th className="p-2 text-start">{t("sales.col.status")}</th>
                <th className="p-2 text-start">{t("sales.col.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {day.sales.map((sale) => (
                <tr key={sale.id} className="border-t border-border">
                  <td className="p-2">{formatInteger(sale.receiptNo, locale)}</td>
                  <td className="p-2">
                    <bdi dir="ltr">{formatDateTime(sale.soldAt)}</bdi>
                  </td>
                  <td className="p-2">
                    <Money value={sale.total} currency={sale.settlement} />
                  </td>
                  <td className="p-2">
                    <Money value={sale.tendered} currency={sale.tenderCurrency} />
                  </td>
                  <td className="p-2">
                    <Money value={sale.change} currency={sale.changeCurrency} />
                  </td>
                  <td className={`p-2 ${sale.status === "voided" ? "text-danger" : ""}`}>
                    {tDynamic(`sales.status.${sale.status}`)}
                    {sale.payment === "credit" ? <span className="block text-xs text-text-muted">{t("sales.on_credit_to", { name: sale.creditCustomerName })}</span> : null}
                  </td>
                  <td className="p-2">
                    <Button onClick={() => void openSale(sale.id)}>{t("sales.open")}</Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      <RangeExport title={t("sales.export")} onExport={(from, to, format) => client.exports.salesHistory(from, to, format)} />

      {open ? <ReceiptView sale={open} onClose={() => setOpen(null)} onVoided={voided} /> : null}
    </section>
  );
}
