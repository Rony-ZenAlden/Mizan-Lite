import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import { FirstRunGate } from "./FirstRun";

const shell = <p>the shell</p>;

async function fill(shop: string, pin: string, again: string) {
  await userEvent.type(await screen.findByLabelText("Shop name"), shop);
  await userEvent.type(screen.getByLabelText("Owner PIN"), pin);
  await userEvent.type(screen.getByLabelText("Owner PIN again"), again);
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

    expect(completeFirstRun).toHaveBeenCalledWith({ shopName: "بقالية المونة", locale: "en", pin: "246813" });
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
});
