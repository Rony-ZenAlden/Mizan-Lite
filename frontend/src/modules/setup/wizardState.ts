import type { SetupCountry, SetupInput, SetupOptions } from "@/lib/wails";

/**
 * The wizard's seven steps (Addendum §C.2, collapsed).
 *
 * §C.2 lists ten. Three of them are single fields that belong with their neighbours, one
 * (starting data) is Phase 9, and one (tax) is omitted rather than shown disabled — no country
 * profile ships a tax profile to enable (§C.3), so the question has no answer to offer yet.
 * Showing a control that cannot do anything teaches users to distrust controls.
 */
export const STEPS = [
  "language",
  "country",
  "company",
  "currency",
  "business",
  "locations",
  "administrator",
] as const;

export type Step = (typeof STEPS)[number];

/**
 * Everything the wizard collects, plus the password confirmation the backend never sees.
 *
 * ONE object across all seven steps (Step 1.10, D5), submitted once. Two reasons, and the
 * second is the one that matters:
 *
 *   - The backend applies everything in a single transaction (1.9 D3). A stepwise API would
 *     have to hold a partial company or invent a draft, and both are states nobody can recover
 *     from.
 *   - A validation failure must not cost the user what they typed. Twelve fields re-entered
 *     because step six was wrong is how a first-run experience earns its reputation.
 */
export interface WizardState extends SetupInput {
  /** Confirmed locally and never sent: the backend has one password field. */
  adminPasswordConfirm: string;
}

export function initialState(defaultLocale: string): WizardState {
  return {
    locale: defaultLocale,
    countryCode: "",
    companyCode: "",
    companyName: "",
    legalName: "",
    taxNumber: "",
    functionalCurrency: "",
    pricingCurrency: "",
    businessProfile: "",
    fiscalYearStartMonth: 1,
    fiscalYearStartYear: new Date().getFullYear(),
    branchCode: "HQ",
    branchName: "",
    warehouseCode: "WH1",
    warehouseName: "",
    adminUsername: "",
    adminDisplayName: "",
    adminPassword: "",
    adminPasswordConfirm: "",
  };
}

/**
 * Applies a country's defaults to the draft.
 *
 * Everything it sets stays editable on a later step. That is §C.1's *"the profile only supplies
 * defaults; it never constrains later configuration"* — and it is why the wizard SHOWS these
 * values on their own screens rather than applying them invisibly. A default the user never
 * sees is indistinguishable from a rule they cannot change.
 */
export function withCountryDefaults(state: WizardState, country: SetupCountry): WizardState {
  return {
    ...state,
    countryCode: country.code,
    locale: country.defaultLocale || state.locale,
    functionalCurrency: country.functionalCurrency,
    pricingCurrency: country.pricingCurrency || country.functionalCurrency,
    fiscalYearStartMonth: country.fiscalYearStart || 1,
  };
}

/**
 * Strips the local-only field before submission.
 *
 * Built by naming every field rather than by spreading and deleting: the backend's Input is the
 * contract, and a field added to WizardState for the UI's own purposes must not reach it by
 * accident. A `...rest` here would send whatever the wizard happened to be holding.
 */
export function toInput(state: WizardState): SetupInput {
  return {
    locale: state.locale,
    countryCode: state.countryCode,
    companyCode: state.companyCode,
    companyName: state.companyName,
    legalName: state.legalName,
    taxNumber: state.taxNumber,
    functionalCurrency: state.functionalCurrency,
    pricingCurrency: state.pricingCurrency,
    businessProfile: state.businessProfile,
    fiscalYearStartMonth: state.fiscalYearStartMonth,
    fiscalYearStartYear: state.fiscalYearStartYear,
    branchCode: state.branchCode,
    branchName: state.branchName,
    warehouseCode: state.warehouseCode,
    warehouseName: state.warehouseName,
    adminUsername: state.adminUsername,
    adminDisplayName: state.adminDisplayName,
    adminPassword: state.adminPassword,
  };
}

/**
 * Whether a step has everything it needs to advance.
 *
 * Local checks only, and deliberately shallow: the backend owns every real rule — the password
 * policy, the country registry, whether a currency is seeded — and a second copy here would
 * eventually disagree with the first. This stops the user walking forward with an empty box,
 * nothing more.
 */
export function canAdvance(step: Step, state: WizardState, options?: SetupOptions): boolean {
  switch (step) {
    case "language":
      return state.locale !== "";
    case "country":
      return state.countryCode !== "" && Boolean(options?.countries.some((c) => c.code === state.countryCode));
    case "company":
      return state.companyCode.trim() !== "" && state.companyName.trim() !== "";
    case "currency":
      return state.functionalCurrency !== "" && state.pricingCurrency !== "";
    case "business":
      // Optional: a shop that fits none of the shipped trades is a real case, and forcing a
      // choice would make the first one they see the answer for everybody.
      return true;
    case "locations":
      return (
        state.branchCode.trim() !== "" &&
        state.branchName.trim() !== "" &&
        state.warehouseCode.trim() !== "" &&
        state.warehouseName.trim() !== "" &&
        state.fiscalYearStartYear > 0
      );
    case "administrator":
      return (
        state.adminUsername.trim() !== "" &&
        state.adminPassword !== "" &&
        state.adminPassword === state.adminPasswordConfirm
      );
  }
}
