import { useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Returnable, ReturnLineInput, SaleReturn } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { formErrors } from "@/screens/stock/forms";
import { Money } from "@/screens/sales/Money";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { Dialog } from "@/ui/Dialog";
import { CellInput, SelectField, TextField } from "@/ui/Field";

/**
 * The return wizard (F8, the owner's request of 2026-09-20): find the sale by the number printed on the receipt the
 * customer handed over, tick what is coming back, and record it.
 *
 * # Why the receipt number and not a search
 *
 * The person at the counter is holding the paper. Asking them to find the sale by product or by time, with the
 * customer waiting, would be slower than the thing it replaces.
 *
 * # Why it quotes before it records
 *
 * The customer is told what they are owed before the goods change hands, and the figure they are told is the figure
 * Go worked out — not one the screen calculated and Go might disagree with.
 */
export function ReturnWizard({ onClose, onDone }: { onClose: () => void; onDone: (r: SaleReturn) => void }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, locale, errorText } = useLocale();

  const [receiptNo, setReceiptNo] = useState("");
  const [sale, setSale] = useState<Returnable | null>(null);
  const [picked, setPicked] = useState<Record<string, { quantity: string; restock: boolean }>>({});
  const [settlement, setSettlement] = useState<"cash" | "debt">("cash");
  const [reason, setReason] = useState("");
  const [quote, setQuote] = useState<SaleReturn | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const errors = formErrors(error, errorText);

  const name = (l: { nameAr: string; nameEn: string }) => (locale === "en" && l.nameEn ? l.nameEn : l.nameAr);

  const find = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    setQuote(null);
    try {
      const found = await client.sales.returnable(receiptNo.trim());
      setSale(found);
      setPicked({});
      setSettlement(found.payment === "credit" ? "debt" : "cash");
    } catch (e) {
      setSale(null);
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  const lines = (): ReturnLineInput[] =>
    Object.entries(picked)
      .filter(([, v]) => v.quantity.trim() !== "")
      .map(([saleLineId, v]) => ({ saleLineId, quantity: v.quantity.trim(), restock: v.restock }));

  // Every change to what is ticked invalidates the figure on screen, so the quote is cleared rather than left to look
  // current: a stale refund beside a changed basket is how a customer is handed the wrong money.
  const change = (saleLineId: string, patch: Partial<{ quantity: string; restock: boolean }>) => {
    setQuote(null);
    setPicked((p) => ({ ...p, [saleLineId]: { quantity: "", restock: true, ...p[saleLineId], ...patch } }));
  };

  const priceIt = async () => {
    if (!sale) return;
    setBusy(true);
    setError(null);
    try {
      setQuote(await client.sales.quoteReturn({ saleId: sale.saleId, lines: lines(), settlement, reason }));
    } catch (e) {
      setQuote(null);
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  const record = async () => {
    if (!sale) return;
    setBusy(true);
    setError(null);
    try {
      onDone(await withOwner(() => client.sales.recordReturn({ saleId: sale.saleId, lines: lines(), settlement, reason })));
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  const anyLeft = sale?.lines.some((l) => l.returnable) ?? false;

  return (
    <Dialog title={t("returns.title")} onClose={onClose} wide>
      <form onSubmit={(e) => void find(e)} className="flex items-end gap-2">
        <TextField
          label={t("returns.find")}
          hint={t("returns.find_hint")}
          value={receiptNo}
          onChange={(e) => setReceiptNo(e.target.value)}
          inputMode="numeric"
          dir="ltr"
          autoFocus
        />
        <Button type="submit" disabled={busy || receiptNo.trim() === ""}>
          {t("returns.find_action")}
        </Button>
      </form>

      {sale ? (
        <div className="mt-4 space-y-3" data-testid="return-sale">
          <p className="text-sm font-semibold">
            {t("returns.sale", { number: sale.receiptNo, date: sale.businessDate })}
            {sale.customerName ? ` · ${sale.customerName}` : ""}
          </p>

          {sale.voided ? <Alert tone="danger" title={t("returns.voided")} /> : null}
          {!sale.voided && !anyLeft ? <Alert tone="danger" title={t("returns.nothing_left")} /> : null}

          {anyLeft && !sale.voided ? (
            <>
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-start text-xs text-text-muted">
                    <th className="p-1 text-start">{t("nav.products")}</th>
                    <th className="p-1 text-start">{t("returns.line_left")}</th>
                    <th className="p-1 text-start">{t("returns.quantity")}</th>
                    <th className="p-1 text-start">{t("returns.restock")}</th>
                  </tr>
                </thead>
                <tbody>
                  {sale.lines.map((l) => (
                    <tr key={l.saleLineId} data-testid="return-line" className={l.returnable ? "" : "opacity-50"}>
                      <td className="p-1">
                        {name(l)}
                        <span className="block text-xs text-text-muted">
                          {t("returns.line_sold")}: {l.sold} · {t("returns.line_returned")}: {l.returned}
                        </span>
                      </td>
                      <td className="p-1">
                        <bdi dir="ltr">{l.left}</bdi> {tDynamic(`uom.${l.unitCode}`)}
                      </td>
                      <td className="w-28 p-1">
                        <CellInput
                          aria-label={t("returns.quantity_of", { name: name(l) })}
                          value={picked[l.saleLineId]?.quantity ?? ""}
                          onChange={(e) => change(l.saleLineId, { quantity: e.target.value })}
                          disabled={!l.returnable}
                          inputMode="decimal"
                          dir="ltr"
                        />
                      </td>
                      <td className="p-1">
                        <Checkbox
                          label=""
                          aria-label={t("returns.restock_of", { name: name(l) })}
                          checked={picked[l.saleLineId]?.restock ?? true}
                          onChange={(e) => change(l.saleLineId, { restock: e.target.checked })}
                          disabled={!l.returnable}
                        />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <p className="text-xs text-text-muted">{t("returns.restock_hint")}</p>

              <SelectField
                label={t("returns.settlement")}
                value={settlement}
                onChange={(e) => {
                  setQuote(null);
                  setSettlement(e.target.value as "cash" | "debt");
                }}
              >
                <option value="cash">{t("returns.settlement.cash")}</option>
                {sale.payment === "credit" ? <option value="debt">{t("returns.settlement.debt")}</option> : null}
              </SelectField>
              <TextField
                label={t("returns.reason")}
                value={reason}
                onChange={(e) => {
                  setQuote(null);
                  setReason(e.target.value);
                }}
                maxLength={200}
                error={errors.field("reason")}
                required
              />

              {quote ? (
                <div className="rounded-md border border-border p-3" data-testid="return-quote">
                  <p className="font-semibold">
                    {t("returns.refund")}: <Money value={quote.refund} currency={quote.currency} />
                  </p>
                  {!quote.costKnown ? <p className="mt-1 text-xs text-text-muted">{t("returns.cost_unknown")}</p> : null}
                </div>
              ) : null}
            </>
          ) : null}

          {sale.returns.length > 0 ? (
            <details data-testid="return-past">
              <summary className="cursor-pointer text-sm text-text-muted">{t("returns.past")}</summary>
              <ul className="mt-1 space-y-1 text-sm">
                {sale.returns.map((r) => (
                  <li key={r.id}>
                    <bdi dir="ltr">#{r.returnNo}</bdi> · {r.businessDate} · <Money value={r.refund} currency={r.currency} />
                  </li>
                ))}
              </ul>
            </details>
          ) : null}
        </div>
      ) : null}

      {errors.form ? <Alert tone="danger" title={errors.form} /> : null}

      <div className="mt-4 flex justify-end gap-2">
        <Button onClick={onClose}>{t("action.cancel")}</Button>
        {quote ? (
          <Button variant="primary" onClick={() => void record()} disabled={busy}>
            {t("returns.confirm")}
          </Button>
        ) : (
          <Button
            variant="primary"
            onClick={() => void priceIt()}
            disabled={busy || !sale || sale.voided || lines().length === 0 || reason.trim() === ""}
          >
            {t("returns.refund")}
          </Button>
        )}
      </div>
    </Dialog>
  );
}
