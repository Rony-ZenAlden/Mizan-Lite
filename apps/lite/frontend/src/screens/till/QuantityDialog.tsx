import { useState, type FormEvent } from "react";
import { useLocale } from "@/i18n/LocaleProvider";
import { typedQuantity } from "@/screens/stock/forms";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { TextField } from "@/ui/Field";

/**
 * How much of a weighed or measured product (L4 §10.2): 1.750 kg, typed with Latin or Arabic-Indic digits and either
 * decimal point. The unit's decimals are checked as typed, by the same rule Go applies.
 */
export function QuantityDialog({
  name,
  unitCode,
  decimals,
  onAdd,
  onClose,
}: {
  name: string;
  unitCode: string;
  decimals: number;
  onAdd: (quantity: string) => void;
  onClose: () => void;
}) {
  const { t, tDynamic } = useLocale();
  const [quantity, setQuantity] = useState("");
  const code = typedQuantity(quantity, decimals);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (quantity === "" || code) return;
    onAdd(quantity);
  };

  return (
    <Dialog title={t("till.quantity_title", { name })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <TextField
          label={t("till.quantity_label", { unit: tDynamic(`uom.${unitCode}`) })}
          value={quantity}
          onChange={(e) => setQuantity(e.target.value)}
          error={code ? tDynamic(code, { decimals: String(decimals) }) : undefined}
          inputMode="decimal"
          dir="ltr"
          autoComplete="off"
          required
        />
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={quantity === "" || Boolean(code)}>
            {t("till.add")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
