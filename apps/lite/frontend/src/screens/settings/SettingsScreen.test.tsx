import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aRate, aSettings, fakeClient, renderWithProviders } from "@/api/testing";
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

    // The currency group shows what the shop has, and points at where each is changed rather than duplicating a control.
    const currency = screen.getByTestId("settings-currency");
    expect(currency).toHaveTextContent("500");
    expect(currency).toHaveTextContent("The smallest note is set on the Cash screen");
    // And it says the redenomination is coming rather than offering a switch that does nothing yet.
    expect(screen.getByTestId("redenomination-note")).toHaveTextContent("arrives in a later release");
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
