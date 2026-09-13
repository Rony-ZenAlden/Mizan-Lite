import { useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Product, StockLevel, Unit } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { TextField } from "@/ui/Field";
import { formErrors, typedQuantity } from "./forms";

/**
 * A count: what is on the shelf, not the difference — Go works the difference out when it writes (D-L2.10). A count
 * lower than the record needs the owner, which withOwner asks for only if Go says so (Q-L2.3).
 */
export function CountDialog({
  product,
  name,
  unit,
  onHand,
  onDone,
  onClose,
}: {
  product: Product;
  name: string;
  unit: Unit;
  onHand: string;
  onDone: (level: StockLevel) => void;
  onClose: () => void;
}) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [counted, setCounted] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const code = typedQuantity(counted, unit.inputDecimals);
  const errors = formErrors(error, errorText);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onDone(await withOwner(() => client.stock.count({ productId: product.id, counted, note })));
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t("stock.count_title", { name })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <p className="text-sm">
          {t("stock.count_current", { quantity: `${formatDecimal(onHand, locale)} ${tDynamic(`uom.${unit.code}`)}` })}
        </p>
        <TextField
          label={t("stock.counted", { unit: tDynamic(`uom.${unit.code}`) })}
          value={counted}
          onChange={(e) => setCounted(e.target.value)}
          error={(code ? tDynamic(code, { decimals: String(unit.inputDecimals) }) : undefined) ?? errors.field("quantity")}
          hint={t("stock.count_hint")}
          inputMode="decimal"
          dir="ltr"
          required
        />
        <TextField label={t("stock.note")} value={note} onChange={(e) => setNote(e.target.value)} error={errors.field("note")} maxLength={200} />
        {errors.form ? (
          <p role="alert" className="text-sm text-danger">
            {errors.form}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || counted === "" || Boolean(code)}>
            {t("action.save")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
