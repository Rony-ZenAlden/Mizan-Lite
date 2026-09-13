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
        onSaved(await client.catalog.createProduct({ nameAr, nameEn, barcode, unitCode, priceCurrency, price }));
        return;
      }
      // Fresh, so the edit carries the current version rather than the one the list showed.
      let current = await client.catalog.product(product.id);
      if (current.nameAr !== nameAr || current.nameEn !== nameEn || current.barcode !== barcode) {
        current = await client.catalog.updateProduct({ id: current.id, rowVersion: current.rowVersion, nameAr, nameEn, barcode });
      }
      if (current.priceCurrency !== priceCurrency || current.price !== typedPrice.value) {
        const version = current.rowVersion;
        current = await withOwner(() => client.catalog.setPrice({ id: current.id, rowVersion: version, priceCurrency, price }));
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
            onChange={(e) => setPrice(e.target.value)}
            error={priceHint ?? fieldError("price")}
            hint={product ? t("product.price_hint") : undefined}
            inputMode="decimal"
            dir="ltr"
            required
          />
        </div>
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
