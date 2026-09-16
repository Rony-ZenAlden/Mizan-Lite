import { useEffect, useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Currency, Product, Unit } from "@/api/client";
import { BindingError } from "@/api/envelope";
import { useLocale } from "@/i18n/LocaleProvider";
import { normaliseNumber } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Button } from "@/ui/Button";
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
  const [priceCurrency, setPriceCurrency] = useState(product?.priceCurrency ?? "SYP");
  const [price, setPrice] = useState(product?.price ?? "");
  // The cost price, and the margin between it and the selling price (L9). `lastTyped` remembers which box the shopkeeper
  // touched last, because that is the one Go should work the other two out from — and only Go does that arithmetic.
  const [costPrice, setCostPrice] = useState(product?.costPrice ?? "");
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
    if (!typedPrice.ok) return;
    setBusy(true);
    setError(null);
    try {
      if (!product) {
        onSaved(
          await client.catalog.createProduct({
            nameAr, nameEn, barcode, unitCode, priceCurrency, price,
            costPrice,
            marginPercent: lastTyped === "percent" ? marginPercent : "",
            marginAmount: lastTyped === "amount" ? marginAmount : "",
          }),
        );
        return;
      }
      // Fresh, so the edit carries the current version rather than the one the list showed.
      let current = await client.catalog.product(product.id);
      if (current.nameAr !== nameAr || current.nameEn !== nameEn || current.barcode !== barcode) {
        current = await client.catalog.updateProduct({ id: current.id, rowVersion: current.rowVersion, nameAr, nameEn, barcode });
      }
      // The cost travels with the price: both live on the product, and one call keeps them consistent. "-" takes a cost
      // off; "" leaves whatever is stored alone.
      const cost = costPrice === "" && current.costPrice !== "" ? "-" : costPrice;
      const marginChanged = (lastTyped === "percent" && marginPercent !== "") || (lastTyped === "amount" && marginAmount !== "");
      if (current.priceCurrency !== priceCurrency || current.price !== typedPrice.value || cost !== "" || marginChanged) {
        const version = current.rowVersion;
        current = await withOwner(() =>
          client.catalog.setPrice({
            id: current.id, rowVersion: version, priceCurrency, price,
            costPrice: cost,
            marginPercent: lastTyped === "percent" ? marginPercent : "",
            marginAmount: lastTyped === "amount" ? marginAmount : "",
          }),
        );
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
        <div className="grid grid-cols-2 gap-3">
          <SelectField label={t("product.currency")} value={priceCurrency} onChange={(e) => setPriceCurrency(e.target.value)} error={fieldError("priceCurrency")}>
            {currencies.map((c) => (
              <option key={c.code} value={c.code}>
                {tDynamic(`currency.${c.code}`)}
              </option>
            ))}
          </SelectField>
          <TextField
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
          />
        </div>

        {/* The cost price and the margin (L9). Go does the arithmetic on save: the form never computes money. */}
        <div className="space-y-3 rounded-md border border-border p-3" data-testid="product-cost">
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
        </div>
        {product && units.find((u) => u.code === product.unitCode)?.kind === "count" ? (
          <PackageSection product={product} units={units} />
        ) : null}
        {formError ? (
          <p role="alert" className="text-sm text-danger">
            {formError}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || !typedPrice.ok}>
            {t("action.save")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
