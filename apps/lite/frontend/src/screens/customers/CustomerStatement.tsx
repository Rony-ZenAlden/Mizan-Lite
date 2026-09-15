import { useCallback, useEffect, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { Customer, Entry, Sale, Statement } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal } from "@/i18n/numbers";
import { formatDateTime } from "@/i18n/time";
import { ExportButtons } from "@/exports/ExportButtons";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { printsItself, usePrinterSettings } from "@/printing/PrintPanel";
import { VoucherDialog, hasVoucher } from "@/printing/VoucherDialog";
import { Money, isZero } from "@/screens/sales/Money";
import { ReceiptView } from "@/screens/sales/ReceiptView";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { Balances, isNegative, unsigned } from "./Balances";
import { CustomerForm } from "./CustomerForm";
import { DebtAmountDialog, RefundDialog, ReverseDialog } from "./OwnerDialogs";
import { PaymentDialog } from "./PaymentDialog";

type Open =
  | { kind: "pay" | "opening" | "write_off" | "refund" | "edit" }
  | { kind: "reverse"; entry: Entry }
  | { kind: "receipt"; sale: Sale }
  | { kind: "voucher"; entry: Entry; auto: boolean }
  | null;

/**
 * A customer's statement (L5 §11.2): a tab per currency — never one list summed across them — every entry with the balance
 * after it, and the acts: take a payment for everyone; openings, write-offs, refunds and reversals through the owner's PIN.
 */
export function CustomerStatement({ customer, localCurrency, onChanged, onClose }: { customer: Customer; localCurrency: string; onChanged: () => void; onClose: () => void }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const currencies = [localCurrency, "USD"].filter(Boolean);
  const initial = customer.balances.find((b) => !isZero(b.balance))?.currency ?? customer.balances[0]?.currency ?? "USD";
  const [currency, setCurrency] = useState(initial);
  const [statement, setStatement] = useState<Statement | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [open, setOpen] = useState<Open>(null);
  const printer = usePrinterSettings();

  const load = useCallback(async () => {
    try {
      setStatement(await client.customers.statement(customer.id, currency));
      setError(null);
    } catch (e) {
      setError(e);
    }
  }, [client, customer.id, currency]);

  useEffect(() => {
    void load();
  }, [load]);

  const done = () => {
    setOpen(null);
    void load();
    onChanged();
  };

  // A payment or refund just recorded: its voucher, printing itself when the settings say so (Q-L7.2).
  const recorded = (entry: Entry) => {
    done();
    setOpen({ kind: "voucher", entry, auto: printsItself(printer, "voucher") });
  };

  const current = statement?.customer ?? customer;
  const balance = statement?.balance ?? "0";
  const owes = !isZero(balance) && !isNegative(balance);

  const toggleActive = async () => {
    try {
      await withOwner(() => client.customers.setActive({ id: current.id, rowVersion: current.rowVersion, active: !current.active }));
      done();
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    }
  };

  const openReceipt = async (saleId: string) => {
    try {
      setOpen({ kind: "receipt", sale: await client.sales.receipt(saleId) });
    } catch (e) {
      setError(e);
    }
  };

  return (
    <Dialog title={t("statement.title", { name: current.name })} onClose={onClose} wide>
      <div className="flex flex-wrap items-start justify-between gap-3 text-sm">
        <div className="space-y-1">
          {current.phone ? <bdi dir="ltr">{current.phone}</bdi> : null}
          {current.note ? <p className="text-text-muted">{current.note}</p> : null}
          {!current.active ? <p className="text-danger">{t("customers.inactive")}</p> : null}
          <Balances balances={current.balances} withReference rate={statement?.rate ?? ""} />
        </div>
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => setOpen({ kind: "edit" })}>{t("customers.edit")}</Button>
          <Button onClick={() => void toggleActive()}>{current.active ? t("customers.deactivate") : t("customers.activate")}</Button>
        </div>
      </div>
      <ExportButtons onExport={(format) => client.exports.statement(current.id, format)} />

      <div role="tablist" aria-label={t("statement.currencies")} className="flex gap-2 border-b border-border">
        {currencies.map((c) => (
          <button
            key={c}
            role="tab"
            type="button"
            aria-selected={c === currency}
            onClick={() => setCurrency(c)}
            className={`px-3 py-2 text-sm ${c === currency ? "border-b-2 border-primary font-semibold" : "text-text-muted"}`}
          >
            {tDynamic(`currency.${c}`)}
          </button>
        ))}
      </div>

      {error ? <Alert tone="danger" title={errorText(error)} /> : null}
      {statement ? (
        <div className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p data-testid="statement-balance" className="text-lg font-semibold">
              {isNegative(balance) ? (
                <>
                  {t("customers.in_favour")} <Money value={unsigned(balance)} currency={currency} />
                </>
              ) : (
                <>
                  {t("statement.balance")} <Money value={balance} currency={currency} />
                </>
              )}
              {statement.owedSince ? <span className="ms-2 text-sm font-normal text-text-muted">{t("statement.owed_since", { date: statement.owedSince })}</span> : null}
            </p>
            <div className="flex flex-wrap gap-2">
              {owes ? (
                <Button variant="primary" onClick={() => setOpen({ kind: "pay" })}>
                  {t("statement.take_payment")}
                </Button>
              ) : null}
              <Button onClick={() => setOpen({ kind: "opening" })}>{t("statement.opening")}</Button>
              {owes ? <Button onClick={() => setOpen({ kind: "write_off" })}>{t("statement.write_off")}</Button> : null}
              {isNegative(balance) ? <Button onClick={() => setOpen({ kind: "refund" })}>{t("statement.refund")}</Button> : null}
            </div>
          </div>

          {statement.entries.length === 0 ? <p className="text-text-muted">{t("statement.empty")}</p> : null}
          {statement.entries.length > 0 ? (
            <div className="overflow-x-auto rounded-md border border-border">
              <table className="w-full text-sm">
                <thead className="bg-surface text-text-muted">
                  <tr>
                    <th className="p-2 text-start">{t("statement.col.when")}</th>
                    <th className="p-2 text-start">{t("statement.col.kind")}</th>
                    <th className="p-2 text-start">{t("statement.col.amount")}</th>
                    <th className="p-2 text-start">{t("statement.col.balance")}</th>
                    <th className="p-2 text-start">{t("statement.col.details")}</th>
                    <th className="p-2 text-start">{t("statement.col.actions")}</th>
                  </tr>
                </thead>
                <tbody>
                  {statement.entries.map((e) => (
                    <tr key={e.id} className={`border-t border-border ${e.reversed ? "text-text-muted line-through" : ""}`} data-testid="statement-entry">
                      <td className="p-2">
                        <bdi dir="ltr">{formatDateTime(e.occurredAt, locale)}</bdi>
                      </td>
                      <td className="p-2">{tDynamic(`debt.kind.${e.kind}`)}</td>
                      <td className="p-2">
                        <Money value={e.amount} currency={e.currency} />
                      </td>
                      <td className="p-2">
                        <Money value={e.balanceAfter} currency={e.currency} />
                      </td>
                      <td className="p-2 text-xs">
                        {e.tendered ? (
                          <span className="block">
                            <bdi dir="ltr">
                              {t("statement.cash", {
                                tendered: `${formatDecimal(e.tendered, locale)} ${tDynamic(`currency.short.${e.tenderedCurrency}`)}`,
                                change: `${formatDecimal(e.change, locale)} ${tDynamic(`currency.short.${e.changeCurrency}`)}`,
                                rate: formatDecimal(e.rate, locale),
                              })}
                            </bdi>
                          </span>
                        ) : null}
                        {e.note ? <span className="block">{e.note}</span> : null}
                      </td>
                      <td className="p-2">
                        <div className="flex flex-wrap gap-2">
                          {e.saleId ? <Button onClick={() => void openReceipt(e.saleId)}>{t("statement.receipt")}</Button> : null}
                          {hasVoucher(e) && !e.reversed ? (
                            <Button onClick={() => setOpen({ kind: "voucher", entry: e, auto: false })}>{t("statement.voucher")}</Button>
                          ) : null}
                          {e.reversible ? <Button onClick={() => setOpen({ kind: "reverse", entry: e })}>{t("debt.reverse")}</Button> : null}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </div>
      ) : null}

      {open?.kind === "pay" ? (
        <PaymentDialog customerId={current.id} name={current.name} currency={currency} localCurrency={localCurrency} onDone={recorded} onClose={() => setOpen(null)} />
      ) : null}
      {open?.kind === "opening" || open?.kind === "write_off" ? (
        <DebtAmountDialog kind={open.kind} customerId={current.id} name={current.name} currency={currency} currencies={currencies} onDone={done} onClose={() => setOpen(null)} />
      ) : null}
      {open?.kind === "refund" ? (
        <RefundDialog customerId={current.id} name={current.name} currency={currency} currencies={currencies} onDone={recorded} onClose={() => setOpen(null)} />
      ) : null}
      {open?.kind === "reverse" ? <ReverseDialog entry={open.entry} onDone={done} onClose={() => setOpen(null)} /> : null}
      {open?.kind === "edit" ? <CustomerForm customer={current} onSaved={done} onClose={() => setOpen(null)} /> : null}
      {open?.kind === "voucher" ? <VoucherDialog entry={open.entry} auto={open.auto} onClose={() => setOpen(null)} /> : null}
      {open?.kind === "receipt" ? <ReceiptView sale={open.sale} onClose={() => setOpen(null)} onVoided={() => done()} /> : null}
    </Dialog>
  );
}
