import type { ReactNode } from "react";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import type { SetupOptions } from "@/lib/wails";
import { ChoiceGroup, Input, Select } from "@/shared/ui";
import { codeFromName, withCountryDefaults, type Step, type WizardState } from "./wizardState";

interface StepProps {
  step: Step;
  state: WizardState;
  options?: SetupOptions;
  onChange: (patch: Partial<WizardState>) => void;
}

/**
 * The fields for one step.
 *
 * One component with a switch rather than three files: each step is a handful of controls, and
 * splitting them would mean three modules whose only difference is which ones they render. The
 * wizard's shape — order, progress, submission — lives in SetupWizard, where it belongs.
 *
 * # Choices, not dropdowns
 *
 * Country, trade and currency are asked as visible options rather than closed selects. Somebody
 * opening this screen for the first time does not know what a "business profile" is, and a
 * dropdown labelled that way tells them nothing — while four cards reading *Retail shop*,
 * *Restaurant*, *Wholesale*, *Workshop* answer the question by existing.
 *
 * Selects survive where the list is long enough that seeing it is not the point: months, and the
 * full currency list behind "something else".
 */
export function StepFields({ step, state, options, onChange }: StepProps) {
  const { t } = useTranslation();

  switch (step) {
    case "shop":
      return (
        <>
          <ChoiceGroup
            name="country"
            legend={t("setup.field.country")}
            hint={t("setup.q.country.hint")}
            value={state.countryCode}
            onChange={(code) => {
              const country = options?.countries.find((item) => item.code === code);
              // A country pre-fills currency, fiscal year and language. Every one of them stays
              // visible and editable on the step that uses it (§C.1).
              onChange(country ? withCountryDefaults(state, country) : { countryCode: code });
            }}
            choices={(options?.countries ?? []).map((country) => ({
              value: country.code,
              // The profile carries a KEY, not a name, so the list is translated like everything
              // else rather than making English its source language.
              label: t(country.nameKey),
              hint: country.functionalCurrency,
            }))}
            columns={3}
          />

          <Input
            label={t("setup.q.shopName")}
            value={state.companyName}
            hint={t("setup.q.shopName.hint")}
            onChange={(event) => {
              const companyName = event.target.value;
              /*
               * The name drives four other fields.
               *
               * A shopkeeper knows what their shop is called. They have no opinion about a
               * company code, a branch name or a warehouse name, and asking produced four boxes
               * that everybody filled in with the same word.
               *
               * Derived on every keystroke rather than once on blur, so what the disclosure
               * below shows is always what will be submitted — a summary that lags is worse than
               * no summary.
               */
              onChange({
                companyName,
                companyCode: codeFromName(companyName),
                branchName: companyName,
                warehouseName: companyName,
              });
            }}
            required
          />

          <Details summary={t("setup.details.derived")}>
            {/*
             * Shown, not hidden. §C.1: a default the user never sees is indistinguishable from a
             * rule they cannot change — so everything derived above is here, editable, one click
             * away.
             */}
            <Input
              label={t("setup.field.companyCode")}
              value={state.companyCode}
              onChange={(event) =>
                onChange({ companyCode: event.target.value.toUpperCase() })
              }
              hint={t("setup.field.companyCode.hint")}
              required
            />
            <Input
              label={t("setup.field.legalName")}
              value={state.legalName}
              hint={t("setup.field.legalName.hint")}
              onChange={(event) => onChange({ legalName: event.target.value })}
            />
            <Input
              label={t("setup.field.taxNumber")}
              value={state.taxNumber}
              onChange={(event) => onChange({ taxNumber: event.target.value })}
            />
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
          </Details>
        </>
      );

    case "trade":
      return (
        <>
          <ChoiceGroup
            name="business"
            legend={t("setup.q.trade")}
            hint={t("setup.q.trade.hint")}
            value={state.businessProfile}
            onChange={(businessProfile) => onChange({ businessProfile })}
            choices={(options?.businessProfiles ?? []).map((profile) => ({
              value: profile.code,
              label: t(profile.nameKey),
            }))}
          />

          <ChoiceGroup
            name="currency"
            legend={t("setup.q.currency")}
            hint={t("setup.q.currency.hint")}
            value={state.functionalCurrency}
            onChange={(code) =>
              // Both currencies move together. They differ only for a shop that prices in one
              // money and keeps its books in another — a real case, and a rare enough one that
              // it belongs behind the disclosure rather than in front of every shopkeeper.
              onChange({ functionalCurrency: code, pricingCurrency: code })
            }
            choices={suggestedCurrencies(state, options)}
            columns={3}
          />

          <Details summary={t("setup.details.currency")}>
            <Field
              label={t("setup.field.functionalCurrency")}
              hint={t("setup.field.functionalCurrency.hint")}
            >
              <Select
                value={state.functionalCurrency}
                onValueChange={(functionalCurrency) => onChange({ functionalCurrency })}
                ariaLabel={t("setup.field.functionalCurrency")}
                options={currencyOptions(options)}
              />
            </Field>
            <Field
              label={t("setup.field.pricingCurrency")}
              hint={t("setup.field.pricingCurrency.hint")}
            >
              <Select
                value={state.pricingCurrency}
                onValueChange={(pricingCurrency) => onChange({ pricingCurrency })}
                ariaLabel={t("setup.field.pricingCurrency")}
                options={currencyOptions(options)}
              />
            </Field>
            <Field label={t("setup.field.fiscalYearStartMonth")} hint={t("setup.field.fiscalYear.hint")}>
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
            <Input
              label={t("setup.field.fiscalYearStartYear")}
              type="number"
              value={String(state.fiscalYearStartYear)}
              onChange={(event) =>
                onChange({ fiscalYearStartYear: Number.parseInt(event.target.value, 10) || 0 })
              }
              required
            />
          </Details>
        </>
      );

    case "account":
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
            // hint so the user is not refused after three screens for a rule nobody told them.
            hint={t("setup.field.adminPassword.hint")}
            required
          />
          <Input
            label={t("setup.field.adminPasswordConfirm")}
            type="password"
            value={state.adminPasswordConfirm}
            onChange={(event) => onChange({ adminPasswordConfirm: event.target.value })}
            autoComplete="new-password"
            // Confirmed HERE and never sent: mistyping the one credential that can open a fresh
            // install is a support call nobody can resolve, since no default password ships to
            // fall back on (§13.1).
            error={
              state.adminPasswordConfirm !== "" &&
              state.adminPassword !== state.adminPasswordConfirm
                ? t("setup.field.adminPassword.mismatch")
                : undefined
            }
            required
          />

          <Input
            label={t("setup.q.receiptHeader")}
            value={state.receiptHeader}
            // The shop's name is shown as the PLACEHOLDER rather than filled in, because blank
            // genuinely means "use the shop's name" all the way down to the print path. Filling
            // it in would freeze today's name into a setting, so renaming the shop later would
            // keep printing the old one.
            placeholder={state.companyName}
            hint={t("setup.q.receiptHeader.hint")}
            onChange={(event) => onChange({ receiptHeader: event.target.value })}
          />
        </>
      );
  }
}

const MONTHS = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12];

/**
 * The currencies worth showing as cards: the country's own, plus the ones a shop in the region
 * actually quotes in.
 *
 * The full list is two hundred entries and belongs in the Select behind the disclosure. Showing
 * every currency as a card would be the dropdown problem again, in a worse shape.
 */
function suggestedCurrencies(state: WizardState, options?: SetupOptions) {
  const all = options?.currencies ?? [];
  const wanted = [state.functionalCurrency, "USD", "EUR", "SAR", "AED"];

  const seen = new Set<string>();
  const out: Array<{ value: string; label: string; hint?: string }> = [];
  for (const code of wanted) {
    if (!code || seen.has(code)) continue;
    const currency = all.find((item) => item.code === code);
    if (!currency) continue;
    seen.add(code);
    out.push({ value: currency.code, label: currency.code, hint: currency.name });
  }
  return out;
}

function currencyOptions(options?: SetupOptions) {
  return (options?.currencies ?? []).map((currency) => ({
    value: currency.code,
    label: `${currency.code} — ${currency.name}`,
  }));
}

/**
 * The fields a step derives rather than asks for, one click away.
 *
 * A native <details>, not a state-driven panel: it works before hydration, it is announced as
 * expandable, and Enter and Space open it without any keyboard handling of our own.
 */
function Details({ summary, children }: { summary: string; children: ReactNode }) {
  return (
    <details className="card bg-surface-raised p-3">
      <summary className="cursor-pointer text-sm font-medium text-text">{summary}</summary>
      <div className="mt-3 flex flex-col gap-3">{children}</div>
    </details>
  );
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
