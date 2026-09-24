import { useEffect, useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Purchase } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal, formatInteger } from "@/i18n/numbers";
import { formatDate } from "@/i18n/time";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { formErrors } from "@/screens/stock/forms";
import { Money, isZero } from "@/screens/sales/Money";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { TextField } from "@/ui/Field";
import { useSupplierAct } from "./SupplierDialogs";

/**
 * One purchase as recorded (0.10.0): what arrived, what was damaged and never charged, every discount, and what each good
 * unit cost — the cost the stock book holds. A purchase entered by mistake is voided here.
 */
export function PurchaseView({ purchaseId, purchase: given, onChanged, onClose }: { purchaseId: string; purchase?: Purchase; onChanged: () => void; onClose: () => void }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [purchase, setPurchase] = useState<Purchase | null>(given ?? null);
  const [error, setError] = useState<unknown>(null);
  const [voiding, setVoiding] = useState(false);

  useEffect(() => {
    if (given) return;
    let cancelled = false;
    withOwner(() => client.suppliers.purchase(purchaseId))
      .then((p) => {
        if (!cancelled) setPurchase(p);
      })
      .catch((e: unknown) => {
        if (!cancelled && !(e instanceof OwnerCancelled)) setError(e);
      });
    return () => {
      cancelled = true;
    };
  }, [client, withOwner, purchaseId, given]);

  const name = (nameAr: string, nameEn: string) => (locale === "en" && nameEn ? nameEn : nameAr);

  return (
    <Dialog
      title={purchase ? t("purchase.view_title", { no: formatInteger(purchase.purchaseNo, locale), name: purchase.supplierName }) : t("purchase.loading")}
      onClose={onClose}
      wide
    >
      <Button variant="primary" onClick={onClose} data-testid="purchase-view-back">
        {t("action.back")}
      </Button>
      {error ? <Alert tone="danger" title={errorText(error)} /> : null}
      {purchase ? (
        <div className="space-y-3 text-sm" data-testid="purchase-view">
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
            <dt className="text-text-muted">{t("purchase.date")}</dt>
            <dd>
              <bdi dir="ltr">{formatDate(purchase.businessDate)}</bdi>
            </dd>
            {purchase.supplierRef ? (
              <>
                <dt className="text-text-muted">{t("purchase.ref")}</dt>
                <dd>
                  <bdi dir="ltr">{purchase.supplierRef}</bdi>
                </dd>
              </>
            ) : null}
            <dt className="text-text-muted">{t("purchase.currency")}</dt>
            <dd>
              {tDynamic(`currency.${purchase.currency}`)}
              {purchase.rate ? <span className="ms-2 text-text-muted">{t("purchase.at_rate", { rate: formatDecimal(purchase.rate, locale) })}</span> : null}
            </dd>
            {purchase.note ? (
              <>
                <dt className="text-text-muted">{t("purchase.note")}</dt>
                <dd>{purchase.note}</dd>
              </>
            ) : null}
          </dl>
          {purchase.status === "voided" ? <Alert tone="danger" title={t("purchase.voided", { reason: purchase.voidReason })} /> : null}

          <div className="overflow-x-auto rounded-md border border-border">
            <table className="w-full text-sm">
              <thead className="bg-surface text-text-muted">
                <tr>
                  <th className="p-2 text-start">{t("purchase.col.item")}</th>
                  <th className="p-2 text-start">{t("purchase.col.quantity")}</th>
                  <th className="p-2 text-start">{t("purchase.col.damaged")}</th>
                  <th className="p-2 text-start">{t("purchase.col.unit_cost")}</th>
                  <th className="p-2 text-start">{t("purchase.col.discount")}</th>
                  <th className="p-2 text-start">{t("purchase.col.due")}</th>
                  <th className="p-2 text-start">{t("purchase.col.net_cost")}</th>
                </tr>
              </thead>
              <tbody>
                {purchase.lines.map((l) => (
                  <tr key={l.lineNo} className="border-t border-border" data-testid="purchase-line">
                    <td className="p-2">{name(l.nameAr, l.nameEn)}</td>
                    <td className="p-2">
                      <bdi dir="ltr">{formatDecimal(l.quantity, locale)}</bdi> {tDynamic(`uom.${l.unitCode}`)}
                    </td>
                    <td className="p-2">{isZero(l.damaged) ? null : <bdi dir="ltr">{formatDecimal(l.damaged, locale)}</bdi>}</td>
                    <td className="p-2">
                      <Money value={l.unitCost} currency={purchase.currency} />
                    </td>
                    <td className="p-2">
                      {isZero(l.lineDiscount) ? null : <Money value={l.lineDiscount} currency={purchase.currency} />}
                      {l.discountPercent ? <span className="ms-1 text-xs text-text-muted">({formatDecimal(l.discountPercent, locale)}%)</span> : null}
                    </td>
                    <td className="p-2">
                      <Money value={l.due} currency={purchase.currency} className="font-medium" />
                    </td>
                    <td className="p-2">{isZero(l.good) ? null : <Money value={l.netUnitCost} currency={purchase.currency} />}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <PurchaseTotals purchase={purchase} />

          {purchase.status === "posted" ? (
            <div className="flex justify-end">
              <Button onClick={() => setVoiding(true)}>{t("purchase.void")}</Button>
            </div>
          ) : null}
        </div>
      ) : null}
      {voiding && purchase ? (
        <VoidPurchaseDialog
          purchase={purchase}
          onDone={(p) => {
            setVoiding(false);
            setPurchase(p);
            onChanged();
          }}
          onClose={() => setVoiding(false)}
        />
      ) : null}
    </Dialog>
  );
}

/** A purchase's figures, top to bottom: the goods, the discounts, what is due, what was paid on the spot. */
export function PurchaseTotals({ purchase }: { purchase: Purchase }) {
  const { t, tDynamic } = useLocale();
  const c = purchase.currency;
  const rows: { key: string; value: string; strong?: boolean }[] = [
    { key: "purchase.gross", value: purchase.gross },
    { key: "purchase.line_discounts", value: purchase.lineDiscount },
    { key: "purchase.invoice_discount_total", value: purchase.invoiceDiscount },
    { key: "purchase.due", value: purchase.due, strong: true },
  ];
  return (
    <dl className="ms-auto grid max-w-sm grid-cols-[1fr_auto] gap-x-4 gap-y-1" data-testid="purchase-totals">
      {rows
        .filter((r) => r.strong || !isZero(r.value))
        .map((r) => (
          <div key={r.key} className="contents">
            <dt className={r.strong ? "font-semibold" : "text-text-muted"}>{tDynamic(r.key)}</dt>
            <dd>
              <Money value={r.value} currency={c} className={r.strong ? "font-semibold" : undefined} />
            </dd>
          </div>
        ))}
      {isZero(purchase.paidNow) ? null : (
        <>
          <dt className="text-text-muted">{t("purchase.paid_now_from", { source: tDynamic(`suppliers.source.${purchase.paidFrom}`) })}</dt>
          <dd>
            <Money value={purchase.paidNow} currency={c} />
          </dd>
        </>
      )}
    </dl>
  );
}

/** Voiding a purchase entered by mistake: the goods back out of stock, the cost off the account, a reason. The owner's act. */
function VoidPurchaseDialog({ purchase, onDone, onClose }: { purchase: Purchase; onDone: (p: Purchase) => void; onClose: () => void }) {
  const client = useClient();
  const { t, errorText, locale } = useLocale();
  const [reason, setReason] = useState("");
  const { error, busy, run } = useSupplierAct(onDone);
  const errors = formErrors(error, errorText);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    void run(() => client.suppliers.voidPurchase(purchase.id, reason));
  };

  return (
    <Dialog title={t("purchase.void_title", { no: formatInteger(purchase.purchaseNo, locale) })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <p className="text-sm">{t("purchase.void_hint")}</p>
        <TextField label={t("suppliers.reason")} value={reason} onChange={(e) => setReason(e.target.value)} error={errors.field("reason")} maxLength={200} required autoComplete="off" />
        {errors.form ? (
          <p role="alert" className="text-sm text-danger">
            {errors.form}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || reason.trim() === ""}>
            {t("purchase.void")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
