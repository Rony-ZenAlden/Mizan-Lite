import { useCallback, useEffect, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { SettingsState } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { SelectField, TextField } from "@/ui/Field";
import { Spinner } from "@/ui/Spinner";
import { usePrinterSettings } from "@/printing/PrintPanel";
import { Link } from "react-router-dom";
import { StoreInfo } from "./StoreInfo";

/** Where a fetched rate comes from, as the screen offers it. "manual" is the rate mode, not a fetch source. */
type Source = "standard" | "local" | "manual";

/**
 * The shop's settings in one place (the owner's request, 2026-09-17): what used to be spread across the header's language
 * buttons, the Rates screen and the Cash screen.
 *
 * Groups in the order a shop thinks about them: the language, then the store's own information — its name, phone, city,
 * address and logo, which head every document (0.10.0) — then its currency, then where the exchange rate comes from,
 * then who may do what. Each group says plainly where the settings it does NOT hold are
 * set, rather than quietly duplicating a control that lives somewhere else — two places to change one figure is how the
 * two disagree.
 *
 * Since 2026-09-17 the language lives ONLY here: the header's two buttons are gone, which is why this screen's first
 * group is where a shop now looks for them.
 */
export function SettingsScreen() {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText, locale, setLocale } = useLocale();
  const [settings, setSettings] = useState<SettingsState | null>(null);
  const [rateMode, setRateMode] = useState<"automatic" | "manual">("automatic");
  const [source, setSource] = useState<Source>("standard");
  const [url, setUrl] = useState("");
  const [field, setField] = useState("");
  const [moneyDisplay, setMoneyDisplay] = useState<"legacy" | "new" | "dual">("legacy");
  const [pinRequired, setPinRequired] = useState(false);
  // Printing lives on its own screen (the printer, the paper, the header); what belongs HERE is the one decision a
  // shop makes about it — whether a finished sale prints itself (2026-09-20).
  const printer = usePrinterSettings();
  const [autoPrint, setAutoPrint] = useState<"none" | "credit" | "all">("none");
  const [autoPrintLoaded, setAutoPrintLoaded] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [saved, setSaved] = useState(false);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const [current, rate] = await Promise.all([client.settings.get(), client.fx.current()]);
      setSettings(current);
      setMoneyDisplay((current.moneyDisplay || "legacy") as "legacy" | "new" | "dual");
      setPinRequired(current.pinRequired);
      setUrl(current.localRateUrl);
      setField(current.localRateField);
      setRateMode(rate.mode === "manual" ? "manual" : "automatic");
      // Manual mode is a source as far as the shop is concerned: it is where the rate comes from.
      setSource(rate.mode === "manual" ? "manual" : (current.rateSource as Source));
      setError(null);
    } catch (e) {
      setError(e);
    }
  }, [client]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (printer && !autoPrintLoaded) {
      setAutoPrint((printer.autoPrint || "none") as "none" | "credit" | "all");
      setAutoPrintLoaded(true);
    }
  }, [printer, autoPrintLoaded]);

  const save = async () => {
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      // The rate's source is two settings at once: the mode decides whether anything is fetched at all, and the source
      // decides which endpoint is asked first when it is.
      if (source === "manual" && rateMode !== "manual") {
        await withOwner(() => client.fx.setMode("manual"));
      } else if (source !== "manual") {
        if (rateMode === "manual") {
          await withOwner(() => client.fx.setMode("automatic"));
        }
        await withOwner(() =>
          client.settings.update({ rateSource: source, localRateUrl: url, localRateField: field }),
        );
      }
      if (moneyDisplay !== settings?.moneyDisplay) {
        await withOwner(() => client.settings.update({ moneyDisplay }));
      }
      // Turning the PIN ON is free; turning it OFF asks for it, because Go guards the switch against the setting as it
      // stands rather than the one being written.
      if (pinRequired !== settings?.pinRequired) {
        await withOwner(() => client.settings.update({ pinRequired }));
      }
      if (printer && autoPrint !== printer.autoPrint) {
        await withOwner(() => client.printers.save({ ...printer, paperMm: String(printer.paperMm), autoPrint }));
      }
      await load();
      setSaved(true);
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  if (!settings) {
    return error ? <Alert tone="danger" title={errorText(error)} /> : <Spinner label={t("state.loading")} />;
  }

  return (
    <section className="mx-auto max-w-3xl space-y-4">
      <h2 className="text-xl font-semibold">{t("settings.title")}</h2>
      {error ? <Alert tone="danger" title={errorText(error)} /> : null}
      {saved ? <Alert tone="success" title={t("settings.saved")} /> : null}

      <div className="space-y-3 rounded-lg border border-border bg-surface-raised p-5" data-testid="settings-general">
        <h3 className="font-semibold">{t("settings.general")}</h3>
        <SelectField label={t("settings.language")} value={locale} onChange={(e) => void setLocale(e.target.value as "ar" | "en")}>
          {/* Each language names itself, in itself, marked with its own tag so a screen reader says it that way. */}
          <option value="ar" lang="ar">
            {t("language.ar")}
          </option>
          <option value="en" lang="en">
            {t("language.en")}
          </option>
        </SelectField>
      </div>

      <StoreInfo />

      <div className="space-y-2 rounded-lg border border-border bg-surface-raised p-5" data-testid="settings-currency">
        <h3 className="font-semibold">{t("settings.currency")}</h3>
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 text-sm">
          <dt className="text-text-muted">{t("settings.local_currency")}</dt>
          <dd>{tDynamic(`currency.${settings.localCurrency}`)}</dd>
          <dt className="text-text-muted">{t("settings.cash_note")}</dt>
          <dd>
            <bdi dir="ltr">{settings.cashNote}</bdi> {tDynamic(`currency.short.${settings.localCurrency}`)}
          </dd>
          <dt className="text-text-muted">{t("settings.debt_currency")}</dt>
          <dd>{tDynamic(`currency.${settings.debtCurrency}`)}</dd>
        </dl>
        <p className="text-xs text-text-muted">{t("settings.currency_note")}</p>

        {/* Dropping the two noughts (L10): what the shop sees, and what it types. */}
        <div className="space-y-2 border-t border-border pt-3" data-testid="money-display">
          <SelectField
            label={t("settings.money_display")}
            value={moneyDisplay}
            onChange={(e) => setMoneyDisplay(e.target.value as "legacy" | "new" | "dual")}
          >
            {(["legacy", "new", "dual"] as const).map((name) => (
              <option key={name} value={name}>
                {t(`settings.money_display.${name}` as "settings.money_display.legacy")}
              </option>
            ))}
          </SelectField>
          <p className="text-xs text-text-muted">{t("settings.money_display_hint")}</p>
          {moneyDisplay !== "legacy" ? (
            <p className="text-xs text-text-muted" data-testid="money-display-typing">
              {t("settings.money_display_typing")}
            </p>
          ) : null}
        </div>
      </div>

      <div className="space-y-3 rounded-lg border border-border bg-surface-raised p-5" data-testid="settings-rate">
        <h3 className="font-semibold">{t("settings.rate")}</h3>
        <SelectField label={t("settings.rate_source")} value={source} onChange={(e) => setSource(e.target.value as Source)}>
          {(["standard", "local", "manual"] as const).map((name) => (
            <option key={name} value={name}>
              {t(`settings.rate_source.${name}` as "settings.rate_source.standard")}
            </option>
          ))}
        </SelectField>
        {source === "local" ? (
          <div className="space-y-3" data-testid="settings-local">
            <p className="text-xs text-text-muted" data-testid="settings-local-builtin">
              {t("settings.local_builtin")}
            </p>
            <TextField
              label={t("settings.local_url")}
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              hint={t("settings.local_url_hint")}
              dir="ltr"
              autoComplete="off"
            />
            <TextField
              label={t("settings.local_field")}
              value={field}
              onChange={(e) => setField(e.target.value)}
              hint={t("settings.local_field_hint")}
              dir="ltr"
              autoComplete="off"
            />
          </div>
        ) : null}
        <p className="text-xs text-text-muted">{t("settings.rate_on_rates_screen")}</p>
      </div>

      <div className="space-y-2 rounded-lg border border-border bg-surface-raised p-5" data-testid="settings-printing">
        <h3 className="font-semibold">{t("settings.printing")}</h3>
        <p className="text-sm text-text-muted" data-testid="settings-printer-state">
          {printer && printer.printer !== "" ? t("settings.printer_on", { name: printer.printer }) : t("settings.printer_none")}
        </p>
        <SelectField
          label={t("settings.silent")}
          value={autoPrint}
          onChange={(e) => setAutoPrint(e.target.value as "none" | "credit" | "all")}
          hint={t("settings.silent_hint")}
          disabled={!printer || printer.printer === ""}
          data-testid="silent-printing"
        >
          <option value="none">{t("settings.silent.none")}</option>
          <option value="credit">{t("settings.silent.credit")}</option>
          <option value="all">{t("settings.silent.all")}</option>
        </SelectField>
        <p className="text-xs text-text-muted">
          <Link to="/printer" className="underline">
            {t("settings.printer_screen")}
          </Link>
        </p>
      </div>

      <div className="space-y-2 rounded-lg border border-border bg-surface-raised p-5" data-testid="settings-security">
        <h3 className="font-semibold">{t("settings.security")}</h3>
        <Checkbox
          label={t("settings.pin_required")}
          checked={pinRequired}
          data-testid="pin-required"
          onChange={(e) => setPinRequired(e.target.checked)}
        />
        <p className="text-xs text-text-muted">{t("settings.pin_required_hint")}</p>
        <p className="text-xs text-text-muted">{t("settings.pin_required_always")}</p>
      </div>

      <div className="flex justify-end">
        <Button variant="primary" onClick={() => void save()} disabled={busy}>
          {t("settings.save")}
        </Button>
      </div>
    </section>
  );
}
