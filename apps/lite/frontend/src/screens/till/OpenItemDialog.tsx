import { useState, type FormEvent } from "react";
import { useLocale } from "@/i18n/LocaleProvider";
import { normaliseNumber } from "@/i18n/numbers";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { TextField } from "@/ui/Field";
import type { Pick } from "./cart";

/**
 * The price of an open-priced item — the carrier bag, the bunch of parsley — typed at the till (2026-09-23).
 *
 * The screen checks only that it reads as a number; Go decides whether it is a price. The amount is in the shop's own
 * reading of its money, and Go takes it back to the books' pounds, as it does every typed amount.
 */
export function OpenItemDialog({
  pick,
  currency,
  onAdd,
  onClose,
}: {
  pick: Pick;
  currency: string;
  onAdd: (price: string, quantity: string) => void;
  onClose: () => void;
}) {
  const { t, tDynamic, locale } = useLocale();
  const [price, setPrice] = useState("");
  const [quantity, setQuantity] = useState("1");
  const name = locale === "en" && pick.nameEn ? pick.nameEn : pick.nameAr;

  // Text checks only — the screen never turns a price into a JavaScript number (DESIGN D9). "Above nothing" is "has a
  // digit other than zero", which is exact for any decimal a person can type.
  const typedPrice = normaliseNumber(price);
  const priceOk = typedPrice.ok && /[1-9]/.test(typedPrice.value);
  const typedQuantity = normaliseNumber(quantity);
  const quantityOk = typedQuantity.ok && /^\d+$/.test(typedQuantity.value) && /[1-9]/.test(typedQuantity.value);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!priceOk || !quantityOk) return;
    onAdd(price.trim(), quantity.trim());
  };

  return (
    <Dialog title={t("till.open_item_title", { name })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit} data-testid="open-item-dialog">
        <p className="text-sm text-text-muted">{t("till.open_item_hint")}</p>
        <TextField
          label={t("till.open_item_price", { currency: tDynamic(`currency.short.${currency}`) })}
          value={price}
          onChange={(e) => setPrice(e.target.value)}
          inputMode="decimal"
          dir="ltr"
          autoFocus
          autoComplete="off"
        />
        <TextField
          label={t("till.open_item_quantity")}
          value={quantity}
          onChange={(e) => setQuantity(e.target.value)}
          inputMode="numeric"
          dir="ltr"
          autoComplete="off"
        />
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={!priceOk || !quantityOk}>
            {t("till.open_item_add")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
