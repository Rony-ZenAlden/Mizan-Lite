import type { SetupCountry, SetupInput, SetupOptions } from "@/lib/wails";

/**
 * The wizard's three steps.
 *
 * # Seven became three, and nothing was dropped
 *
 * §C.2 lists ten; a first pass collapsed them to seven. Seven screens to open a shop is still
 * six too many, and the count was never the real problem — the SHAPE was. Five of the seven
 * asked for a code, a name, and two more codes, which is a data-entry form wearing a wizard's
 * clothes.
 *
 * So the questions are now grouped by what a shopkeeper actually knows:
 *
 *   shop     Where are you, and what is the shop called?
 *   trade    What do you sell, and in what money?
 *   account  Who are you, and what goes on the receipt?
 *
 * Everything the backend still requires — company code, branch, warehouse, fiscal year — is
 * DERIVED from those answers and shown on the step that derives it, under a disclosure. That
 * matters: §C.1's rule is that a default the user never sees is indistinguishable from a rule
 * they cannot change. Derived is not hidden.
 *
 * Tax is still absent rather than disabled. No country profile ships a tax profile to enable
 * (§C.3), so the question has no answer to offer, and a control that cannot do anything teaches
 * users to distrust controls.
 */
export const STEPS = ["shop", "trade", "account"] as const;

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

/**
 * A company code derived from the shop's name.
 *
 * The code is an internal identifier that appears in exports and imports; a shopkeeper has no
 * opinion about it and asking for one is a question with no right answer. Letters and digits
 * only, upper-cased, capped — matching what the backend accepts, so the derived value is never
 * the reason a submission is refused.
 *
 * Falls back to "MAIN" rather than to an empty string: a blank code fails validation on the last
 * step, which is the worst possible moment to discover it.
 */
export function codeFromName(name: string): string {
  const cleaned = name
    .toUpperCase()
    .replace(/[^A-Z0-9]+/g, "")
    .slice(0, 12);
  return cleaned || "MAIN";
}

export function initialState(defaultLocale: string): WizardState {
  return {
    locale: defaultLocale,
    receiptHeader: "",
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
    receiptHeader: state.receiptHeader,
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
    case "shop":
      return (
        state.countryCode !== "" &&
        Boolean(options?.countries.some((c) => c.code === state.countryCode)) &&
        state.companyName.trim() !== "" &&
        // Derived from the name, but still checked. If the derivation ever produced nothing the
        // failure would surface here, on the step that owns it, rather than as a rejection on
        // the last screen with no indication of which answer caused it.
        state.companyCode.trim() !== "" &&
        state.branchCode.trim() !== "" &&
        state.branchName.trim() !== "" &&
        state.warehouseCode.trim() !== "" &&
        state.warehouseName.trim() !== "" &&
        state.fiscalYearStartYear > 0
      );
    case "trade":
      // The business profile itself is OPTIONAL: a shop that fits none of the shipped trades is
      // a real case, and forcing a choice would make the first one on the list the answer for
      // everybody. The currencies are not optional — nothing can be priced without them.
      return state.functionalCurrency !== "" && state.pricingCurrency !== "";
    case "account":
      // The receipt header is deliberately absent from this check. Blank means "use the shop's
      // name", which is the right answer for most shops and must not be a thing to fill in.
      return (
        state.adminUsername.trim() !== "" &&
        state.adminPassword !== "" &&
        state.adminPassword === state.adminPasswordConfirm
      );
  }
}
