import { screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SetupWizard } from "@/modules/setup/SetupWizard";
import { canAdvance, initialState, toInput, withCountryDefaults } from "@/modules/setup/wizardState";
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

/** Fills every step and presses Finish. */
async function completeWizard(user: ReturnType<typeof renderApp>["user"]) {
  const next = () => user.click(screen.getByRole("button", { name: /^next$/i }));

  await next(); // language: the default is already valid

  await selectOption(user, /country/i, "SY");
  await next();

  await user.type(screen.getByLabelText(/company name/i), "Demo Trading");
  await user.type(screen.getByLabelText(/short code/i), "main");
  await next();

  await next(); // currency: pre-filled from the country

  await next(); // business type: optional

  await user.type(screen.getByLabelText(/branch name/i), "Head Office");
  await user.type(screen.getByLabelText(/store name/i), "Main Store");
  await next();

  await user.type(screen.getByLabelText(/^username$/i), "nadia");
  await user.type(screen.getByLabelText(/^password$/i), "a sufficiently long passphrase");
  await user.type(screen.getByLabelText(/repeat password/i), "a sufficiently long passphrase");
  await user.click(screen.getByRole("button", { name: /finish setup/i }));
}

async function selectOption(
  user: ReturnType<typeof renderApp>["user"],
  name: RegExp,
  value: string,
) {
  await user.click(screen.getByRole("combobox", { name }));
  await user.click(await screen.findByRole("option", { name: new RegExp(labelFor(value), "i") }));
}

function labelFor(value: string): string {
  return value === "SY" ? "syria" : value;
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
        companyCode: "MAIN",
        functionalCurrency: "SYP",
        pricingCurrency: "USD",
        branchName: "Head Office",
        warehouseName: "Main Store",
        adminUsername: "nadia",
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
    expect(screen.getByLabelText(/branch name/i)).toHaveValue("Head Office");
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
    expect(canAdvance("currency", edited, OPTIONS)).toBe(true);
  });

  it("falls back to the functional currency when a country prices in one currency", () => {
    const single = { ...SYRIA, pricingCurrency: "" };
    expect(withCountryDefaults(initialState("en"), single).pricingCurrency).toBe("SYP");
  });
});

describe("step validation", () => {
  it("will not advance past a country that is not on offer", () => {
    const state = { ...initialState("en"), countryCode: "ZZ" };
    expect(canAdvance("country", state, OPTIONS)).toBe(false);
  });

  it("will not finish while the two passwords differ", () => {
    const state = {
      ...initialState("en"),
      adminUsername: "nadia",
      adminPassword: "one passphrase entirely",
      adminPasswordConfirm: "another passphrase entirely",
    };
    expect(canAdvance("administrator", state, OPTIONS)).toBe(false);
  });

  it("treats the business profile as optional", () => {
    // A shop that fits none of the shipped trades is a real case, and forcing a choice would
    // make whichever appears first the answer for everybody.
    expect(canAdvance("business", initialState("en"), OPTIONS)).toBe(true);
  });
});
