import { useCallback, useEffect, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { ShopState } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { useShop } from "@/shop/ShopProvider";
import { formErrors } from "@/screens/stock/forms";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { TextField } from "@/ui/Field";
import { Spinner } from "@/ui/Spinner";

// The limits Go holds (settings/domain: MaxPhoneRunes, MaxCityRunes, MaxLineRunes; the shop's name 200).
const MAX_NAME = 200;
const MAX_PHONE = 40;
const MAX_CITY = 60;
const MAX_LINE = 120;

/**
 * Store information (the owner's request, 2026-09-24): what heads every invoice, receipt and report — the shop's name,
 * phone, city and address, and its logo. Nothing about any particular shop is built into the application: each sets its
 * own here. With no logo, the documents set the name in large type — which is what the preview beside the button shows.
 *
 * Choosing the logo file changes nothing; saving it is the owner's act. The two are separate so that the PIN, when it is
 * asked for, retries the save alone and never opens the file dialog a second time.
 */
export function StoreInfo() {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, errorText } = useLocale();
  const [shop, setShop] = useState<ShopState | null>(null);
  const [form, setForm] = useState({ name: "", phone: "", city: "", address: "" });
  const [error, setError] = useState<unknown>(null);
  const [saved, setSaved] = useState(false);
  const [busy, setBusy] = useState(false);

  const header = useShop();
  const adopt = useCallback((next: ShopState) => {
    setShop(next);
    setForm({ name: next.name, phone: next.phone, city: next.city, address: next.address });
  }, []);

  useEffect(() => {
    client.settings
      .shop()
      .then(adopt)
      .catch(setError);
  }, [client, adopt]);

  const act = async (fn: () => Promise<ShopState>) => {
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      const next = await withOwner(fn);
      adopt(next);
      header.adopt(next); // the header shows the new name or logo at once
      setSaved(true);
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  const chooseLogo = async () => {
    setError(null);
    try {
      const file = await client.settings.pickLogoFile();
      if (file.cancelled || file.path === "") return;
      await act(() => client.settings.setLogo(file.path));
    } catch (e) {
      setError(e);
    }
  };

  if (!shop) return error ? <Alert tone="danger" title={errorText(error)} /> : <Spinner label={t("state.loading")} />;

  const errors = formErrors(error, errorText);
  const set = (key: keyof typeof form) => (value: string) => {
    setSaved(false);
    setForm({ ...form, [key]: value });
  };

  return (
    <div className="space-y-3 rounded-lg border border-border bg-surface-raised p-5" data-testid="settings-store">
      <h3 className="font-semibold">{t("settings.store")}</h3>
      <p className="text-xs text-text-muted">{t("settings.store_hint")}</p>

      <div className="flex flex-wrap items-center gap-4 rounded-md border border-border p-3" data-testid="store-logo">
        {shop.logo ? (
          <img
            src={`data:image/png;base64,${shop.logo}`}
            alt={t("settings.logo_alt", { name: shop.name })}
            className="max-h-24 max-w-64 object-contain"
            data-testid="store-logo-image"
          />
        ) : (
          // What the documents print instead: the name, large and bold.
          <p className="text-2xl font-bold" data-testid="store-logo-typeset">
            {form.name || t("app.name")}
          </p>
        )}
        <div className="flex flex-col gap-2">
          <p className="text-xs text-text-muted">{shop.logo ? t("settings.logo_set") : t("settings.logo_none")}</p>
          <div className="flex gap-2">
            <Button onClick={() => void chooseLogo()} disabled={busy} data-testid="store-logo-choose">
              {shop.logo ? t("settings.logo_change") : t("settings.logo_add")}
            </Button>
            {shop.logo ? (
              <Button onClick={() => void act(() => client.settings.removeLogo())} disabled={busy} data-testid="store-logo-remove">
                {t("settings.logo_remove")}
              </Button>
            ) : null}
          </div>
        </div>
      </div>

      <TextField label={t("settings.shop_name")} value={form.name} onChange={(e) => set("name")(e.target.value)} error={errors.field("shopName")} maxLength={MAX_NAME} required />
      <TextField label={t("settings.shop_phone")} value={form.phone} onChange={(e) => set("phone")(e.target.value)} error={errors.field("phone")} maxLength={MAX_PHONE} dir="ltr" inputMode="tel" autoComplete="off" />
      <TextField label={t("settings.shop_city")} value={form.city} onChange={(e) => set("city")(e.target.value)} error={errors.field("city")} maxLength={MAX_CITY} autoComplete="off" />
      <TextField label={t("settings.shop_address")} value={form.address} onChange={(e) => set("address")(e.target.value)} error={errors.field("address")} maxLength={MAX_LINE} autoComplete="off" />

      {errors.form ? <Alert tone="danger" title={errors.form} /> : null}
      {saved ? <Alert tone="success" title={t("settings.store_saved")} testId="store-saved" /> : null}
      <div className="flex justify-end">
        <Button variant="primary" onClick={() => void act(() => client.settings.saveShop(form))} disabled={busy || form.name.trim() === ""} data-testid="store-save">
          {t("settings.store_save")}
        </Button>
      </div>
    </div>
  );
}
