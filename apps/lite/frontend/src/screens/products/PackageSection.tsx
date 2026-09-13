import { useEffect, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { Product, Unit } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { Button } from "@/ui/Button";
import { SelectField, TextField } from "@/ui/Field";
import { formErrors, typedQuantity } from "@/screens/stock/forms";

/**
 * What a package product opens into — a 16-litre tin into loose olive oil (L2 §3.5). One level only: Go refuses a
 * link that would nest (Q-L2.8). Saved on its own, apart from the form's names and price.
 */
export function PackageSection({ product, units }: { product: Product; units: Unit[] }) {
  const client = useClient();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [candidates, setCandidates] = useState<Product[]>([]);
  const [contentId, setContentId] = useState(product.packageContentId);
  const [quantity, setQuantity] = useState(product.packageContentQuantity);
  const [linked, setLinked] = useState(product.packageContentId !== "");
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    client.catalog
      .products({ text: "", includeInactive: false })
      .then((all) => {
        if (!cancelled) setCandidates(all.filter((p) => p.id !== product.id));
      })
      .catch((e) => {
        if (!cancelled) setError(e);
      });
    return () => {
      cancelled = true;
    };
  }, [client, product.id]);

  const content = candidates.find((p) => p.id === contentId);
  const contentUnit = units.find((u) => u.code === content?.unitCode);
  const code = contentUnit ? typedQuantity(quantity, contentUnit.inputDecimals) : null;
  const errors = formErrors(error, errorText);
  const name = (p: Product) => (locale === "en" && p.nameEn ? p.nameEn : p.nameAr);

  const run = async (act: () => Promise<Product>) => {
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      const next = await act();
      setLinked(next.packageContentId !== "");
      setContentId(next.packageContentId);
      setQuantity(next.packageContentQuantity);
      setSaved(true);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <fieldset className="space-y-3 rounded-md border border-border p-3">
      <legend className="px-1 text-sm font-medium">{t("product.package_title")}</legend>
      <p className="text-xs text-text-muted">{t("product.package_hint")}</p>
      <SelectField label={t("product.package_content")} value={contentId} onChange={(e) => setContentId(e.target.value)} error={errors.field("contentProductId")}>
        <option value="">{t("product.package_none")}</option>
        {candidates.map((p) => (
          <option key={p.id} value={p.id}>
            {name(p)} — {tDynamic(`uom.${p.unitCode}`)}
          </option>
        ))}
      </SelectField>
      <TextField
        label={t("product.package_quantity")}
        value={quantity}
        onChange={(e) => setQuantity(e.target.value)}
        error={(code ? tDynamic(code, { decimals: String(contentUnit?.inputDecimals ?? 0) }) : undefined) ?? errors.field("contentQuantity")}
        inputMode="decimal"
        dir="ltr"
      />
      {errors.form ? (
        <p role="alert" className="text-sm text-danger">
          {errors.form}
        </p>
      ) : null}
      {saved ? (
        <p role="status" className="text-sm text-success">
          {t("product.package_saved")}
        </p>
      ) : null}
      <div className="flex justify-end gap-2">
        {linked ? (
          <Button disabled={busy} onClick={() => void run(() => client.catalog.clearPackage(product.id))}>
            {t("product.package_clear")}
          </Button>
        ) : null}
        <Button
          disabled={busy || contentId === "" || quantity === "" || Boolean(code)}
          onClick={() => void run(() => client.catalog.setPackage({ packageProductId: product.id, contentProductId: contentId, contentQuantity: quantity }))}
        >
          {t("product.package_save")}
        </Button>
      </div>
    </fieldset>
  );
}
