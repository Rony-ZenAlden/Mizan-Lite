import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import { FirstRunGate } from "./FirstRun";

const shell = <p>the shell</p>;

async function fill(shop: string, pin: string, again: string, rate = "15000") {
  await userEvent.type(await screen.findByLabelText("Shop name"), shop);
  await userEvent.type(screen.getByLabelText("Owner PIN"), pin);
  await userEvent.type(screen.getByLabelText("Owner PIN again"), again);
  await userEvent.type(screen.getByLabelText("Today's exchange rate (Syrian pound per 1 USD)"), rate);
}

describe("FirstRunGate", () => {
  it("renders the application once first run is complete", async () => {
    renderWithProviders(<FirstRunGate>{shell}</FirstRunGate>);
    expect(await screen.findByText("the shell")).toBeInTheDocument();
  });

  it("blocks the shell until the shop is set up, then shows the recovery code until it is written down", async () => {
    const completeFirstRun = vi.fn(async () => ({ recoveryCode: "ABCD-EFGH-JKMN-PQRS" }));
    const client = fakeClient({ app: { firstRunStatus: async () => ({ complete: false }), completeFirstRun } });
    renderWithProviders(<FirstRunGate>{shell}</FirstRunGate>, { client, locale: "en" });

    expect(await screen.findByRole("heading", { name: "Set up your shop" })).toBeInTheDocument();
    expect(screen.queryByText("the shell")).not.toBeInTheDocument();

    await fill("بقالية المونة", "246813", "246813");
    await userEvent.click(screen.getByRole("button", { name: "Continue" }));

    expect(completeFirstRun).toHaveBeenCalledWith({ shopName: "بقالية المونة", locale: "en", pin: "246813", rate: "15000" });
    expect(await screen.findByTestId("recovery-code")).toHaveTextContent("ABCD-EFGH-JKMN-PQRS");
    expect(screen.queryByText("the shell")).not.toBeInTheDocument();

    const start = screen.getByRole("button", { name: "Start" });
    expect(start).toBeDisabled();
    await userEvent.click(screen.getByLabelText("I have written the code down"));
    await userEvent.click(start);
    expect(await screen.findByText("the shell")).toBeInTheDocument();
  });

  it("refuses mismatched PINs on the screen, without asking Go", async () => {
    const completeFirstRun = vi.fn(async () => ({ recoveryCode: "x" }));
    const client = fakeClient({ app: { firstRunStatus: async () => ({ complete: false }), completeFirstRun } });
    renderWithProviders(<FirstRunGate>{shell}</FirstRunGate>, { client, locale: "en" });

    await fill("المونة", "246813", "246810");
    await userEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(await screen.findByText("The two PINs do not match.")).toBeInTheDocument();
    expect(completeFirstRun).not.toHaveBeenCalled();
  });

  it("shows Go's refusal under the field it concerns", async () => {
    const completeFirstRun = vi.fn(async () => {
      throw new BindingError({
        code: "lite.owner.pin_weak",
        messageKey: "lite.owner.pin_weak",
        fields: [{ field: "pin", code: "lite.owner.pin_weak", messageKey: "lite.owner.pin_weak" }],
      });
    });
    const client = fakeClient({ app: { firstRunStatus: async () => ({ complete: false }), completeFirstRun } });
    renderWithProviders(<FirstRunGate>{shell}</FirstRunGate>, { client, locale: "en" });

    await fill("المونة", "123456", "123456");
    await userEvent.click(screen.getByRole("button", { name: "Continue" }));
    const message = await screen.findByText(/easy to guess/);
    expect(screen.getByLabelText("Owner PIN")).toHaveAttribute("aria-describedby", message.id);
  });

  it("switches the interface language the moment it is chosen", async () => {
    const client = fakeClient({ app: { firstRunStatus: async () => ({ complete: false }) } });
    renderWithProviders(<FirstRunGate>{shell}</FirstRunGate>, { client, locale: "ar" });
    expect(await screen.findByRole("heading", { name: "إعداد المتجر" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "English" }));
    expect(document.documentElement).toHaveAttribute("dir", "ltr");
    expect(screen.getByRole("heading", { name: "Set up your shop" })).toBeInTheDocument();
  });

  it("reads the typed rate back, so 15.000 is seen to mean fifteen", async () => {
    const client = fakeClient({ app: { firstRunStatus: async () => ({ complete: false }) } });
    renderWithProviders(<FirstRunGate>{shell}</FirstRunGate>, { client, locale: "en" });
    const field = await screen.findByLabelText("Today's exchange rate (Syrian pound per 1 USD)");
    await userEvent.type(field, "15.000");
    expect(screen.getByTestId("rate-readback")).toHaveTextContent("1 USD = 15 Syrian pound");
    await userEvent.clear(field);
    await userEvent.type(field, "١٥٠٠٠");
    expect(screen.getByTestId("rate-readback")).toHaveTextContent("1 USD = 15,000 Syrian pound");
  });

  it("fetches a rate from the internet into the field, names where it came from, and still sends what the field holds", async () => {
    const fetchQuote = vi.fn(async () => ({ provider: "currency-api-jsdelivr", rate: "13007.5355" }));
    const completeFirstRun = vi.fn(async () => ({ recoveryCode: "ABCD-EFGH-JKMN-PQRS" }));
    const client = fakeClient({ app: { firstRunStatus: async () => ({ complete: false }), completeFirstRun }, fx: { fetchQuote } });
    renderWithProviders(<FirstRunGate>{shell}</FirstRunGate>, { client, locale: "en" });

    await userEvent.type(await screen.findByLabelText("Shop name"), "المونة");
    await userEvent.type(screen.getByLabelText("Owner PIN"), "246813");
    await userEvent.type(screen.getByLabelText("Owner PIN again"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "Fetch from the internet" }));
    const field = screen.getByLabelText("Today's exchange rate (Syrian pound per 1 USD)");
    await vi.waitFor(() => expect(field).toHaveValue("13007.5355"));
    expect(screen.getByText(/Fetched from Currency API. Check it against the market rate/)).toBeInTheDocument();

    // The owner corrects it to the market rate before continuing: what is sent is what the field holds.
    await userEvent.clear(field);
    await userEvent.type(field, "15000");
    await userEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(completeFirstRun).toHaveBeenCalledWith({ shopName: "المونة", locale: "en", pin: "246813", rate: "15000" });
  });

  it("offline, the fetch says so and the rate can still be typed", async () => {
    const fetchQuote = vi.fn(async () => {
      throw new BindingError({ code: "lite.fx.fetch_offline", messageKey: "lite.fx.fetch_offline" });
    });
    const client = fakeClient({ app: { firstRunStatus: async () => ({ complete: false }) }, fx: { fetchQuote } });
    renderWithProviders(<FirstRunGate>{shell}</FirstRunGate>, { client, locale: "en" });
    await userEvent.click(await screen.findByRole("button", { name: "Fetch from the internet" }));
    expect(await screen.findByText(/No internet connection/)).toBeInTheDocument();
    expect(screen.getByLabelText("Today's exchange rate (Syrian pound per 1 USD)")).toBeEnabled();
  });
});
