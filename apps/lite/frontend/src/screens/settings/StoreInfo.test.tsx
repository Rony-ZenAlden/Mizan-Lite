import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { ShopInput, ShopState } from "@/api/client";
import { BindingError } from "@/api/envelope";
import { aShop, fakeClient, renderWithProviders } from "@/api/testing";
import { StoreInfo } from "./StoreInfo";

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}

describe("StoreInfo — what heads every document, set by the shop (2026-09-24)", () => {
  it("edits the name, phone, city and address, and saves them as the owner's act", async () => {
    const saveShop = vi
      .fn(async (input: ShopInput): Promise<ShopState> => aShop(input))
      .mockImplementationOnce(async () => {
        throw new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
      });
    renderWithProviders(<StoreInfo />, { client: fakeClient({ settings: { saveShop } }), locale: "en" });
    await settle();
    expect(screen.getByLabelText("The shop's name")).toHaveValue("بقالية المونة");
    await userEvent.clear(screen.getByLabelText("The shop's name"));
    await userEvent.type(screen.getByLabelText("The shop's name"), "كردي");
    await userEvent.type(screen.getByLabelText("Address"), "السوق المسقوف");
    await userEvent.click(screen.getByRole("button", { name: "Save store information" }));
    await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await settle();
    expect(saveShop).toHaveBeenLastCalledWith({ name: "كردي", phone: "0933 123 456", city: "عفرين", address: "السوق المسقوف" });
    expect(screen.getByTestId("store-saved")).toBeInTheDocument();
  });

  it("with no logo, shows the name as the documents set it in type", async () => {
    renderWithProviders(<StoreInfo />, { locale: "en" });
    await settle();
    expect(screen.getByTestId("store-logo-typeset")).toHaveTextContent("بقالية المونة");
    expect(screen.queryByTestId("store-logo-image")).not.toBeInTheDocument();
    expect(screen.getByText("No logo — the shop's name is printed in large type instead.")).toBeInTheDocument();
  });

  it("picks a logo file, then saves it — and a cancelled pick changes nothing", async () => {
    const setLogo = vi.fn(async () => aShop({ logo: "iVBORw0KGgo=", logoWidth: 300, logoHeight: 120 }));
    const pickLogoFile = vi
      .fn(async () => ({ path: "/Users/shop/Pictures/logo.png", cancelled: false }))
      .mockImplementationOnce(async () => ({ path: "", cancelled: true }));
    renderWithProviders(<StoreInfo />, { client: fakeClient({ settings: { setLogo, pickLogoFile } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Add a logo…" }));
    await settle();
    expect(setLogo).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: "Add a logo…" }));
    await settle();
    expect(setLogo).toHaveBeenCalledWith("/Users/shop/Pictures/logo.png");
    const logo = within(screen.getByTestId("store-logo")).getByRole("img", { name: "بقالية المونة logo" });
    expect(logo).toHaveAttribute("src", "data:image/png;base64,iVBORw0KGgo=");
    expect(screen.getByRole("button", { name: "Change the logo…" })).toBeInTheDocument();
  });

  it("removes a logo, and says why a picture was refused", async () => {
    const removeLogo = vi.fn(async () => aShop());
    const setLogo = vi.fn(async (): Promise<ShopState> => {
      throw new BindingError({ code: "lite.settings.logo_invalid", messageKey: "lite.settings.logo_invalid" });
    });
    renderWithProviders(<StoreInfo />, {
      client: fakeClient({ settings: { shop: async () => aShop({ logo: "iVBORw0KGgo=", logoWidth: 10, logoHeight: 10 }), removeLogo, setLogo } }),
      locale: "en",
    });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Change the logo…" }));
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent("The logo must be a PNG or JPEG picture.");
    await userEvent.click(screen.getByRole("button", { name: "Remove the logo" }));
    await settle();
    expect(removeLogo).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("store-logo-typeset")).toBeInTheDocument();
  });
});
