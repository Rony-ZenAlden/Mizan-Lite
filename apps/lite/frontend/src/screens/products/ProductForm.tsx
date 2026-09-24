import { useEffect, useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Currency, Product, Unit } from "@/api/client";
import { BindingError } from "@/api/envelope";
import { useLocale } from "@/i18n/LocaleProvider";
import { useCurrencies } from "@/rates/useCurrencies";
import { normaliseNumber, typeable } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { Dialog } from "@/ui/Dialog";
import { SelectField, TextField } from "@/ui/Field";
import { PackageSection } from "./PackageSection";

/**
 * Create a product, or edit one.
 *
 * Editing is up to three acts, sent only when they change something: names and barcode (open), and the
 * price (owner only, through withOwner). The unit is shown and fixed — it cannot change after creation.
 */
export function ProductForm({ product, onSaved, onClose }: { product?: Product; onSaved: (p: Product) => void; onClose: () => void }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText } = useLocale();
  const [units, setUnits] = useState<Unit[]>([]);
  const [currencies, setCurrencies] = useState<Currency[]>([]);
  const [nameAr, setNameAr] = useState(product?.nameAr ?? "");
  const [nameEn, setNameEn] = useState(product?.nameEn ?? "");
  const [barcode, setBarcode] = useState(product?.barcode ?? "");
  const [unitCode, setUnitCode] = useState(product?.unitCode ?? "");
  // A dollars-only shop prices in dollars (0.10.0).
  const { usdOnly } = useCurrencies();
  const [priceCurrency, setPriceCurrency] = useState(product?.priceCurrency ?? (usdOnly ? "USD" : "SYP"));
  // In dual mode a price reads "150 (15000)"; the field starts from the figure a person types, the new one (0.9.9).
  const [price, setPrice] = useState(typeable(product?.price ?? ""));
  // The cost price, and the margin between it and the selling price (L9). `lastTyped` remembers which box the shopkeeper
  // touched last, because that is the one Go should work the other two out from — and only Go does that arithmetic.
  const [costPrice, setCostPrice] = useState(typeable(product?.costPrice ?? ""));
  // A supplier's discount off the cost typed, as a percentage (0.10.0). Never stored: Go takes it off the cost typed with
  // it, and the cost kept — the one this form shows next time — is what the shop really pays.
  const [costDiscount, setCostDiscount] = useState("");
  // The level at or below which the shop wants to be told to buy more (2026-09-20). Empty means never tell me.
  const [reorderLevel, setReorderLevel] = useState(product?.reorderLevel ?? "");
  // How many of the unit make one carton (طرد) — what the invoice's carton column counts in (0.10.0). Empty for none.
  const [unitsPerCarton, setUnitsPerCarton] = useState(product?.unitsPerCarton ?? "");
  // An item whose price is typed at the till and which is never counted (2026-09-23). Chosen at creation only: the
  // schema refuses to change it afterwards, because a stocked product turned open would strand its stock.
  const [openPrice, setOpenPrice] = useState(product?.openPrice ?? false);
  const [marginPercent, setMarginPercent] = useState("");
  const [marginAmount, setMarginAmount] = useState("");
  const [lastTyped, setLastTyped] = useState<"price" | "percent" | "amount">("price");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    Promise.all([client.catalog.units(), client.catalog.currencies()])
      .then(([u, c]) => {
        if (cancelled) return;
        setUnits(u);
        setCurrencies(c);
        setUnitCode((current) => current || u[0]?.code || "");
      })
      .catch((e) => {
        if (!cancelled) setError(e);
      });
    return () => {
      cancelled = true;
    };
  }, [client]);

  const typedPrice = normaliseNumber(price);
  const priceHint = price !== "" && !typedPrice.ok ? tDynamic(typedPrice.code) : undefined;

  const fieldError = (field: string) =>
    error instanceof BindingError && error.apiError.fields?.some((f) => f.field === field) ? errorText(error) : undefined;
  const duplicateInactive =
    error instanceof BindingError && error.code === "lite.catalog.duplicate_name" && error.apiError.params?.existingActive === "false";
  const formError =
    error && !(error instanceof BindingError && error.apiError.fields?.length) ? errorText(error) : null;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!openPrice && !typedPrice.ok) return;
    setBusy(true);
    setError(null);
    try {
      if (!product) {
        onSaved(
          await client.catalog.createProduct(
            openPrice
              ? { nameAr, nameEn, barcode, unitCode, priceCurrency, price: "", costPrice: "", costDiscount: "", marginPercent: "", marginAmount: "", openPrice: true,
                  unitsPerCarton: unitsPerCarton.trim() }
              : {
                  nameAr, nameEn, barcode, unitCode, priceCurrency, price,
                  costPrice,
                  costDiscount: costDiscount.trim(),
                  marginPercent: lastTyped === "percent" ? marginPercent : "",
                  marginAmount: lastTyped === "amount" ? marginAmount : "",
                  openPrice: false,
                  unitsPerCarton: unitsPerCarton.trim(),
                },
          ),
        );
        return;
      }
      // Fresh, so the edit carries the current version rather than the one the list showed.
      let current = await client.catalog.product(product.id);
      if (current.nameAr !== nameAr || current.nameEn !== nameEn || current.barcode !== barcode || current.unitsPerCarton !== unitsPerCarton.trim()) {
        current = await client.catalog.updateProduct({ id: current.id, rowVersion: current.rowVersion, nameAr, nameEn, barcode,
          unitsPerCarton: unitsPerCarton.trim() });
      }
      // The cost travels with the price: both live on the product, and one call keeps them consistent. "-" takes a cost
      // off; "" leaves whatever is stored alone.
      // A discount always travels with the cost it comes off, the one on screen, so it is never taken off a stored cost twice.
      const discounting = costDiscount.trim() !== "" && costPrice !== "";
      const cost = discounting ? costPrice : costPrice === "" && current.costPrice !== "" ? "-" : costPrice === typeable(current.costPrice) ? "" : costPrice;
      const marginChanged = (lastTyped === "percent" && marginPercent !== "") || (lastTyped === "amount" && marginAmount !== "");
      // An open-priced item has no price, cost or reorder level of its own: only its name and barcode are edited here.
      const typedValue = typedPrice.ok ? typedPrice.value : "";
      if (!current.openPrice && (current.priceCurrency !== priceCurrency || typeable(current.price) !== typedValue || cost !== "" || marginChanged)) {
        const version = current.rowVersion;
        current = await withOwner(() =>
          client.catalog.setPrice({
            id: current.id, rowVersion: version, priceCurrency, price,
            costPrice: cost,
            costDiscount: discounting ? costDiscount.trim() : "",
            marginPercent: lastTyped === "percent" ? marginPercent : "",
            marginAmount: lastTyped === "amount" ? marginAmount : "",
          }),
        );
      }
      if (!current.openPrice && reorderLevel.trim() !== (product?.reorderLevel ?? "")) {
        // Not an owner's act: it changes no price and no quantity, only when a badge appears.
        current = await client.catalog.setReorder({ id: current.id, rowVersion: current.rowVersion, level: reorderLevel.trim() });
      }
      onSaved(current);
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t(product ? "product.edit_title" : "product.new_title")} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <TextField label={t("product.name_ar")} value={nameAr} onChange={(e) => setNameAr(e.target.value)} error={fieldError("nameAr")} required maxLength={200} dir="rtl" />
        {duplicateInactive ? <p className="text-xs text-text-muted">{t("products.duplicate_inactive")}</p> : null}
        <TextField label={t("product.name_en")} value={nameEn} onChange={(e) => setNameEn(e.target.value)} error={fieldError("nameEn")} maxLength={200} dir="ltr" />
        <TextField label={t("product.barcode")} value={barcode} onChange={(e) => setBarcode(e.target.value)} error={fieldError("barcode")} maxLength={64} dir="ltr" autoComplete="off" />
        {openPrice ? null : <TextField
          label={t("catalog.reorder")}
          hint={t("catalog.reorder_hint")}
          value={reorderLevel}
          onChange={(e) => setReorderLevel(e.target.value)}
          error={fieldError("reorderLevel")}
          inputMode="decimal"
          dir="ltr"
          autoComplete="off"
        />}
        <TextField
          label={t("product.units_per_carton")}
          hint={t("product.units_per_carton_hint")}
          value={unitsPerCarton}
          onChange={(e) => setUnitsPerCarton(e.target.value)}
          error={fieldError("unitsPerCarton")}
          inputMode="decimal"
          dir="ltr"
          autoComplete="off"
          data-testid="product-units-per-carton"
        />
        <SelectField
          label={t("product.unit")}
          value={unitCode}
          onChange={(e) => setUnitCode(e.target.value)}
          disabled={Boolean(product)}
          hint={product ? t("product.unit_fixed") : undefined}
          error={fieldError("unitCode")}
        >
          {units.map((u) => (
            <option key={u.code} value={u.code}>
              {tDynamic(`uom.${u.code}`)}
            </option>
          ))}
        </SelectField>
        {!product ? (
          <div className="space-y-1" data-testid="product-open-price">
            <Checkbox label={t("product.open_price")} checked={openPrice} onChange={(e) => setOpenPrice(e.target.checked)} />
            <p className="text-xs text-text-muted">{t("product.open_price_hint")}</p>
          </div>
        ) : openPrice ? (
          <p className="text-sm text-text-muted" data-testid="product-open-price-note">
            {t("product.open_price_note")}
          </p>
        ) : null}
        <div className="grid grid-cols-2 gap-3">
          <SelectField label={t("product.currency")} value={priceCurrency} onChange={(e) => setPriceCurrency(e.target.value)} error={fieldError("priceCurrency")}>
            {currencies.filter((c) => !usdOnly || c.code === "USD").map((c) => (
              <option key={c.code} value={c.code}>
                {tDynamic(`currency.${c.code}`)}
              </option>
            ))}
          </SelectField>
          {openPrice ? null : <TextField
            label={t("product.price")}
            value={price}
            onChange={(e) => {
              setPrice(e.target.value);
              setLastTyped("price");
            }}
            error={priceHint ?? fieldError("price")}
            hint={product ? t("product.price_hint") : undefined}
            inputMode="decimal"
            dir="ltr"
            required
          />}
        </div>

        {/* The cost price and the margin (L9). Go does the arithmetic on save: the form never computes money. */}
        {openPrice ? null : <div className="space-y-3 rounded-md border border-border p-3" data-testid="product-cost">
          <TextField
            label={t("products.cost_price")}
            value={costPrice}
            onChange={(e) => setCostPrice(e.target.value)}
            error={fieldError("costPrice")}
            hint={t("products.cost_hint")}
            inputMode="decimal"
            dir="ltr"
            autoComplete="off"
          />
          <TextField
            label={t("products.cost_discount")}
            value={costDiscount}
            onChange={(e) => setCostDiscount(e.target.value)}
            error={fieldError("costDiscount")}
            hint={t("products.cost_discount_hint")}
            inputMode="decimal"
            dir="ltr"
            autoComplete="off"
            disabled={costPrice === ""}
          />
          <div className="grid grid-cols-2 gap-3">
            <TextField
              label={t("products.margin_percent")}
              value={marginPercent}
              onChange={(e) => {
                setMarginPercent(e.target.value);
                setLastTyped("percent");
              }}
              error={lastTyped === "percent" ? fieldError("margin") : undefined}
              inputMode="decimal"
              dir="ltr"
              autoComplete="off"
              disabled={costPrice === ""}
            />
            <TextField
              label={t("products.margin_amount")}
              value={marginAmount}
              onChange={(e) => {
                setMarginAmount(e.target.value);
                setLastTyped("amount");
              }}
              error={lastTyped === "amount" ? fieldError("margin") : undefined}
              inputMode="decimal"
              dir="ltr"
              autoComplete="off"
              disabled={costPrice === ""}
            />
          </div>
          <p className="text-xs text-text-muted">{t("products.margin_hint")}</p>
          {product?.marginPercent ? (
            <p className="text-sm" data-testid="product-margin">
              {t("products.margin")}: <bdi dir="ltr">{product.marginAmount}</bdi> ·{" "}
              <bdi dir="ltr">{product.marginPercent}%</bdi>
              {product.marginAmount.startsWith("-") ? <span className="ms-2 text-danger">{t("products.margin_loss")}</span> : null}
            </p>
          ) : null}
        </div>}
        {product && !product.openPrice && units.find((u) => u.code === product.unitCode)?.kind === "count" ? (
          <PackageSection product={product} units={units} />
        ) : null}
        {formError ? (
          <p role="alert" className="text-sm text-danger">
            {formError}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || (!openPrice && !typedPrice.ok)}>
            {t("action.save")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
