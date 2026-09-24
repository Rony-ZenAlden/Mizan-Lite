import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { BindingError } from "@/api/envelope";
import { aRate, aSettings, aUSDOnlyPlan, fakeClient, renderWithProviders } from "@/api/testing";
import { SettingsScreen } from "./SettingsScreen";

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}

describe("SettingsScreen — the shop's settings in one place (owner's request, 2026-09-17)", () => {
  it("groups the language, the currency and the rate, and says where the rest are set", async () => {
    renderWithProviders(<SettingsScreen />, { client: fakeClient(), locale: "en" });
    await settle();

    expect(screen.getByTestId("settings-general")).toHaveTextContent("Language and general preferences");
    expect(screen.getByTestId("settings-currency")).toHaveTextContent("Currency");
    expect(screen.getByTestId("settings-rate")).toHaveTextContent("Exchange rate");
    expect(screen.getByTestId("settings-security")).toHaveTextContent("Security");

    // The currency group shows what the shop has, and points at where each is changed rather than duplicating a control.
    const currency = screen.getByTestId("settings-currency");
    expect(currency).toHaveTextContent("500");
    expect(currency).toHaveTextContent("The smallest note is set on the Cash screen");
    // The redenomination is no longer a promise: it is a control (L10).
    expect(screen.getByTestId("money-display")).toBeInTheDocument();
  });

  it("offers three rate sources and asks for the endpoint only for the local one", async () => {
    const update = vi.fn(async () => aSettings({ rateSource: "local" }));
    renderWithProviders(<SettingsScreen />, { client: fakeClient({ settings: { update } }), locale: "en" });
    await settle();

    const picker = screen.getByLabelText("Where the rate comes from");
    expect([...picker.querySelectorAll("option")].map((o) => o.textContent)).toEqual([
      "The standard provider + your margin %",
      "A local market rate (an address you choose)",
      "A fixed rate you type",
    ]);

    // The endpoint boxes appear only when the local source is chosen.
    expect(screen.queryByTestId("settings-local")).not.toBeInTheDocument();
    await userEvent.selectOptions(picker, "local");
    expect(screen.getByTestId("settings-local")).toBeInTheDocument();
    // It works with no address: the built-in Damascus source is used until the shop gives one of its own.
    expect(screen.getByTestId("settings-local-builtin")).toHaveTextContent("Built in: the Damascus dollar rate");

    await userEvent.type(screen.getByLabelText("Local market address"), "https://rates.example.sy/api");
    await userEvent.type(screen.getByLabelText("The field inside the JSON"), "usd.sell");
    await userEvent.click(screen.getByRole("button", { name: "Save the settings" }));
    await settle();

    expect(update).toHaveBeenCalledWith(
      expect.objectContaining({ rateSource: "local", localRateUrl: "https://rates.example.sy/api", localRateField: "usd.sell" }),
    );
  });

  it("choosing a fixed rate puts the shop into manual mode instead of setting a source", async () => {
    const setMode = vi.fn(async () => aRate({ mode: "manual" }));
    const update = vi.fn(async () => aSettings());
    renderWithProviders(<SettingsScreen />, {
      client: fakeClient({ fx: { current: async () => aRate({ mode: "automatic" }), setMode }, settings: { update } }),
      locale: "en",
    });
    await settle();

    await userEvent.selectOptions(screen.getByLabelText("Where the rate comes from"), "manual");
    await userEvent.click(screen.getByRole("button", { name: "Save the settings" }));
    await settle();

    expect(setMode).toHaveBeenCalledWith("manual");
    // Manual mode fetches nothing, so no source is written alongside it.
    expect(update).not.toHaveBeenCalledWith(expect.objectContaining({ rateSource: expect.anything() }));
  });
});

describe("SettingsScreen — dropping the two noughts (L10, 2026-09-17)", () => {
  it("offers the three readings of the currency and saves the one chosen", async () => {
    const update = vi.fn(async () => aSettings({ moneyDisplay: "new" }));
    renderWithProviders(<SettingsScreen />, { client: fakeClient({ settings: { update } }), locale: "en" });
    await settle();

    const picker = screen.getByLabelText("How money is shown");
    expect([...picker.querySelectorAll("option")].map((o) => o.textContent)).toEqual([
      "The old pound only (15,000 SYP)",
      "The new pound only — two noughts dropped (150 SYP)",
      "Both (150 SYP with 15,000 beside it)",
      "US dollars only",
    ]);

    // The old pound is what a shop reads until it says otherwise, and no typing note is shown for it.
    expect(picker).toHaveValue("legacy");
    expect(screen.queryByTestId("money-display-typing")).not.toBeInTheDocument();

    await userEvent.selectOptions(picker, "new");
    // Choosing a new reading tells the shop that what it types changes too — the part that would otherwise surprise it.
    expect(screen.getByTestId("money-display-typing")).toHaveTextContent("type amounts in the new pound");

    await userEvent.click(screen.getByRole("button", { name: "Save the settings" }));
    await settle();
    expect(update).toHaveBeenCalledWith({ moneyDisplay: "new" });
  });

  it("says plainly that the books do not change", async () => {
    renderWithProviders(<SettingsScreen />, { client: fakeClient(), locale: "en" });
    await settle();
    expect(screen.getByTestId("money-display")).toHaveTextContent("It changes nothing in your books");
  });
});

describe("SettingsScreen — the language lives here now (owner's request, 2026-09-17)", () => {
  it("names each language in its own language, marked with its own lang attribute", async () => {
    renderWithProviders(<SettingsScreen />, { client: fakeClient(), locale: "en" });
    await settle();
    const picker = screen.getByLabelText("Language");
    expect(picker.querySelector('option[value="ar"]')).toHaveAttribute("lang", "ar");
    expect(picker.querySelector('option[value="en"]')).toHaveAttribute("lang", "en");
  });

  it("switches the language at once, in the same render, and saves it", async () => {
    const update = vi.fn(async () => aSettings({ locale: "en", direction: "ltr" }));
    renderWithProviders(<SettingsScreen />, { client: fakeClient({ settings: { update } }), locale: "ar" });
    await settle();
    expect(document.documentElement).toHaveAttribute("dir", "rtl");

    await userEvent.selectOptions(screen.getByLabelText("اللغة"), "en");

    expect(document.documentElement).toHaveAttribute("lang", "en");
    expect(document.documentElement).toHaveAttribute("dir", "ltr");
    expect(update).toHaveBeenCalledWith({ locale: "en" });
    await settle();
  });

  it("reverts when the language cannot be saved, so the screen never claims one that will not survive a restart", async () => {
    const update = vi.fn(async () => {
      throw new BindingError({ code: "lite.api.internal", messageKey: "lite.api.internal" });
    });
    renderWithProviders(<SettingsScreen />, { client: fakeClient({ settings: { update } }), locale: "ar" });
    await settle();

    await userEvent.selectOptions(screen.getByLabelText("اللغة"), "en");

    await waitFor(() => expect(document.documentElement).toHaveAttribute("dir", "rtl"));
    await settle();
  });
});

describe("SettingsScreen — the PIN switch (owner's request, 2026-09-17)", () => {
  it("is off on a shop that has not chosen, and turning it on saves it", async () => {
    const update = vi.fn(async () => aSettings({ pinRequired: true }));
    renderWithProviders(<SettingsScreen />, { client: fakeClient({ settings: { update } }), locale: "en" });
    await settle();

    const toggle = screen.getByTestId("pin-required");
    expect(toggle).not.toBeChecked();
    // The group says what turning it on costs, and what it does not change.
    const security = screen.getByTestId("settings-security");
    expect(security).toHaveTextContent("PIN verification required for sensitive actions");
    expect(security).toHaveTextContent("still written to the owner's record");
    // And that two acts ask for the PIN whichever way it is set.
    expect(security).toHaveTextContent("always ask for the PIN");

    await userEvent.click(toggle);
    await userEvent.click(screen.getByRole("button", { name: "Save the settings" }));
    await settle();

    expect(update).toHaveBeenCalledWith({ pinRequired: true });
  });

  it("shows the switch as the shop left it", async () => {
    renderWithProviders(<SettingsScreen />, {
      client: fakeClient({ settings: { get: async () => aSettings({ pinRequired: true }) } }),
      locale: "en",
    });
    await settle();
    expect(screen.getByTestId("pin-required")).toBeChecked();
  });
});

describe("SettingsScreen — printing (owner's request, 2026-09-20)", () => {
  it("offers silent printing only once a printer is set up, and says so when none is", async () => {
    const none = async () => ({ printer: "", paperMm: 80, path: "driver", autoPrint: "none", drawer: false, footer: "", invoicePrinter: "" });
    renderWithProviders(<SettingsScreen />, { client: fakeClient({ printers: { settings: none } }), locale: "en" });
    await settle();
    expect(screen.getByTestId("settings-printer-state")).toHaveTextContent("No printer is set up");
    // The control is there but cannot be used: a shop without a printer is told why, not shown a dead option.
    expect(screen.getByLabelText("Print receipts straight away")).toBeDisabled();
  });

  it("saves the choice against the printer's settings", async () => {
    const save = vi.fn(async () => ({ printer: "Xprinter XP-80", paperMm: 80, path: "driver", autoPrint: "all", drawer: false, footer: "", invoicePrinter: "" }));
    const set = async () => ({ printer: "Xprinter XP-80", paperMm: 80, path: "driver", autoPrint: "none", drawer: false, footer: "", invoicePrinter: "" });
    renderWithProviders(<SettingsScreen />, { client: fakeClient({ printers: { settings: set, save } }), locale: "en" });
    await settle();

    expect(screen.getByTestId("settings-printer-state")).toHaveTextContent("Xprinter XP-80");
    await userEvent.selectOptions(screen.getByLabelText("Print receipts straight away"), "all");
    await userEvent.click(screen.getByRole("button", { name: "Save the settings" }));
    await settle();

    expect(save).toHaveBeenCalledWith(expect.objectContaining({ autoPrint: "all", printer: "Xprinter XP-80" }));
  });
});

describe("SettingsScreen — US dollars only (the owner's request, 2026-09-24)", () => {
  it("shows Go's conversion before anything is written, and writes the plan it showed", async () => {
    const update = vi.fn(async () => aSettings());
    const switchToUsdOnly = vi.fn(async () => aSettings({ moneyDisplay: "usd" }));
    renderWithProviders(<SettingsScreen />, { client: fakeClient({ settings: { update, switchToUsdOnly } }), locale: "en" });
    await settle();
    await userEvent.selectOptions(screen.getByLabelText("How money is shown"), "usd");
    const dialog = await screen.findByRole("dialog", { name: "Switch to US dollars only" });
    await settle();
    expect(screen.getAllByTestId("usd-only-price")[0]).toHaveTextContent("60,000 SYP");
    expect(screen.getAllByTestId("usd-only-price")[0]).toHaveTextContent("4.00 USD");
    expect(screen.getAllByTestId("usd-only-price")[1]).toHaveTextContent("less than a cent — becomes a cent");
    expect(screen.getByTestId("usd-only-customers")).toHaveTextContent("10.00 USD");
    expect(screen.getByTestId("usd-only-suppliers")).toHaveTextContent("20.00 USD");
    expect(screen.getByTestId("usd-only-drawer")).toHaveTextContent("25.00 USD");

    const go = screen.getByRole("button", { name: "Switch to US dollars only" });
    expect(go).toBeDisabled(); // the owner says they checked the figures first
    await userEvent.click(screen.getByLabelText("I have checked these figures. Convert everything to US dollars."));
    await userEvent.click(go);
    await settle();
    expect(switchToUsdOnly).toHaveBeenCalledWith("plan-token");
    expect(update).not.toHaveBeenCalled(); // never as a plain setting
    expect(dialog).not.toBeInTheDocument();
    expect(screen.getByText("The shop now counts in US dollars only.")).toBeInTheDocument();
  });

  it("a plan that moved is read again and asked again", async () => {
    const switchToUsdOnly = vi.fn(async () => {
      throw new BindingError({ code: "lite.usdmode.plan_changed", messageKey: "lite.usdmode.plan_changed" });
    });
    const usdOnlyPlan = vi.fn(async () => aUSDOnlyPlan());
    renderWithProviders(<SettingsScreen />, { client: fakeClient({ settings: { switchToUsdOnly, usdOnlyPlan } }), locale: "en" });
    await settle();
    await userEvent.selectOptions(screen.getByLabelText("How money is shown"), "usd");
    await screen.findByRole("dialog", { name: "Switch to US dollars only" });
    await settle();
    await userEvent.click(screen.getByLabelText("I have checked these figures. Convert everything to US dollars."));
    await userEvent.click(screen.getByRole("button", { name: "Switch to US dollars only" }));
    await settle();
    expect(usdOnlyPlan).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("button", { name: "Switch to US dollars only" })).toBeDisabled();
    expect(screen.getByText(/Something changed since the conversion was shown/)).toBeInTheDocument();
  });
});
