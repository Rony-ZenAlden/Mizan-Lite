import { useEffect, useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Product, Unit } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { SPOILAGE_REASONS } from "@/screens/stock/AdjustDialog";
import { formErrors, typedQuantity } from "@/screens/stock/forms";
import { useProductSearch } from "@/screens/stock/useProductSearch";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { SelectField, TextField } from "@/ui/Field";

type SpoilageReason = (typeof SPOILAGE_REASONS)[number];

/**
 * Stock lost in the shop (0.10.0, the owner's request of 2026-09-24): an item found, how much of it went, and why —
 * damaged, expired or spoiled. It is the stock book's own write-off, so it comes off the shelf at its average cost and
 * needs the owner, like every write-off (Q-L2.3).
 */
export function SpoilageDialog({ onDone, onClose }: { onDone: (message: string) => void; onClose: () => void }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [units, setUnits] = useState<Unit[]>([]);
  const [product, setProduct] = useState<Product | null>(null);
  const [quantity, setQuantity] = useState("");
  const [reason, setReason] = useState<SpoilageReason>("spoiled");
  const [note, setNote] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const finder = useProductSearch(setProduct);

  useEffect(() => {
    let cancelled = false;
    client.catalog
      .units()
      .then((all) => {
        if (!cancelled) setUnits(all);
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e);
      });
    return () => {
      cancelled = true;
    };
  }, [client]);

  const name = (p: Product) => (locale === "en" && p.nameEn ? p.nameEn : p.nameAr);
  const unit = units.find((u) => u.code === product?.unitCode);
  const unitName = product ? tDynamic(`uom.${product.unitCode}`) : "";
  const code = unit ? typedQuantity(quantity, unit.inputDecimals) : null;
  const errors = formErrors(error ?? finder.error, errorText);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!product) return;
    setBusy(true);
    setError(null);
    try {
      await withOwner(() => client.stock.adjust({ productId: product.id, direction: "out", quantity, reason, note }));
      onDone(t("spoilage.recorded", { quantity, unit: unitName, name: name(product) }));
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t("spoilage.title")} onClose={onClose}>
      <form className="space-y-3" onSubmit={(e) => void submit(e)} aria-label={t("spoilage.title")}>
        {product ? (
          <div className="flex items-center justify-between gap-2 rounded-md border border-border p-2 text-sm" data-testid="spoilage-item">
            <span className="font-medium">
              {name(product)} — {unitName}
            </span>
            <Button onClick={() => setProduct(null)}>{t("spoilage.change")}</Button>
          </div>
        ) : (
          <div className="space-y-2">
            <TextField
              label={t("spoilage.find")}
              value={finder.search}
              onChange={(e) => finder.setSearch(e.target.value)}
              onKeyDown={finder.onKeyDown}
              type="search"
              autoComplete="off"
            />
            {finder.search.trim() !== "" && finder.loaded ? (
              finder.matches.length === 0 ? (
                <p className="text-sm text-text-muted">{t("spoilage.no_match")}</p>
              ) : (
                <ul className="flex flex-wrap gap-2" aria-label={t("spoilage.matches")}>
                  {finder.matches.map((p) => (
                    <li key={p.id}>
                      <Button onClick={() => finder.pick(p)}>
                        {name(p)} — {tDynamic(`uom.${p.unitCode}`)}
                      </Button>
                    </li>
                  ))}
                </ul>
              )
            ) : null}
          </div>
        )}
        <TextField
          label={t("spoilage.quantity", { unit: unitName })}
          value={quantity}
          onChange={(e) => setQuantity(e.target.value)}
          error={(code ? tDynamic(code, { decimals: String(unit?.inputDecimals ?? 0) }) : undefined) ?? errors.field("quantity")}
          inputMode="decimal"
          dir="ltr"
          autoComplete="off"
          disabled={!product}
          required
        />
        <SelectField label={t("spoilage.reason")} value={reason} onChange={(e) => setReason(e.target.value as SpoilageReason)} error={errors.field("reason")}>
          {SPOILAGE_REASONS.map((r) => (
            <option key={r} value={r}>
              {tDynamic(`stock.reason.${r}`)}
            </option>
          ))}
        </SelectField>
        <TextField label={t("spoilage.note")} value={note} onChange={(e) => setNote(e.target.value)} error={errors.field("note")} maxLength={200} autoComplete="off" />
        <p className="text-xs text-text-muted">{t("spoilage.hint")}</p>
        {errors.form ? (
          <p role="alert" className="text-sm text-danger">
            {errors.form}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || !product || quantity.trim() === "" || Boolean(code)}>
            {t("spoilage.save")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
