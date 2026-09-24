import { useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Currency, Product, StockLevel, Unit } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { typeable } from "@/i18n/numbers";
import { formatDecimal } from "@/i18n/numbers";
import { useRate } from "@/rates/RateProvider";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { SelectField, TextField } from "@/ui/Field";
import { formErrors, typedAmount, typedQuantity } from "./forms";

/** The cost currency; a cost in any other currency asks for the rate it was paid at (Q-L2.1). */
export const COST_CURRENCY = "USD";

/**
 * A delivery, or — for a product that has never moved — its opening stock (L2 §5.1–5.2).
 *
 * The cost is typed the way the invoice reads: a total by default, or per unit (Q-L2.7). Go divides; this form never
 * does arithmetic on it.
 */
export function ReceiveDialog({
  product,
  name,
  unit,
  currencies,
  firstMovement,
  onDone,
  onClose,
}: {
  product: Product;
  name: string;
  unit: Unit;
  currencies: Currency[];
  firstMovement: boolean;
  onDone: (level: StockLevel) => void;
  onClose: () => void;
}) {
  const client = useClient();
  const { t, tDynamic, errorText, locale } = useLocale();
  const { rate: inForce } = useRate();
  // A dollars-only shop receives in dollars (0.10.0).
  const usdOnly = inForce?.usdOnly === true;
  const [kind, setKind] = useState<"receipt" | "opening">(firstMovement ? "opening" : "receipt");
  const [quantity, setQuantity] = useState("");
  const [costMode, setCostMode] = useState<"total" | "unit">("total");
  const [cost, setCost] = useState("");
  const [currency, setCurrency] = useState(COST_CURRENCY);
  const [rate, setRate] = useState("");
  // A supplier's discount off the cost typed, as a percentage (0.10.0): Go costs the delivery at what was really paid.
  const [discount, setDiscount] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const unitName = tDynamic(`uom.${unit.code}`);
  const needsRate = currency !== COST_CURRENCY;
  const quantityCode = typedQuantity(quantity, unit.inputDecimals);
  const costCode = typedAmount(cost);
  const rateCode = needsRate ? typedAmount(rate) : null;
  const complete = quantity !== "" && cost !== "" && (!needsRate || rate !== "");
  const errors = formErrors(error, errorText);
  const hint = (code: string | null) => (code ? tDynamic(code, { decimals: String(unit.inputDecimals) }) : undefined);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const input = { productId: product.id, quantity, costMode, cost, currency, rate: needsRate ? rate : "", note, discountPercent: discount.trim() };
      onDone(await (kind === "opening" ? client.stock.opening(input) : client.stock.receive(input)));
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t("stock.receive_title", { name })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        {firstMovement ? (
          <SelectField label={t("stock.receive_kind")} value={kind} onChange={(e) => setKind(e.target.value as "receipt" | "opening")}>
            <option value="opening">{t("stock.kind_opening")}</option>
            <option value="receipt">{t("stock.kind_receipt")}</option>
          </SelectField>
        ) : null}
        <TextField
          label={t("stock.quantity", { unit: unitName })}
          value={quantity}
          onChange={(e) => setQuantity(e.target.value)}
          error={hint(quantityCode) ?? errors.field("quantity")}
          inputMode="decimal"
          dir="ltr"
          required
        />
        <div className="grid grid-cols-2 gap-3">
          <SelectField label={t("stock.cost_mode")} value={costMode} onChange={(e) => setCostMode(e.target.value as "total" | "unit")}>
            <option value="total">{t("stock.cost_mode_total")}</option>
            <option value="unit">{t("stock.cost_mode_unit")}</option>
          </SelectField>
          <SelectField
            label={t("stock.currency")}
            value={currency}
            onChange={(e) => {
              const next = e.target.value;
              setCurrency(next);
              // Pre-filled from the rate in force (Q-L2.1, D-L3.12), still editable: the rate the shop paid at is the one kept.
              if (next !== COST_CURRENCY && rate === "" && inForce?.set && inForce.localCurrency === next) setRate(typeable(inForce.rate));
            }}
            error={errors.field("currency")}
          >
            {currencies.filter((c) => !usdOnly || c.code === COST_CURRENCY).map((c) => (
              <option key={c.code} value={c.code}>
                {tDynamic(`currency.${c.code}`)}
              </option>
            ))}
          </SelectField>
        </div>
        <TextField
          label={t(costMode === "total" ? "stock.cost_total" : "stock.cost_unit")}
          value={cost}
          onChange={(e) => setCost(e.target.value)}
          error={hint(costCode) ?? errors.field("cost")}
          inputMode="decimal"
          dir="ltr"
          required
        />
        <TextField
          label={t("stock.discount")}
          value={discount}
          onChange={(e) => setDiscount(e.target.value)}
          error={errors.field("discountPercent")}
          hint={t("stock.discount_hint")}
          inputMode="decimal"
          dir="ltr"
          autoComplete="off"
        />
        {needsRate ? (
          <TextField
            label={t("stock.rate")}
            value={rate}
            onChange={(e) => setRate(e.target.value)}
            error={hint(rateCode) ?? errors.field("rate")}
            hint={
              inForce?.set
                ? t(inForce.stale ? "stock.rate_in_force_stale" : "stock.rate_in_force", { rate: formatDecimal(inForce.rate, locale) })
                : undefined
            }
            inputMode="decimal"
            dir="ltr"
            required
          />
        ) : null}
        <TextField label={t("stock.note")} value={note} onChange={(e) => setNote(e.target.value)} error={errors.field("note")} maxLength={200} />
        {errors.form ? (
          <p role="alert" className="text-sm text-danger">
            {errors.form}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || !complete || Boolean(quantityCode || costCode || rateCode)}>
            {t("action.save")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
