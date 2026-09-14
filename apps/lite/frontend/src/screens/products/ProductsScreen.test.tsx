import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aProduct, fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import { SEARCH_DEBOUNCE_MS, ProductsScreen } from "./ProductsScreen";

const required = () => new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, SEARCH_DEBOUNCE_MS + 50));
  });
}

describe("ProductsScreen", () => {
  it("lists products with translated units, grouped Latin-digit prices and currency names", async () => {
    const products = vi.fn(async () => [aProduct({ nameAr: "دبس رمان", unitCode: "jar", priceCurrency: "SYP", price: "45000", convertedPrice: "3.00", convertedCurrency: "USD" })]);
    renderWithProviders(<ProductsScreen />, { client: fakeClient({ catalog: { products } }), locale: "ar" });
    await settle();

    const row = screen.getByRole("row", { name: /دبس رمان/ });
    expect(within(row).getByText("مرطبان")).toBeInTheDocument();
    expect(within(row).getByText("45,000")).toBeInTheDocument();
    expect(within(row).getByText(/ليرة سورية/)).toBeInTheDocument();
    // The price in the other currency, as Go computed it at the rate in force (Q-L3.5).
    expect(within(row).getByText("≈ 3.00")).toBeInTheDocument();
    expect(within(row).getByText(/دولار أمريكي/)).toBeInTheDocument();
    expect(products).toHaveBeenCalledWith({ text: "", includeInactive: false });
  });

  it("shows the English name in English when there is one", async () => {
    renderWithProviders(<ProductsScreen />, { locale: "en" });
    await settle();
    expect(screen.getByRole("row", { name: /Olive oil/ })).toBeInTheDocument();
  });

  it("searches once typing pauses, not on every keystroke", async () => {
    const products = vi.fn(async () => [aProduct()]);
    renderWithProviders(<ProductsScreen />, { client: fakeClient({ catalog: { products } }), locale: "en" });
    await settle();
    products.mockClear();

    await userEvent.type(screen.getByLabelText("Search by name or barcode"), "زيت");
    await settle();
    expect(products).toHaveBeenCalledTimes(1);
    expect(products).toHaveBeenLastCalledWith({ text: "زيت", includeInactive: false });

    await userEvent.click(screen.getByLabelText("Show inactive"));
    await settle();
    expect(products).toHaveBeenLastCalledWith({ text: "زيت", includeInactive: true });
  });

  it("says there are no results, or no products at all", async () => {
    renderWithProviders(<ProductsScreen />, { client: fakeClient({ catalog: { products: async () => [] } }), locale: "en" });
    await settle();
    expect(screen.getByText("No products yet.")).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Search by name or barcode"), "x");
    await settle();
    expect(screen.getByText("No results.")).toBeInTheDocument();
  });

  it("deactivation goes through the owner PIN; reactivation does not", async () => {
    let calls = 0;
    const setActive = vi.fn(async (input: { id: string; rowVersion: number; active: boolean }) => {
      calls += 1;
      if (!input.active && calls === 1) throw required();
      return aProduct({ active: input.active });
    });
    const elevate = vi.fn(async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: 120 }));
    const products = vi.fn(async () => [aProduct({ id: "a", nameEn: "Oil", active: true }), aProduct({ id: "b", nameAr: "سمن", nameEn: "Ghee", active: false, rowVersion: 4 })]);
    renderWithProviders(<ProductsScreen />, { client: fakeClient({ catalog: { products, setActive }, owner: { elevate } }), locale: "en" });
    await settle();

    await userEvent.click(within(screen.getByRole("row", { name: /Oil/ })).getByRole("button", { name: "Deactivate" }));
    await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await settle();
    expect(setActive).toHaveBeenCalledTimes(2);
    expect(elevate).toHaveBeenCalledTimes(1);

    await userEvent.click(within(screen.getByRole("row", { name: /Ghee/ })).getByRole("button", { name: "Reactivate" }));
    await settle();
    expect(setActive).toHaveBeenLastCalledWith({ id: "b", rowVersion: 4, active: true });
    expect(elevate).toHaveBeenCalledTimes(1);
  });

  it("puts a product on a till button", async () => {
    const setQuickSlot = vi.fn(async () => aProduct({ quickSlot: 7 }));
    renderWithProviders(<ProductsScreen />, { client: fakeClient({ catalog: { setQuickSlot } }), locale: "en" });
    await settle();
    await userEvent.selectOptions(screen.getByLabelText("Quick button for Olive oil"), "7");
    await settle();
    expect(setQuickSlot).toHaveBeenCalledWith({ id: aProduct().id, slot: 7 });
    expect(screen.getByLabelText("Quick button for Olive oil").querySelectorAll("option")).toHaveLength(25);
  });

  it("an inactive product's till button cannot be chosen", async () => {
    renderWithProviders(<ProductsScreen />, { client: fakeClient({ catalog: { products: async () => [aProduct({ active: false })] } }), locale: "en" });
    await settle();
    expect(screen.getByLabelText("Quick button for Olive oil")).toBeDisabled();
  });
});
