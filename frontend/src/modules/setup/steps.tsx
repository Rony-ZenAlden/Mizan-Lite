import type { ReactNode } from "react";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import type { SetupOptions } from "@/lib/wails";
import { Input, Select } from "@/shared/ui";
import { withCountryDefaults, type Step, type WizardState } from "./wizardState";

interface StepProps {
  step: Step;
  state: WizardState;
  options?: SetupOptions;
  onChange: (patch: Partial<WizardState>) => void;
}

/**
 * The fields for one step.
 *
 * One component with a switch rather than seven files: each step is two to five inputs, and
 * splitting them would mean seven modules whose only difference is which fields they render.
 * The wizard's shape — order, progress, submission — lives in SetupWizard, where it belongs.
 *
 * Every control is built from the Step 0.11 primitives. §FE.4 said no new primitive should be
 * needed and that needing one would be a finding worth recording; none was.
 */
export function StepFields({ step, state, options, onChange }: StepProps) {
  const { t } = useTranslation();

  switch (step) {
    case "language":
      return (
        <Select
          value={state.locale}
          onValueChange={(locale) => onChange({ locale })}
          ariaLabel={t("setup.field.language")}
          options={(options?.locales ?? []).map((code) => ({
            value: code,
            // Named in its own language: someone looking for Arabic is looking for "العربية".
            label: t(`locale.name.${code}`),
          }))}
        />
      );

    case "country":
      return (
        <Select
          value={state.countryCode}
          onValueChange={(code) => {
            const country = options?.countries.find((item) => item.code === code);
            // Selecting a country pre-fills currency, fiscal year, and language — and every one
            // of them is shown and editable on a later step (§C.1).
            onChange(country ? withCountryDefaults(state, country) : { countryCode: code });
          }}
          ariaLabel={t("setup.field.country")}
          options={(options?.countries ?? []).map((country) => ({
            value: country.code,
            // The profile carries a KEY, not a name, so the country list is translated like
            // everything else rather than making English its source language.
            label: t(country.nameKey),
          }))}
        />
      );

    case "company":
      return (
        <>
          <Input
            label={t("setup.field.companyName")}
            value={state.companyName}
            onChange={(event) => onChange({ companyName: event.target.value })}
            required
          />
          <Input
            label={t("setup.field.companyCode")}
            value={state.companyCode}
            onChange={(event) => onChange({ companyCode: event.target.value.toUpperCase() })}
            hint={t("setup.field.companyCode.hint")}
            required
          />
          <Input
            label={t("setup.field.legalName")}
            value={state.legalName}
            onChange={(event) => onChange({ legalName: event.target.value })}
          />
          <Input
            label={t("setup.field.taxNumber")}
            value={state.taxNumber}
            onChange={(event) => onChange({ taxNumber: event.target.value })}
          />
        </>
      );

    case "currency":
      return (
        <>
          <Field label={t("setup.field.functionalCurrency")} hint={t("setup.field.functionalCurrency.hint")}>
            <Select
              value={state.functionalCurrency}
              onValueChange={(functionalCurrency) => onChange({ functionalCurrency })}
              ariaLabel={t("setup.field.functionalCurrency")}
              options={currencyOptions(options)}
            />
          </Field>
          <Field label={t("setup.field.pricingCurrency")} hint={t("setup.field.pricingCurrency.hint")}>
            <Select
              value={state.pricingCurrency}
              onValueChange={(pricingCurrency) => onChange({ pricingCurrency })}
              ariaLabel={t("setup.field.pricingCurrency")}
              options={currencyOptions(options)}
            />
          </Field>
        </>
      );

    case "business":
      return (
        <Select
          value={state.businessProfile}
          onValueChange={(businessProfile) => onChange({ businessProfile })}
          ariaLabel={t("setup.field.businessProfile")}
          options={(options?.businessProfiles ?? []).map((profile) => ({
            value: profile.code,
            label: t(profile.nameKey),
          }))}
        />
      );

    case "locations":
      return (
        <>
          <Input
            label={t("setup.field.branchName")}
            value={state.branchName}
            onChange={(event) => onChange({ branchName: event.target.value })}
            required
          />
          <Input
            label={t("setup.field.branchCode")}
            value={state.branchCode}
            onChange={(event) => onChange({ branchCode: event.target.value.toUpperCase() })}
            required
          />
          <Input
            label={t("setup.field.warehouseName")}
            value={state.warehouseName}
            onChange={(event) => onChange({ warehouseName: event.target.value })}
            required
          />
          <Input
            label={t("setup.field.warehouseCode")}
            value={state.warehouseCode}
            onChange={(event) => onChange({ warehouseCode: event.target.value.toUpperCase() })}
            required
          />
          <Input
            label={t("setup.field.fiscalYearStartYear")}
            type="number"
            value={String(state.fiscalYearStartYear)}
            onChange={(event) =>
              onChange({ fiscalYearStartYear: Number.parseInt(event.target.value, 10) || 0 })
            }
            hint={t("setup.field.fiscalYear.hint")}
            required
          />
          <Field label={t("setup.field.fiscalYearStartMonth")}>
            <Select
              value={String(state.fiscalYearStartMonth)}
              onValueChange={(month) =>
                onChange({ fiscalYearStartMonth: Number.parseInt(month, 10) })
              }
              ariaLabel={t("setup.field.fiscalYearStartMonth")}
              options={MONTHS.map((month) => ({
                value: String(month),
                label: t(`month.${month}`),
              }))}
            />
          </Field>
        </>
      );

    case "administrator":
      return (
        <>
          <Input
            label={t("setup.field.adminUsername")}
            value={state.adminUsername}
            onChange={(event) => onChange({ adminUsername: event.target.value })}
            autoComplete="username"
            required
          />
          <Input
            label={t("setup.field.adminDisplayName")}
            value={state.adminDisplayName}
            onChange={(event) => onChange({ adminDisplayName: event.target.value })}
          />
          <Input
            label={t("setup.field.adminPassword")}
            type="password"
            value={state.adminPassword}
            onChange={(event) => onChange({ adminPassword: event.target.value })}
            autoComplete="new-password"
            // The policy itself lives on the backend (§13.1) and is enforced there; this is a
            // hint so the user is not refused after seven screens for a rule nobody told them.
            hint={t("setup.field.adminPassword.hint")}
            required
          />
          <Input
            label={t("setup.field.adminPasswordConfirm")}
            type="password"
            value={state.adminPasswordConfirm}
            onChange={(event) => onChange({ adminPasswordConfirm: event.target.value })}
            autoComplete="new-password"
            // Confirmed HERE and never sent: mistyping the one credential that can open a
            // fresh install is a support call nobody can resolve, since no default password
            // ships to fall back on (§13.1).
            error={
              state.adminPasswordConfirm !== "" &&
              state.adminPassword !== state.adminPasswordConfirm
                ? t("setup.field.adminPassword.mismatch")
                : undefined
            }
            required
          />
        </>
      );
  }
}

const MONTHS = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12];

function currencyOptions(options?: SetupOptions) {
  return (options?.currencies ?? []).map((currency) => ({
    value: currency.code,
    label: `${currency.code} — ${currency.name}`,
  }));
}

/**
 * A label and hint around a control that has no label of its own.
 *
 * Select takes an aria-label but renders no visible one, and an unlabelled control in a
 * form-heavy application is the most common accessibility defect there is.
 */
function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-sm font-medium text-text">{label}</span>
      {children}
      {hint ? <span className="text-xs text-text-muted">{hint}</span> : null}
    </div>
  );
}
