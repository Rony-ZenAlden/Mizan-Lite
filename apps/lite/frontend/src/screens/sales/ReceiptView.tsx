import { useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Sale, SaleLine } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal } from "@/i18n/numbers";
import { formatDateTime } from "@/i18n/time";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { formErrors } from "@/screens/stock/forms";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { TextField } from "@/ui/Field";
import { Money, isZero } from "./Money";

/**
 * A receipt on screen (Q-L4.7): the sale exactly as recorded, laid out at the width of an 80 mm thermal roll (Q-L4.9) so
 * the printing in L7 prints what the shopkeeper has already seen. Voiding is here — the whole sale, the owner's PIN and a
 * reason (Q-L4.4, Q-L4.5).
 */
export function ReceiptView({ sale, onClose, onVoided }: { sale: Sale; onClose: () => void; onVoided: (sale: Sale) => void }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [voiding, setVoiding] = useState(false);
  const [reason, setReason] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const inSettlement = (local: string, usd: string) => (sale.settlement === "USD" ? usd : local);
  const net = (l: SaleLine) => inSettlement(l.netLocal, l.netUsd);
  const name = (l: SaleLine) => (locale === "en" && l.nameEn ? l.nameEn : l.nameAr);
  const discount = inSettlement(sale.discountLocal, sale.discountUsd);
  const errors = formErrors(error, errorText);

  const submitVoid = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const voided = await withOwner(() => client.sales.void({ saleId: sale.id, reason }));
      setVoiding(false);
      onVoided(voided);
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t("receipt.title", { number: String(sale.receiptNo) })} onClose={onClose}>
      <article data-testid="receipt" className="mx-auto w-full max-w-[80mm] space-y-2 border border-dashed border-border p-3 text-xs">
        <header className="space-y-1 text-center">
          <p className="text-sm font-semibold">{sale.shopName}</p>
          <p>
            {t("receipt.number", { number: String(sale.receiptNo) })} · <bdi dir="ltr">{formatDateTime(sale.soldAt, locale)}</bdi>
          </p>
        </header>

        {sale.status === "voided" ? (
          <p role="status" className="rounded-sm bg-danger-subtle p-1 text-center font-semibold text-danger">
            {t("receipt.voided", { reason: sale.voidReason })}
          </p>
        ) : null}

        <ul className="space-y-1 border-t border-dashed border-border pt-2">
          {sale.lines.map((l) => (
            <li key={l.id}>
              <p className="font-medium">{name(l)}</p>
              <p className="flex justify-between gap-2">
                <span>
                  <bdi dir="ltr">
                    {formatDecimal(l.quantity, locale)} {tDynamic(`uom.${l.unitCode}`)} × {formatDecimal(l.unitPrice, locale)} {tDynamic(`currency.short.${l.priceCurrency}`)}
                  </bdi>
                </span>
                <Money value={net(l)} currency={sale.settlement} />
              </p>
              {l.discountPercent ? <p className="text-text-muted">{t("receipt.line_discount", { percent: l.discountPercent })}</p> : null}
            </li>
          ))}
        </ul>

        <dl className="grid grid-cols-[1fr_auto] gap-x-2 border-t border-dashed border-border pt-2">
          {!isZero(discount) ? (
            <>
              <dt>{t("receipt.discount")}</dt>
              <dd>
                <Money value={discount} currency={sale.settlement} />
              </dd>
            </>
          ) : null}
          {!isZero(sale.rounding) ? (
            <>
              <dt>{t("receipt.rounding", { note: sale.cashNote ? formatDecimal(sale.cashNote, locale) : "" })}</dt>
              <dd>
                <Money value={sale.rounding} currency={sale.settlement} />
              </dd>
            </>
          ) : null}
          <dt className="text-sm font-semibold">{t("receipt.total")}</dt>
          <dd className="text-sm font-semibold">
            <Money value={sale.total} currency={sale.settlement} />
          </dd>
          {sale.payment === "credit" ? (
            <>
              <dt>{t("receipt.paid_now")}</dt>
              <dd>
                <Money value={sale.tendered} currency={sale.tenderCurrency} />
              </dd>
            </>
          ) : (
            <>
              <dt>{t("receipt.tendered")}</dt>
              <dd>
                <Money value={sale.tendered} currency={sale.tenderCurrency} />
              </dd>
              <dt>{t("receipt.change")}</dt>
              <dd>
                <Money value={sale.change} currency={sale.changeCurrency} />
              </dd>
            </>
          )}
        </dl>

        {sale.payment === "credit" && sale.creditCustomerId ? (
          <dl data-testid="receipt-credit" className="grid grid-cols-[1fr_auto] gap-x-2 border-t border-dashed border-border pt-2">
            <dt className="col-span-2 font-semibold">{t("receipt.on_credit", { name: sale.creditCustomerName })}</dt>
            <dt>{t("receipt.debt_added")}</dt>
            <dd>
              <Money value={sale.creditAmount} currency={sale.creditCurrency} />
            </dd>
            <dt>{t("receipt.balance_after")}</dt>
            <dd>
              <Money value={sale.creditBalanceAfter} currency={sale.creditCurrency} />
            </dd>
            {sale.creditReversed ? <dd className="col-span-2 text-danger">{t("receipt.debt_reversed")}</dd> : null}
          </dl>
        ) : null}

        <footer className="space-y-1 border-t border-dashed border-border pt-2 text-center text-text-muted">
          <p>
            <bdi dir="ltr">{t("receipt.rate", { rate: formatDecimal(sale.rate, locale), currency: tDynamic(`currency.short.${sale.localCurrency}`) })}</bdi>
          </p>
          <p>{t("receipt.thanks")}</p>
        </footer>
      </article>

      {voiding ? (
        <form className="space-y-3" onSubmit={submitVoid}>
          <p role="status" data-testid="void-hand-back" className="rounded-md border border-border bg-surface p-2 text-sm font-semibold">
            {t("receipt.void_hand_back", { amount: formatDecimal(sale.voidReturn, locale), currency: tDynamic(`currency.short.${sale.voidReturnCurrency}`) })}
          </p>
          <TextField
            label={t("receipt.void_reason")}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            error={errors.field("reason")}
            hint={t("receipt.void_hint")}
            maxLength={200}
            required
          />
          {errors.form ? <Alert tone="danger" title={errors.form} /> : null}
          <div className="flex justify-end gap-2">
            <Button onClick={() => setVoiding(false)}>{t("action.cancel")}</Button>
            <Button variant="primary" type="submit" disabled={busy || reason.trim() === ""}>
              {t("receipt.void_confirm")}
            </Button>
          </div>
        </form>
      ) : (
        <div className="flex justify-end gap-2">
          {sale.status === "posted" ? <Button onClick={() => setVoiding(true)}>{t("receipt.void")}</Button> : null}
          <Button variant="primary" onClick={onClose}>
            {t("action.close")}
          </Button>
        </div>
      )}
    </Dialog>
  );
}
