import { useCallback, useEffect, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { Purchase, Supplier, SupplierEntry, SupplierStatement } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { useCurrencies } from "@/rates/useCurrencies";
import { formatDateTime } from "@/i18n/time";
import { formatInteger } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { isNegative, unsigned } from "@/screens/customers/Balances";
import { Money, isZero } from "@/screens/sales/Money";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { PurchaseDialog } from "./PurchaseDialog";
import { PurchaseView } from "./PurchaseView";
import { SupplierBalances } from "./SupplierBalances";
import { SupplierMoneyDialog, SupplierReverseDialog } from "./SupplierDialogs";
import { SupplierForm } from "./SupplierForm";

type Open =
  | { kind: "pay" | "refund" | "opening" | "edit" | "purchase" }
  | { kind: "reverse"; entry: SupplierEntry }
  | { kind: "view"; purchaseId: string }
  | null;

/**
 * A supplier's account (0.10.0): a tab per currency — never one list summed across them — every entry with the balance
 * after it, and the acts: a purchase, a payment, money back, an opening balance, a reversal.
 */
export function SupplierAccount({
  supplier,
  localCurrency,
  rate,
  onChanged,
  onClose,
}: {
  supplier: Supplier;
  localCurrency: string;
  rate: string;
  onChanged: () => void;
  onClose: () => void;
}) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const { currencies } = useCurrencies(localCurrency);
  // The first currency owed in, of those the shop still reads — dollars alone in a dollars-only shop (0.10.0).
  const initial = supplier.balances.find((b) => !isZero(b.balance) && currencies.includes(b.currency))?.currency ?? currencies[currencies.length - 1] ?? "USD";
  const [currency, setCurrency] = useState(initial);
  const [statement, setStatement] = useState<SupplierStatement | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [open, setOpen] = useState<Open>(null);

  const load = useCallback(async () => {
    try {
      setStatement(await withOwner(() => client.suppliers.statement(supplier.id, currency)));
      setError(null);
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    }
  }, [client, withOwner, supplier.id, currency]);

  useEffect(() => {
    void load();
  }, [load]);

  const close = useCallback(() => setOpen(null), []);
  const done = useCallback(() => {
    setOpen(null);
    void load();
    onChanged();
  }, [load, onChanged]);
  const bought = useCallback(
    (p: Purchase) => {
      setCurrency(p.currency);
      setOpen({ kind: "view", purchaseId: p.id });
      void load();
      onChanged();
    },
    [load, onChanged],
  );

  const current = statement?.supplier ?? supplier;
  const balance = statement?.balance ?? "0";

  const toggleActive = async () => {
    try {
      await withOwner(() => client.suppliers.setActive(current.id, current.rowVersion, !current.active));
      done();
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    }
  };

  const details = (e: SupplierEntry) => {
    const parts: string[] = [];
    if (e.purchaseNo > 0) parts.push(t("suppliers.purchase_no", { no: formatInteger(e.purchaseNo, locale) }));
    if (e.supplierRef) parts.push(t("suppliers.their_invoice", { ref: e.supplierRef }));
    if (e.source) parts.push(tDynamic(`suppliers.source.${e.source}`));
    if (e.note) parts.push(e.note);
    return parts.join(" · ");
  };

  return (
    <Dialog title={t("suppliers.account_title", { name: current.name })} onClose={onClose} wide>
      <Button variant="primary" onClick={onClose} data-testid="supplier-account-back">
        {t("action.back")}
      </Button>
      <div className="flex flex-wrap items-start justify-between gap-3 text-sm">
        <div className="space-y-1">
          {current.phone ? <bdi dir="ltr">{current.phone}</bdi> : null}
          {current.city ? <p>{current.city}</p> : null}
          {current.note ? <p className="text-text-muted">{current.note}</p> : null}
          {!current.active ? <p className="text-danger">{t("suppliers.inactive")}</p> : null}
          <SupplierBalances balances={current.balances} />
        </div>
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => setOpen({ kind: "edit" })}>{t("suppliers.edit")}</Button>
          <Button onClick={() => void toggleActive()}>{current.active ? t("suppliers.deactivate") : t("suppliers.activate")}</Button>
        </div>
      </div>

      <div role="tablist" aria-label={t("suppliers.currencies")} className="flex gap-2 border-b border-border">
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
            <p data-testid="supplier-balance" className="text-lg font-semibold">
              {isZero(balance) ? (
                t("suppliers.settled")
              ) : isNegative(balance) ? (
                <>
                  {t("suppliers.owes_shop")} <Money value={unsigned(balance)} currency={currency} />
                </>
              ) : (
                <>
                  {t("suppliers.shop_owes")} <Money value={balance} currency={currency} />
                </>
              )}
            </p>
            <div className="flex flex-wrap gap-2">
              {current.active ? (
                <Button variant="primary" onClick={() => setOpen({ kind: "purchase" })}>
                  {t("suppliers.new_purchase")}
                </Button>
              ) : null}
              <Button onClick={() => setOpen({ kind: "pay" })}>{t("suppliers.pay")}</Button>
              {isNegative(balance) ? <Button onClick={() => setOpen({ kind: "refund" })}>{t("suppliers.refund")}</Button> : null}
              <Button onClick={() => setOpen({ kind: "opening" })}>{t("suppliers.opening")}</Button>
            </div>
          </div>

          {statement.entries.length === 0 ? <p className="text-text-muted">{t("suppliers.account_empty")}</p> : null}
          {statement.entries.length > 0 ? (
            <div className="overflow-x-auto rounded-md border border-border">
              <table className="w-full text-sm">
                <thead className="bg-surface text-text-muted">
                  <tr>
                    <th className="p-2 text-start">{t("suppliers.col.when")}</th>
                    <th className="p-2 text-start">{t("suppliers.col.kind")}</th>
                    <th className="p-2 text-start">{t("suppliers.col.amount")}</th>
                    <th className="p-2 text-start">{t("suppliers.col.balance")}</th>
                    <th className="p-2 text-start">{t("suppliers.col.details")}</th>
                    <th className="p-2 text-start">{t("suppliers.col.actions")}</th>
                  </tr>
                </thead>
                <tbody>
                  {statement.entries.map((e) => (
                    <tr key={e.id} className={`border-t border-border ${e.reversed ? "text-text-muted line-through" : ""}`} data-testid="supplier-entry">
                      <td className="p-2">
                        <bdi dir="ltr">{formatDateTime(e.occurredAt)}</bdi>
                      </td>
                      <td className="p-2">{tDynamic(`suppliers.kind.${e.kind}`)}</td>
                      <td className="p-2">
                        <Money value={e.amount} currency={e.currency} />
                      </td>
                      <td className="p-2">
                        <Money value={e.balanceAfter} currency={e.currency} />
                      </td>
                      <td className="p-2 text-xs">{details(e)}</td>
                      <td className="p-2">
                        <div className="flex flex-wrap gap-2">
                          {e.purchaseId ? <Button onClick={() => setOpen({ kind: "view", purchaseId: e.purchaseId })}>{t("suppliers.view_purchase")}</Button> : null}
                          {e.reversible ? <Button onClick={() => setOpen({ kind: "reverse", entry: e })}>{t("suppliers.reverse")}</Button> : null}
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

      {open?.kind === "pay" || open?.kind === "refund" || open?.kind === "opening" ? (
        <SupplierMoneyDialog kind={open.kind} supplierId={current.id} name={current.name} currency={currency} currencies={currencies} onDone={done} onClose={close} />
      ) : null}
      {open?.kind === "reverse" ? <SupplierReverseDialog entry={open.entry} onDone={done} onClose={close} /> : null}
      {open?.kind === "edit" ? <SupplierForm supplier={current} onSaved={done} onClose={close} /> : null}
      {open?.kind === "purchase" ? (
        <PurchaseDialog suppliers={[current]} supplierId={current.id} localCurrency={localCurrency} rate={rate} onRecorded={bought} onClose={close} />
      ) : null}
      {open?.kind === "view" ? <PurchaseView purchaseId={open.purchaseId} onChanged={done} onClose={close} /> : null}
    </Dialog>
  );
}
