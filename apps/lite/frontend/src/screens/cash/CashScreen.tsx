import { useCallback, useEffect, useRef, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { CashEntry, Drawer, DrawerCurrency } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal } from "@/i18n/numbers";
import { formatDate, formatDateTime } from "@/i18n/time";
import { ExportButtons } from "@/exports/ExportButtons";
import { useOwner } from "@/owner/OwnerProvider";
import { Money, isZero } from "@/screens/sales/Money";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { TextField } from "@/ui/Field";
import { CountDialog, MoneyDialog, ReverseCashDialog, type MoneyKind } from "./CashDialogs";

type Term = { key: Parameters<ReturnType<typeof useLocale>["t"]>[0]; value: string; out: boolean };

/**
 * The cash drawer (L6 §7, §10.2): for a day, per currency, what should be in it and how that was made up — every figure
 * money that moved in that currency, nothing converted — what was counted and the difference. The count is the counter's;
 * expenses, withdrawals, deposits and reversals are the owner's.
 */
export function CashScreen() {
  const client = useClient();
  const { status } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [date, setDate] = useState("");
  const [drawer, setDrawer] = useState<Drawer | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [counting, setCounting] = useState<DrawerCurrency | null>(null);
  const [money, setMoney] = useState<MoneyKind | null>(null);
  const [reversing, setReversing] = useState<CashEntry | null>(null);
  const [saved, setSaved] = useState<CashEntry | null>(null);

  const load = useCallback(async () => {
    try {
      setDrawer(await client.cash.drawer(date));
      setError(null);
    } catch (e) {
      setError(e);
    }
  }, [client, date]);

  useEffect(() => {
    void load();
  }, [load]);

  // Owner mode starting or ending changes what the drawer may show: read it again.
  const ownerMode = status.elevatedSeconds > 0;
  const wasOwner = useRef(ownerMode);
  useEffect(() => {
    if (wasOwner.current !== ownerMode) void load();
    wasOwner.current = ownerMode;
  }, [ownerMode, load]);

  const done = (entry: CashEntry) => {
    setCounting(null);
    setMoney(null);
    setReversing(null);
    setSaved(entry);
    void load();
  };

  const isToday = drawer !== null && drawer.date === drawer.today;
  // A day either side, and back to today, without opening the date picker: a drawer is read day by day (Q-L8.15).
  const shown = date || drawer?.date || "";
  const shift = (days: number) => {
    if (!shown) return;
    const at = new Date(`${shown}T12:00:00Z`); // midday, so no time zone can move the date across midnight
    at.setUTCDate(at.getUTCDate() + days);
    setDate(at.toISOString().slice(0, 10));
  };

  return (
    <section className="space-y-4">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <h2 className="text-xl font-semibold">{t("cash.title")}</h2>
        <div className="flex items-end gap-2">
          <Button onClick={() => shift(-1)} disabled={!shown}>
            {t("cash.day.previous")}
          </Button>
          <div className="w-44">
            <TextField label={t("cash.date")} type="date" value={shown} onChange={(e) => setDate(e.target.value)} dir="ltr" />
          </div>
          <Button onClick={() => shift(1)} disabled={!shown || isToday}>
            {t("cash.day.next")}
          </Button>
          <Button onClick={() => setDate(drawer?.today ?? "")} disabled={isToday}>
            {t("cash.day.today")}
          </Button>
        </div>
      </header>

      {drawer ? <ExportButtons onExport={(format) => client.exports.report({ kind: "drawer", date: date || drawer.date, month: "", from: "", to: "", format })} /> : null}
      {error ? <Alert tone="danger" title={errorText(error)} /> : null}
      {saved ? (
        <Alert
          tone="success"
          title={
            saved.kind === "count"
              ? t("cash.count.saved", { difference: formatDecimal(saved.difference, locale), currency: tDynamic(`currency.short.${saved.currency}`) })
              : t("cash.saved")
          }
        />
      ) : null}

      {drawer ? (
        <>
          <div className="grid gap-3 lg:grid-cols-2">
            {drawer.currencies.map((c) => (
              <DrawerCard key={c.currency} terms={c} ownerView={drawer.ownerView} onCount={isToday ? () => setCounting(c) : undefined} />
            ))}
          </div>
          {isToday ? (
            <div className="flex flex-wrap gap-2">
              <Button onClick={() => setMoney("expense")}>{t("cash.expense.action")}</Button>
              <Button onClick={() => setMoney("withdrawal")}>{t("cash.withdrawal.action")}</Button>
              <Button onClick={() => setMoney("deposit")}>{t("cash.deposit.action")}</Button>
            </div>
          ) : (
            <p className="text-sm text-text-muted">{t("cash.past_day")}</p>
          )}

          <h3 className="font-semibold">{t("cash.book")}</h3>
          {!drawer.ownerView ? <p className="text-xs text-text-muted">{t("cash.book_owner_hint")}</p> : null}
          {drawer.entries.length === 0 ? (
            <p className="text-text-muted">{t("cash.book_empty")}</p>
          ) : (
            <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
              <table className="w-full text-sm" data-testid="cash-book">
                <thead className="bg-surface text-text-muted">
                  <tr>
                    <th className="p-2 text-start">{t("cash.col.time")}</th>
                    <th className="p-2 text-start">{t("cash.col.kind")}</th>
                    <th className="p-2 text-start">{t("cash.col.amount")}</th>
                    <th className="p-2 text-start">{t("cash.col.detail")}</th>
                    <th className="p-2 text-start">{t("cash.col.actions")}</th>
                  </tr>
                </thead>
                <tbody>
                  {drawer.entries.map((e) => (
                    <tr key={e.id} className={`border-t border-border ${e.reversed ? "text-text-muted line-through" : ""}`}>
                      <td className="p-2">
                        <bdi dir="ltr">{formatDateTime(e.occurredAt)}</bdi>
                      </td>
                      <td className="p-2">
                        {tDynamic(`cash.kind.${e.kind}`)}
                        {e.reversesKind ? <span className="block text-xs">{tDynamic(`cash.kind.${e.reversesKind}`)}</span> : null}
                      </td>
                      <td className="p-2">
                        <Money value={e.amount} currency={e.currency} />
                      </td>
                      <td className="p-2">
                        {e.kind === "count" ? t("cash.count.detail", { expected: formatDecimal(e.expected, locale), difference: formatDecimal(e.difference, locale) }) : null}
                        {e.category ? tDynamic(`cash.category.${e.category}`) : null}
                        {e.kind === "expense" ? <span className="block text-xs">{e.fromDrawer ? t("cash.paid_from_drawer") : t("cash.paid_elsewhere")}</span> : null}
                        {e.note ? <span className="block text-xs">{e.note}</span> : null}
                      </td>
                      <td className="p-2">{e.reversible ? <Button onClick={() => setReversing(e)}>{t("cash.reverse.action")}</Button> : null}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      ) : null}

      {counting ? <CountDialog currency={counting.currency} expected={formatDecimal(counting.expected, locale)} onDone={done} onClose={() => setCounting(null)} /> : null}
      {money && drawer ? <MoneyDialog kind={money} categories={drawer.categories} onDone={done} onClose={() => setMoney(null)} /> : null}
      {reversing ? <ReverseCashDialog entry={reversing} onDone={done} onClose={() => setReversing(null)} /> : null}
    </section>
  );
}

function DrawerCard({ terms: c, ownerView, onCount }: { terms: DrawerCurrency; ownerView: boolean; onCount?: () => void }) {
  const { t, tDynamic, locale } = useLocale();
  const terms: Term[] = [
    { key: "cash.term.cash_sales", value: c.cashSalesIn, out: false },
    { key: "cash.term.credit_paid", value: c.creditPaidIn, out: false },
    { key: "cash.term.repayments", value: c.repaymentsIn, out: false },
    { key: "cash.term.deposits", value: c.depositsIn, out: false },
    { key: "cash.term.suppliers_in", value: c.suppliersIn, out: false },
    { key: "cash.term.change", value: c.changeOut, out: true },
    { key: "cash.term.refunds", value: c.refundsOut, out: true },
    { key: "cash.term.void_returns", value: c.voidReturns, out: true },
    { key: "cash.term.returns", value: c.returnsOut, out: true },
    { key: "cash.term.suppliers_out", value: c.suppliersOut, out: true },
    { key: "cash.term.expenses", value: c.expensesOut, out: true },
    { key: ownerView ? "cash.term.withdrawals" : "cash.term.taken_out", value: c.withdrawalsOut, out: true },
  ];
  return (
    <div data-testid={`drawer-${c.currency}`} className="space-y-2 rounded-lg border border-border bg-surface-raised p-4 text-sm">
      <h3 className="font-semibold">{tDynamic(`currency.${c.currency}`)}</h3>
      <dl className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-1">
        <dt>{c.openingCountDate ? t("cash.opening_counted", { date: formatDate(c.openingCountDate) }) : t("cash.opening_never")}</dt>
        <dd>
          <Money value={c.opening} currency={c.currency} />
        </dd>
        {terms
          .filter((term) => !isZero(term.value))
          .map((term) => (
            <div key={term.key} className="contents">
              <dt className="text-text-muted">{t(term.out ? "cash.less" : "cash.plus", { term: t(term.key) })}</dt>
              <dd>
                <Money value={term.value} currency={c.currency} />
              </dd>
            </div>
          ))}
        <dt className="font-semibold">{t("cash.expected")}</dt>
        <dd className="font-semibold" data-testid={`expected-${c.currency}`}>
          <Money value={c.expected} currency={c.currency} />
        </dd>
        {c.counted ? (
          <>
            <dt>{t("cash.counted_at", { time: formatDateTime(c.countedAt) })}</dt>
            <dd>
              <Money value={c.count} currency={c.currency} />
            </dd>
            <dt>{t("cash.difference", { expected: formatDecimal(c.countExpected, locale) })}</dt>
            <dd data-testid={`difference-${c.currency}`} className={isZero(c.difference) ? "" : "text-danger"}>
              <Money value={c.difference} currency={c.currency} />
            </dd>
          </>
        ) : (
          <dd className="col-span-2 text-text-muted">{t("cash.not_counted")}</dd>
        )}
      </dl>
      {onCount ? (
        <Button variant="primary" onClick={onCount}>
          {t("cash.count.action")}
        </Button>
      ) : null}
    </div>
  );
}
