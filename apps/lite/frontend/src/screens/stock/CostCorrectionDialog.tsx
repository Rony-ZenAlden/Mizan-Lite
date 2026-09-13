import { useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Product, StockLevel } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { TextField } from "@/ui/Field";
import { formErrors, typedAmount } from "./forms";

/** The owner sets a product's average cost, with a reason — for a cost typed wrongly after stock has moved (L2 §5.6). */
export function CostCorrectionDialog({
  product,
  name,
  onDone,
  onClose,
}: {
  product: Product;
  name: string;
  onDone: (level: StockLevel) => void;
  onClose: () => void;
}) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText } = useLocale();
  const [averageCost, setAverageCost] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const code = typedAmount(averageCost);
  const errors = formErrors(error, errorText);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onDone(await withOwner(() => client.stock.correctCost({ productId: product.id, averageCost, note })));
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t("stock.correct_title", { name })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <TextField
          label={t("stock.average_cost")}
          value={averageCost}
          onChange={(e) => setAverageCost(e.target.value)}
          error={(code ? tDynamic(code) : undefined) ?? errors.field("cost")}
          hint={t("stock.correct_hint")}
          inputMode="decimal"
          dir="ltr"
          required
        />
        <TextField label={t("stock.note_required")} value={note} onChange={(e) => setNote(e.target.value)} error={errors.field("note")} required maxLength={200} />
        {errors.form ? (
          <p role="alert" className="text-sm text-danger">
            {errors.form}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || averageCost === "" || Boolean(code) || note.trim() === ""}>
            {t("action.save")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
