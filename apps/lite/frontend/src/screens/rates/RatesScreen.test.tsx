import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aFetch, aRate, fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import type { SetRateInput } from "@/api/client";
import { RatesScreen } from "./RatesScreen";

const required = () => new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
const elevated = { setUp: true, lockedSeconds: 0, elevatedSeconds: 120 };

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}

async function enterPin() {
  await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
  await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
  await settle();
}

describe("RatesScreen", () => {
  it("shows the rate in force with who set it and how old it is, and the history with its correction", async () => {
    renderWithProviders(<RatesScreen />, { locale: "en" });
    await settle();
    const card = screen.getByTestId("rate-in-force");
    expect(card).toHaveTextContent("1 USD = 15,000 Syrian pound");
    expect(card).toHaveTextContent("Set by the owner · Recorded 3 hours ago");
    const rows = screen.getAllByRole("row").slice(1);
    expect(rows).toHaveLength(2);
    expect(within(rows[0]!).getByText("-1.3%")).toBeInTheDocument();
    expect(within(rows[0]!).getByText("تصحيح")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("—")).toBeInTheDocument();
  });

  it("says plainly when there is no rate, and when the rate is not from today", async () => {
    renderWithProviders(<RatesScreen />, { client: fakeClient({ fx: { current: async () => aRate({ set: false, rate: "", ageSeconds: 0 }) } }), locale: "en" });
    await settle();
    expect(screen.getByText("No exchange rate yet — set one")).toBeInTheDocument();
    expect(screen.queryByText(/1 USD =/)).not.toBeInTheDocument();
  });

  it("warns when the rate was not updated today", async () => {
    renderWithProviders(<RatesScreen />, { client: fakeClient({ fx: { current: async () => aRate({ stale: true, ageSeconds: 30 * 3600 }) } }), locale: "ar" });
    await settle();
    expect(screen.getByText("لم يُحدَّث اليوم")).toBeInTheDocument();
    expect(screen.getByText(/حدّثه من الإنترنت، أو أدخل سعر اليوم يدوياً/)).toBeInTheDocument();
  });

  it("updates from the internet on request and shows what the check decided", async () => {
    const refresh = vi.fn(async () => aRate({ hasFetch: true, lastFetch: aFetch({ outcome: "held_today", change: "-13.3", ageSeconds: 5 }) }));
    renderWithProviders(<RatesScreen />, { client: fakeClient({ fx: { refresh } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Update now" }));
    await settle();
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(screen.getByText(/Last internet check just now/)).toBeInTheDocument();
    expect(screen.getByText(/Internet rate: 13,007.5355 \(Currency API\) — -13.3% from the rate in force/)).toBeInTheDocument();
    expect(screen.getByText(/Not applied: the owner set today's rate manually./)).toBeInTheDocument();
    expect(screen.getByText(/can differ from the market rate/)).toBeInTheDocument();
  });

  it("shows an offline check as its message, and a copy with no provider cannot update", async () => {
    const current = async () => aRate({ hasFetch: true, canFetch: false, lastFetch: aFetch({ outcome: "failed", rate: "", provider: "", errorCode: "lite.fx.fetch_offline", change: "" }) });
    renderWithProviders(<RatesScreen />, { client: fakeClient({ fx: { current } }), locale: "en" });
    await settle();
    expect(screen.getByText(/No internet connection/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Update now" })).toBeDisabled();
    expect(screen.getByText("This copy of the application has no internet rate provider.")).toBeInTheDocument();
  });

  it("offers a proposal for the owner's approval, through the PIN", async () => {
    const proposal = aFetch({ id: "f9", outcome: "proposed", provider: "exchangerate-api-open", rate: "121.9579", change: "-99.2", acceptable: true });
    let calls = 0;
    const acceptProposal = vi.fn(async () => {
      calls += 1;
      if (calls === 1) throw required();
      return aRate({ rate: "121.9579", source: "fetched", hasFetch: true, lastFetch: { ...proposal, acceptable: false, outcome: "proposed" } });
    });
    const elevate = vi.fn(async () => elevated);
    renderWithProviders(<RatesScreen />, { client: fakeClient({ fx: { current: async () => aRate({ hasFetch: true, lastFetch: proposal }), acceptProposal }, owner: { elevate } }), locale: "en" });
    await settle();

    const banner = screen.getByRole("alert");
    expect(banner).toHaveTextContent("The internet rate 121.9579 was not applied");
    expect(banner).toHaveTextContent("It is -99.2% from the rate in force.");
    await userEvent.click(within(banner).getByRole("button", { name: "Accept this rate" }));
    await enterPin();
    expect(acceptProposal).toHaveBeenLastCalledWith("f9");
    expect(elevate).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("rate-in-force")).toHaveTextContent("1 USD = 121.9579 Syrian pound");
    expect(screen.queryByRole("button", { name: "Accept this rate" })).not.toBeInTheDocument();
  });

  it("sets the rate manually through the PIN, and a change beyond 20% asks again with both figures", async () => {
    const setRate = vi.fn(async (input: SetRateInput) => {
      if (input.rate === "15.000" && !input.confirmLargeChange) {
        throw new BindingError({
          code: "lite.fx.large_change",
          messageKey: "lite.fx.large_change",
          params: { inForce: "15000", typed: "15", percent: "-99.9" },
          fields: [{ field: "rate", code: "lite.fx.large_change", messageKey: "lite.fx.large_change" }],
        });
      }
      return aRate({ rate: input.rate === "15.000" ? "15" : input.rate });
    });
    renderWithProviders(<RatesScreen />, { client: fakeClient({ fx: { setRate } }), locale: "en" });
    await settle();

    await userEvent.type(screen.getByLabelText("Syrian pound per 1 USD"), "15.000");
    await userEvent.click(screen.getByRole("button", { name: "Save rate" }));
    const dialog = await screen.findByRole("dialog", { name: "Is this rate right?" });
    expect(dialog).toHaveTextContent("The rate in force is 15000. You entered 15 — a change of -99.9%.");

    await userEvent.click(within(dialog).getByRole("button", { name: "Go back" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(setRate).toHaveBeenCalledTimes(1);

    await userEvent.click(screen.getByRole("button", { name: "Save rate" }));
    await userEvent.click(within(await screen.findByRole("dialog", { name: "Is this rate right?" })).getByRole("button", { name: "Yes, use 15" }));
    await settle();
    expect(setRate).toHaveBeenLastCalledWith({ rate: "15.000", note: "", confirmLargeChange: true });
    expect(screen.getByText("The rate was saved.")).toBeInTheDocument();
  });

  it("a manual rate outside owner mode asks for the PIN, and cancelling says nothing", async () => {
    const setRate = vi.fn(async () => {
      throw required();
    });
    renderWithProviders(<RatesScreen />, { client: fakeClient({ fx: { setRate } }), locale: "en" });
    await settle();
    await userEvent.type(screen.getByLabelText("Syrian pound per 1 USD"), "15100");
    await userEvent.click(screen.getByRole("button", { name: "Save rate" }));
    await userEvent.click(within(await screen.findByRole("dialog", { name: "Owner PIN required" })).getByRole("button", { name: "Cancel" }));
    await settle();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("switches mode through the PIN", async () => {
    let calls = 0;
    const setMode = vi.fn(async (mode: "automatic" | "manual") => {
      calls += 1;
      if (calls === 1) throw required();
      return aRate({ mode });
    });
    renderWithProviders(<RatesScreen />, { client: fakeClient({ fx: { setMode }, owner: { elevate: async () => elevated } }), locale: "en" });
    await settle();
    expect(screen.getByText(/Automatic — fetched from the internet/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Switch to manual" }));
    await enterPin();
    expect(setMode).toHaveBeenLastCalledWith("manual");
    expect(screen.getByText(/Manual — set by the owner/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Switch to automatic" })).toBeInTheDocument();
  });
});

describe("RatesScreen — cash rounding", () => {
  it("shows the smallest note and changes it with the owner's PIN", async () => {
    const setCashNote = vi
      .fn(async (note: string) => ({ currency: "SYP", note: note === "١٠٠٠" ? "1000" : note }))
      .mockImplementationOnce(async () => {
        throw required();
      });
    renderWithProviders(<RatesScreen />, { client: fakeClient({ till: { setCashNote }, owner: { elevate: async () => elevated } }), locale: "en" });
    await settle();
    const section = screen.getByTestId("cash-note");
    expect(section).toHaveTextContent("Totals in Syrian pound are rounded to the nearest 500.");
    await userEvent.type(within(section).getByLabelText("Smallest note"), "١٠٠٠");
    await userEvent.click(within(section).getByRole("button", { name: "Save note" }));
    await enterPin();
    expect(setCashNote).toHaveBeenCalledTimes(2);
    expect(setCashNote).toHaveBeenLastCalledWith("١٠٠٠");
    expect(section).toHaveTextContent("Totals in Syrian pound are rounded to the nearest 1,000.");
    expect(within(section).getByText("Smallest note saved.")).toBeInTheDocument();
  });

  it("Go's refusal of a note is shown under the field", async () => {
    const setCashNote = vi.fn(async () => {
      throw new BindingError({
        code: "lite.settings.invalid_cash_note",
        messageKey: "lite.settings.invalid_cash_note",
        params: { max: "1000000" },
        fields: [{ field: "cashNote", code: "lite.settings.invalid_cash_note", messageKey: "lite.settings.invalid_cash_note" }],
      });
    });
    renderWithProviders(<RatesScreen />, { client: fakeClient({ till: { setCashNote } }), locale: "en" });
    await settle();
    const section = screen.getByTestId("cash-note");
    await userEvent.type(within(section).getByLabelText("Smallest note"), "0");
    await userEvent.click(within(section).getByRole("button", { name: "Save note" }));
    expect(await within(section).findByText("The smallest note is a whole number from 1 to 1000000.")).toBeInTheDocument();
  });
});
