import { screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SetupWizard } from "@/modules/setup/SetupWizard";
import {
  canAdvance,
  codeFromName,
  initialState,
  toInput,
  withCountryDefaults,
} from "@/modules/setup/wizardState";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    applySetup: vi.fn(),
    login: vi.fn(),
    preferences: vi.fn(),
    setLocale: vi.fn(),
  };
});

const SYRIA: wails.SetupCountry = {
  code: "SY",
  nameKey: "country.sy",
  defaultLocale: "ar",
  supportedLocales: ["ar", "en"],
  functionalCurrency: "SYP",
  pricingCurrency: "USD",
  fiscalYearStart: 1,
  dateFormat: "dd/MM/yyyy",
  firstDayOfWeek: 6,
  phoneCode: "+963",
};

const OPTIONS: wails.SetupOptions = {
  countries: [SYRIA],
  businessProfiles: [
    { code: "general_retail", nameKey: "business_profile.general_retail", name: "General retail", description: "" },
  ],
  currencies: [
    { code: "SYP", name: "Syrian Pound", symbol: "ل.س" },
    { code: "USD", name: "US Dollar", symbol: "$" },
  ],
  locales: ["en", "ar"],
};

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en",
    theme: "system",
    availableLocales: ["en", "ar"],
  });
  vi.mocked(wails.setLocale).mockResolvedValue({
    locale: "en",
    theme: "system",
    availableLocales: ["en", "ar"],
  });
});

afterEach(() => vi.clearAllMocks());

/**
 * Fills every step and presses Finish.
 *
 * THREE steps, and the shop's name is the only thing typed on the first — the company code, the
 * branch and the store are all derived from it. That the wizard still submits every field the
 * backend needs is the assertion below.
 */
async function completeWizard(user: ReturnType<typeof renderApp>["user"]) {
  const next = () => user.click(screen.getByRole("button", { name: /^next$/i }));

  // 1. Your shop. A country and a name.
  await user.click(screen.getByRole("radio", { name: /syria/i }));
  await user.type(screen.getByLabelText(/what is the shop called/i), "Demo Trading");
  await next();

  // 2. What you sell. Both currencies came from the country.
  await next();

  // 3. Your account.
  await user.type(screen.getByLabelText(/^username$/i), "nadia");
  await user.type(screen.getByLabelText(/^password$/i), "a sufficiently long passphrase");
  await user.type(screen.getByLabelText(/repeat password/i), "a sufficiently long passphrase");
  await user.click(screen.getByRole("button", { name: /finish setup/i }));
}

describe("SetupWizard", () => {
  it("submits everything in ONE call and then signs in", async () => {
    // One object, submitted once (D5) — matching the backend's single transaction (1.9 D3).
    // And it signs in afterwards, which PROVES the account works before the wizard closes:
    // a configured system nobody can enter is the worst outcome on a fresh install.
    vi.mocked(wails.applySetup).mockResolvedValue({
      companyId: "c", branchId: "b", warehouseId: "w", adminUserId: "u",
    });
    vi.mocked(wails.login).mockResolvedValue(SIGNED_IN);

    const { user } = renderApp(<SetupWizard options={OPTIONS} />);
    await completeWizard(user);

    await waitFor(() => expect(wails.applySetup).toHaveBeenCalledTimes(1));
    expect(wails.applySetup).toHaveBeenCalledWith(
      expect.objectContaining({
        countryCode: "SY",
        companyName: "Demo Trading",
        // DERIVED, every one of them, from the two answers above. Nobody typed these — and the
        // backend still receives a complete company, branch and warehouse.
        companyCode: "DEMOTRADING",
        branchName: "Demo Trading",
        warehouseName: "Demo Trading",
        branchCode: "HQ",
        warehouseCode: "WH1",
        functionalCurrency: "SYP",
        pricingCurrency: "USD",
        adminUsername: "nadia",
        // Blank, and sent blank: it MEANS "use the shop's name", so filling it in here would
        // freeze today's name into a setting.
        receiptHeader: "",
      }),
    );
    await waitFor(() => expect(wails.login).toHaveBeenCalledTimes(1));
  });

  it("keeps everything typed when the submission is rejected", async () => {
    // Twelve fields re-entered because step six was wrong is how a first-run experience earns
    // its reputation. The draft is one object, so a rejection costs nothing but a click.
    vi.mocked(wails.applySetup).mockRejectedValue(
      new wails.BindingError({
        code: "identity.password_too_short",
        messageKey: "identity.password_too_short",
      }),
    );

    const { user } = renderApp(<SetupWizard options={OPTIONS} />);
    await completeWizard(user);

    expect(await screen.findByRole("alert")).toBeInTheDocument();
    // Still on the last step, with what was typed intact.
    expect(screen.getByLabelText(/^username$/i)).toHaveValue("nadia");

    await user.click(screen.getByRole("button", { name: /back/i }));
    await user.click(screen.getByRole("button", { name: /back/i }));
    expect(screen.getByLabelText(/what is the shop called/i)).toHaveValue("Demo Trading");
  });

  it("never sends the password confirmation", async () => {
    // Confirmed locally; the backend has one password field, and sending a second would put a
    // credential somewhere nothing expects one.
    const state = {
      ...initialState("en"),
      adminPassword: "a sufficiently long passphrase",
      adminPasswordConfirm: "a sufficiently long passphrase",
    };
    expect(Object.keys(toInput(state))).not.toContain("adminPasswordConfirm");
  });
});

describe("country defaults", () => {
  it("pre-fills from the profile, and every value stays editable", async () => {
    // §C.1: "the profile only supplies defaults; it never constrains later configuration".
    // The wizard SHOWS them on their own screens for exactly that reason — a default the user
    // never sees is indistinguishable from a rule they cannot change.
    const filled = withCountryDefaults(initialState("en"), SYRIA);
    expect(filled.functionalCurrency).toBe("SYP");
    expect(filled.pricingCurrency).toBe("USD");
    expect(filled.fiscalYearStartMonth).toBe(1);
    expect(filled.locale).toBe("ar");

    const edited = { ...filled, functionalCurrency: "USD" };
    expect(canAdvance("trade", edited, OPTIONS)).toBe(true);
  });

  it("falls back to the functional currency when a country prices in one currency", () => {
    const single = { ...SYRIA, pricingCurrency: "" };
    expect(withCountryDefaults(initialState("en"), single).pricingCurrency).toBe("SYP");
  });
});

describe("step validation", () => {
  /** A shop step filled in the way the UI fills it: the name derives the rest. */
  function namedShop() {
    const named = withCountryDefaults(initialState("en"), SYRIA);
    return {
      ...named,
      countryCode: "SY",
      companyName: "Al-Noor Market",
      companyCode: codeFromName("Al-Noor Market"),
      branchName: "Al-Noor Market",
      warehouseName: "Al-Noor Market",
    };
  }

  it("will not advance past a country that is not on offer", () => {
    expect(canAdvance("shop", { ...namedShop(), countryCode: "ZZ" }, OPTIONS)).toBe(false);
  });

  it("will not advance without a shop name", () => {
    expect(canAdvance("shop", { ...namedShop(), companyName: "   " }, OPTIONS)).toBe(false);
  });

  it("advances once the country and the name are answered", () => {
    // The point of collapsing seven steps into three: two answers, and everything the backend
    // needs for a company, a branch and a warehouse is present.
    expect(canAdvance("shop", namedShop(), OPTIONS)).toBe(true);
  });

  it("will not finish while the two passwords differ", () => {
    const state = {
      ...initialState("en"),
      adminUsername: "nadia",
      adminPassword: "one passphrase entirely",
      adminPasswordConfirm: "another passphrase entirely",
    };
    expect(canAdvance("account", state, OPTIONS)).toBe(false);
  });

  it("treats the business profile as optional", () => {
    // A shop that fits none of the shipped trades is a real case, and forcing a choice would
    // make whichever appears first the answer for everybody.
    const priced = { ...initialState("en"), functionalCurrency: "SYP", pricingCurrency: "SYP" };
    expect(canAdvance("trade", priced, OPTIONS)).toBe(true);
  });

  it("treats the receipt header as optional", () => {
    // Blank MEANS "use the shop's name", all the way down to the print path. Requiring it would
    // be a step that exists to be skipped.
    const state = {
      ...initialState("en"),
      adminUsername: "nadia",
      adminPassword: "a sufficiently long passphrase",
      adminPasswordConfirm: "a sufficiently long passphrase",
      receiptHeader: "",
    };
    expect(canAdvance("account", state, OPTIONS)).toBe(true);
  });
});

describe("derived identifiers", () => {
  /*
   * A shopkeeper knows what their shop is called and has no opinion about a company code. The
   * old wizard asked for one, and the answer was always the shop's name typed again.
   */
  it("makes a usable code out of a shop name", () => {
    expect(codeFromName("Al-Noor Market")).toBe("ALNOORMARKET");
    expect(codeFromName("  corner shop  ")).toBe("CORNERSHOP");
  });

  it("never derives an empty code, whatever it is given", () => {
    // A blank code fails validation on the LAST step — the worst possible moment to find out,
    // and about a field the user never filled in.
    expect(codeFromName("")).toBe("MAIN");
    expect(codeFromName("متجر النور")).toBe("MAIN");
    expect(codeFromName("!!! ???")).toBe("MAIN");
  });

  it("keeps the code short enough for the backend to accept", () => {
    expect(codeFromName("A".repeat(60)).length).toBeLessThanOrEqual(12);
  });
});
