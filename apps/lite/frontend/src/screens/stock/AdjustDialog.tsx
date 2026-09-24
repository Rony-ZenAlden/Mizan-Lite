import { useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Product, StockLevel, Unit } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { SelectField, TextField } from "@/ui/Field";
import { formErrors, typedQuantity } from "./forms";

/** The adjustment reasons, in display order (Q-L2.6); mirrors stock/domain.AdjustmentReasons. */
export const ADJUSTMENT_REASONS = ["damaged", "expired", "spoiled", "own_use", "gift", "other"] as const;

/** The write-offs the spoilage screen records (0.10.0); mirrors stock/domain.SpoilageReasons. */
export const SPOILAGE_REASONS = ["damaged", "expired", "spoiled"] as const;

/** A write-off, or stock found. Taking stock out needs the owner (Q-L2.3); "other" needs a note. */
export function AdjustDialog({
  product,
  name,
  unit,
  onDone,
  onClose,
}: {
  product: Product;
  name: string;
  unit: Unit;
  onDone: (level: StockLevel) => void;
  onClose: () => void;
}) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText } = useLocale();
  const [direction, setDirection] = useState<"out" | "in">("out");
  const [quantity, setQuantity] = useState("");
  const [reason, setReason] = useState<(typeof ADJUSTMENT_REASONS)[number]>("damaged");
  const [note, setNote] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const code = typedQuantity(quantity, unit.inputDecimals);
  const noteRequired = reason === "other";
  const errors = formErrors(error, errorText);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onDone(await withOwner(() => client.stock.adjust({ productId: product.id, direction, quantity, reason, note })));
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t("stock.adjust_title", { name })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <SelectField label={t("stock.direction")} value={direction} onChange={(e) => setDirection(e.target.value as "out" | "in")} hint={t("stock.adjust_hint")}>
          <option value="out">{t("stock.direction_out")}</option>
          <option value="in">{t("stock.direction_in")}</option>
        </SelectField>
        <TextField
          label={t("stock.quantity", { unit: tDynamic(`uom.${unit.code}`) })}
          value={quantity}
          onChange={(e) => setQuantity(e.target.value)}
          error={(code ? tDynamic(code, { decimals: String(unit.inputDecimals) }) : undefined) ?? errors.field("quantity")}
          inputMode="decimal"
          dir="ltr"
          required
        />
        <SelectField label={t("stock.reason")} value={reason} onChange={(e) => setReason(e.target.value as typeof reason)} error={errors.field("reason")}>
          {ADJUSTMENT_REASONS.map((r) => (
            <option key={r} value={r}>
              {tDynamic(`stock.reason.${r}`)}
            </option>
          ))}
        </SelectField>
        <TextField
          label={t(noteRequired ? "stock.note_required" : "stock.note")}
          value={note}
          onChange={(e) => setNote(e.target.value)}
          error={errors.field("note")}
          required={noteRequired}
          maxLength={200}
        />
        {errors.form ? (
          <p role="alert" className="text-sm text-danger">
            {errors.form}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || quantity === "" || Boolean(code) || (noteRequired && note.trim() === "")}>
            {t("action.save")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
