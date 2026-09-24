import { useEffect, useRef, useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Entry, PaymentQuote } from "@/api/client";
import { BindingError } from "@/api/envelope";
import { useLocale } from "@/i18n/LocaleProvider";
import { useCurrencies } from "@/rates/useCurrencies";
import { Money } from "@/screens/sales/Money";
import { formErrors } from "@/screens/stock/forms";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { Dialog } from "@/ui/Dialog";
import { ratePair } from "@/i18n/figures";
import { SelectField, TextField } from "@/ui/Field";

/** How long typing pauses before a payment is priced again. */
export const PAYMENT_DEBOUNCE_MS = 150;

/** The code a payment is refused with when the rate or balance moved after its quote (customers/domain.CodePaymentStale). */
export const CODE_PAYMENT_STALE = "lite.customers.payment_stale";

/**
 * A repayment (L5 §6): in the debt's currency or the other, at the rate in force today (Q-L5.5); an amount handed over,
 * or pay all. What it settles, the change and the balance after are Go's quote, and recording sends that quote's token —
 * so a rate changed or another payment taken in between is caught. No PIN: money coming in.
 */
export function PaymentDialog({
  customerId,
  name,
  currency,
  localCurrency,
  onDone,
  onClose,
}: {
  customerId: string;
  name: string;
  currency: string;
  localCurrency: string;
  onDone: (e: Entry) => void;
  onClose: () => void;
}) {
  const client = useClient();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [tenderCurrency, setTenderCurrency] = useState("");
  const [amount, setAmount] = useState("");
  const [all, setAll] = useState(false);
  const [changeCurrency, setChangeCurrency] = useState("");
  const [note, setNote] = useState("");
  const [quote, setQuote] = useState<PaymentQuote | null>(null);
  const [quoteError, setQuoteError] = useState<unknown>(null);
  const [error, setError] = useState<unknown>(null);
  const [stale, setStale] = useState(false);
  const [busy, setBusy] = useState(false);
  const [requote, setRequote] = useState(0);
  const request = useRef(0);

  const input = { customerId, currency, tenderCurrency, amount: all ? "" : amount, all, changeCurrency, note, token: "" };

  useEffect(() => {
    const id = ++request.current;
    if (!all && amount === "") {
      setQuote(null);
      setQuoteError(null);
      return;
    }
    const timer = setTimeout(() => {
      client.customers
        .quotePayment(input)
        .then((q) => {
          if (id !== request.current) return;
          setQuote(q);
          setQuoteError(null);
        })
        .catch((e) => {
          if (id !== request.current) return;
          setQuote(null);
          setQuoteError(e);
        });
    }, PAYMENT_DEBOUNCE_MS);
    return () => clearTimeout(timer);
    // input is rebuilt every render from the fields listed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, customerId, currency, tenderCurrency, amount, all, changeCurrency, requote]);

  const errors = formErrors(quoteError ?? error, errorText);
  const { currencies } = useCurrencies(localCurrency);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!quote) return;
    setBusy(true);
    setError(null);
    try {
      onDone(await client.customers.recordPayment({ ...input, token: quote.token }));
    } catch (e) {
      if (e instanceof BindingError && e.code === CODE_PAYMENT_STALE) {
        setStale(true);
        setRequote((n) => n + 1);
      } else {
        setError(e);
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t("payment.title", { name, currency: tDynamic(`currency.${currency}`) })} onClose={onClose}>
      <form className="space-y-3" onSubmit={(e) => void submit(e)}>
        <SelectField label={t("payment.paid_in")} value={tenderCurrency} onChange={(e) => setTenderCurrency(e.target.value)}>
          <option value="">{tDynamic(`currency.${currency}`)}</option>
          {currencies
            .filter((c) => c !== currency)
            .map((c) => (
              <option key={c} value={c}>
                {tDynamic(`currency.${c}`)}
              </option>
            ))}
        </SelectField>
        <Checkbox label={t("payment.all")} checked={all} onChange={(e) => setAll(e.target.checked)} />
        {all ? null : (
          <TextField
            label={t("payment.amount")}
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            error={errors.field("amount") ?? errors.field("tendered")}
            inputMode="decimal"
            dir="ltr"
            autoComplete="off"
          />
        )}
        <SelectField label={t("payment.change_in")} value={changeCurrency} onChange={(e) => setChangeCurrency(e.target.value)} error={errors.field("changeCurrency")} hint={t("till.change_default_hint")}>
          <option value="">{t("till.change_default")}</option>
          {currencies.map((c) => (
            <option key={c} value={c}>
              {tDynamic(`currency.${c}`)}
            </option>
          ))}
        </SelectField>
        <TextField label={t("customers.note")} value={note} onChange={(e) => setNote(e.target.value)} maxLength={200} autoComplete="off" />

        {quote ? (
          <dl data-testid="payment-quote" className="grid grid-cols-[1fr_auto] gap-x-3 rounded-md border border-border p-3 text-sm">
            <dt>{t("payment.take")}</dt>
            <dd>
              <Money value={quote.tendered} currency={quote.tenderCurrency} className="font-semibold" />
            </dd>
            <dt>{t("payment.settles")}</dt>
            <dd>
              <Money value={quote.settled} currency={quote.currency} />
            </dd>
            {quote.change !== "" && !/^0(\.0+)?$/.test(quote.change) ? (
              <>
                <dt>{t("payment.change")}</dt>
                <dd>
                  <Money value={quote.change} currency={quote.changeCurrency} />
                </dd>
              </>
            ) : null}
            <dt>{t("payment.balance_after")}</dt>
            <dd>
              <Money value={quote.balanceAfter} currency={quote.currency} className="font-semibold" />
            </dd>
            <dd className="col-span-2 text-xs text-text-muted">
              {t("till.rate", ratePair(quote.rate, localCurrency, locale, tDynamic))}
            </dd>
          </dl>
        ) : null}
        {stale ? <Alert tone="danger" title={t("payment.stale")} /> : null}
        {errors.form ? <Alert tone="danger" title={errors.form} /> : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || !quote}>
            {t("payment.record")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
