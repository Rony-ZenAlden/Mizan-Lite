import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aProduct, fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import { ProductForm } from "./ProductForm";

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
}

describe("ProductForm — create", () => {
  it("sends what was typed — Go normalises it — and reports the saved product", async () => {
    const createProduct = vi.fn(async () => aProduct({ id: "new" }));
    const onSaved = vi.fn();
    renderWithProviders(<ProductForm onSaved={onSaved} onClose={() => undefined} />, { client: fakeClient({ catalog: { createProduct } }), locale: "en" });
    await settle();

    await userEvent.type(screen.getByLabelText("Arabic name"), "دبس رمان");
    await userEvent.type(screen.getByLabelText("English name (optional)"), "Pomegranate molasses");
    await userEvent.type(screen.getByLabelText("Barcode (optional)"), "٦٢٢٣");
    await userEvent.selectOptions(screen.getByLabelText("Selling unit"), "jar");
    await userEvent.selectOptions(screen.getByLabelText("Price currency"), "SYP");
    await userEvent.type(screen.getByLabelText("Price"), "٤٥٠٠٠");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await settle();

    expect(createProduct).toHaveBeenCalledWith({
      nameAr: "دبس رمان", nameEn: "Pomegranate molasses", barcode: "٦٢٢٣", unitCode: "jar", priceCurrency: "SYP", price: "٤٥٠٠٠",
    });
    expect(onSaved).toHaveBeenCalledWith(aProduct({ id: "new" }));
  });

  it("names a mistyped price as it is typed and will not save it", async () => {
    const createProduct = vi.fn(async () => aProduct());
    renderWithProviders(<ProductForm onSaved={() => undefined} onClose={() => undefined} />, { client: fakeClient({ catalog: { createProduct } }), locale: "en" });
    await settle();
    await userEvent.type(screen.getByLabelText("Arabic name"), "زيت");

    await userEvent.type(screen.getByLabelText("Price"), "45,000");
    expect(screen.getByText("Don't use thousands separators. Type the number without commas.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    await userEvent.clear(screen.getByLabelText("Price"));
    await userEvent.type(screen.getByLabelText("Price"), "cheap");
    expect(screen.getByText("That is not a valid number.")).toBeInTheDocument();
    expect(createProduct).not.toHaveBeenCalled();
  });

  it("shows a duplicate under the name, and offers reactivation when the other product is inactive", async () => {
    const createProduct = vi.fn(async () => {
      throw new BindingError({
        code: "lite.catalog.duplicate_name",
        messageKey: "lite.catalog.duplicate_name",
        params: { existingName: "زيت زيتون", existingActive: "false" },
        fields: [{ field: "nameAr", code: "lite.catalog.duplicate_name", messageKey: "lite.catalog.duplicate_name" }],
      });
    });
    renderWithProviders(<ProductForm onSaved={() => undefined} onClose={() => undefined} />, { client: fakeClient({ catalog: { createProduct } }), locale: "en" });
    await settle();
    await userEvent.type(screen.getByLabelText("Arabic name"), "زيـت زيتون");
    await userEvent.type(screen.getByLabelText("Price"), "3");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("A product with this name already exists: “زيت زيتون”.")).toBeInTheDocument();
    expect(screen.getByText(/you can reactivate it/)).toBeInTheDocument();
  });
});

describe("ProductForm — edit", () => {
  const existing = aProduct({ rowVersion: 3 });

  it("shows the unit but will not change it", async () => {
    renderWithProviders(<ProductForm product={existing} onSaved={() => undefined} onClose={() => undefined} />, { locale: "en" });
    await settle();
    expect(screen.getByLabelText("Selling unit")).toBeDisabled();
    expect(screen.getByText("The selling unit cannot change after the product is created.")).toBeInTheDocument();
  });

  it("an unchanged price is not sent, so no PIN is asked for", async () => {
    const setPrice = vi.fn(async () => existing);
    const updateProduct = vi.fn(async () => aProduct({ nameEn: "Olive oil, local", rowVersion: 4 }));
    const product = vi.fn(async () => existing);
    const onSaved = vi.fn();
    renderWithProviders(<ProductForm product={existing} onSaved={onSaved} onClose={() => undefined} />, {
      client: fakeClient({ catalog: { setPrice, updateProduct, product } }),
      locale: "en",
    });
    await settle();
    await userEvent.type(screen.getByLabelText("English name (optional)"), ", local");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await settle();

    expect(product).toHaveBeenCalledWith(existing.id); // read fresh before editing
    expect(updateProduct).toHaveBeenCalledWith({ id: existing.id, rowVersion: 3, nameAr: existing.nameAr, nameEn: "Olive oil, local", barcode: "" });
    expect(setPrice).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog", { name: "Owner PIN required" })).not.toBeInTheDocument();
    expect(onSaved).toHaveBeenCalled();
  });

  it("a changed price goes through the owner PIN, with the version the name edit produced", async () => {
    let priceCalls = 0;
    const setPrice = vi.fn(async (input: { id: string; rowVersion: number; priceCurrency: string; price: string }) => {
      priceCalls += 1;
      if (priceCalls === 1) throw new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
      return aProduct({ ...input, rowVersion: input.rowVersion + 1 });
    });
    const updateProduct = vi.fn(async () => aProduct({ nameAr: "زيت زيتون بلدي", rowVersion: 4 }));
    const onSaved = vi.fn();
    renderWithProviders(<ProductForm product={existing} onSaved={onSaved} onClose={() => undefined} />, {
      client: fakeClient({ catalog: { setPrice, updateProduct, product: async () => existing } }),
      locale: "en",
    });
    await settle();
    await userEvent.type(screen.getByLabelText("Arabic name"), " بلدي");
    await userEvent.clear(screen.getByLabelText("Price"));
    await userEvent.type(screen.getByLabelText("Price"), "3.50");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await settle();

    expect(setPrice).toHaveBeenCalledTimes(2);
    expect(setPrice).toHaveBeenLastCalledWith({ id: existing.id, rowVersion: 4, priceCurrency: "USD", price: "3.50" });
    expect(onSaved).toHaveBeenCalled();
  });

  it("cancelling the PIN keeps the form open and shows no error", async () => {
    const setPrice = vi.fn(async () => {
      throw new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
    });
    const onSaved = vi.fn();
    renderWithProviders(<ProductForm product={existing} onSaved={onSaved} onClose={() => undefined} />, {
      client: fakeClient({ catalog: { setPrice, product: async () => existing } }),
      locale: "en",
    });
    await settle();
    await userEvent.clear(screen.getByLabelText("Price"));
    await userEvent.type(screen.getByLabelText("Price"), "1");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await userEvent.click(within(await screen.findByRole("dialog", { name: "Owner PIN required" })).getByRole("button", { name: "Cancel" }));
    await settle();

    expect(onSaved).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog", { name: "Edit product" })).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

