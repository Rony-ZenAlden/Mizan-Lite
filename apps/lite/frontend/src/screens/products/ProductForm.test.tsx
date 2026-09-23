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
      costPrice: "", marginPercent: "", marginAmount: "", openPrice: false, unitsPerCarton: "",
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
    expect(updateProduct).toHaveBeenCalledWith({ id: existing.id, rowVersion: 3, nameAr: existing.nameAr, nameEn: "Olive oil, local", barcode: "",
      unitsPerCarton: "" });
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
    expect(setPrice).toHaveBeenLastCalledWith({ id: existing.id, rowVersion: 4, priceCurrency: "USD", price: "3.50", costPrice: "", marginPercent: "", marginAmount: "" });
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


describe("ProductForm — what a package opens into", () => {
  const units = async () => [
    { code: "tin", kind: "count", inputDecimals: 0 },
    { code: "l", kind: "volume", inputDecimals: 3 },
  ];
  const tin = aProduct({ id: "tin", nameAr: "تنكة زيت", nameEn: "Oil tin", unitCode: "tin" });
  const loose = aProduct({ id: "loose", nameAr: "زيت فرط", nameEn: "Loose oil", unitCode: "l" });

  it("is not offered for a product sold by volume or weight", async () => {
    renderWithProviders(<ProductForm product={loose} onSaved={() => undefined} onClose={() => undefined} />, {
      client: fakeClient({ catalog: { units, products: async () => [tin, loose] } }),
      locale: "en",
    });
    await settle();
    expect(screen.queryByRole("group", { name: "Opens into" })).not.toBeInTheDocument();
  });

  it("links a tin to loose oil, checking the content's decimals, and removes the link", async () => {
    const setPackage = vi.fn(async () => aProduct({ ...tin, packageContentId: "loose", packageContentQuantity: "16.000" }));
    const clearPackage = vi.fn(async () => tin);
    const onSaved = vi.fn();
    renderWithProviders(<ProductForm product={tin} onSaved={onSaved} onClose={() => undefined} />, {
      client: fakeClient({ catalog: { units, products: async () => [tin, loose], setPackage, clearPackage } }),
      locale: "en",
    });
    await settle();

    const section = screen.getByRole("group", { name: "Opens into" });
    const options = within(within(section).getByLabelText("Loose product")).getAllByRole("option").map((o) => o.textContent);
    expect(options).toEqual(["Not linked", "Loose oil — Litre"]); // never itself
    await userEvent.selectOptions(within(section).getByLabelText("Loose product"), "loose");
    await userEvent.type(within(section).getByLabelText("Quantity in one package"), "16.0005");
    expect(within(section).getByText("This unit takes at most 3 decimal places.")).toBeInTheDocument();
    expect(within(section).getByRole("button", { name: "Save link" })).toBeDisabled();

    await userEvent.clear(within(section).getByLabelText("Quantity in one package"));
    await userEvent.type(within(section).getByLabelText("Quantity in one package"), "16");
    await userEvent.click(within(section).getByRole("button", { name: "Save link" }));
    await settle();
    expect(setPackage).toHaveBeenCalledWith({ packageProductId: "tin", contentProductId: "loose", contentQuantity: "16" });
    expect(within(section).getByText("Link saved.")).toBeInTheDocument();
    expect(within(section).getByLabelText("Quantity in one package")).toHaveValue("16.000");
    expect(onSaved).not.toHaveBeenCalled(); // the link saves on its own; the form stays open

    await userEvent.click(within(section).getByRole("button", { name: "Remove link" }));
    await settle();
    expect(clearPackage).toHaveBeenCalledWith("tin");
    expect(within(section).getByLabelText("Loose product")).toHaveValue("");
    expect(within(section).queryByRole("button", { name: "Remove link" })).not.toBeInTheDocument();
  });

  it("shows a nesting refusal from Go", async () => {
    const setPackage = vi.fn(async () => {
      throw new BindingError({
        code: "lite.catalog.package_nested",
        messageKey: "lite.catalog.package_nested",
        fields: [{ field: "contentProductId", code: "lite.catalog.package_nested", messageKey: "lite.catalog.package_nested" }],
      });
    });
    renderWithProviders(<ProductForm product={tin} onSaved={() => undefined} onClose={() => undefined} />, {
      client: fakeClient({ catalog: { units, products: async () => [tin, loose], setPackage } }),
      locale: "en",
    });
    await settle();
    const section = screen.getByRole("group", { name: "Opens into" });
    await userEvent.selectOptions(within(section).getByLabelText("Loose product"), "loose");
    await userEvent.type(within(section).getByLabelText("Quantity in one package"), "12");
    await userEvent.click(within(section).getByRole("button", { name: "Save link" }));
    expect(await within(section).findByText(/Packages open one level only/)).toBeInTheDocument();
  });
});

describe("ProductForm — cost price and margin (owner's request, 2026-09-16)", () => {
  it("sends the cost and the margin the shopkeeper typed last, and lets Go do the arithmetic", async () => {
    const createProduct = vi.fn(async () => aProduct({ price: "2.50", costPrice: "2.00", marginAmount: "0.50", marginPercent: "25.0" }));
    renderWithProviders(<ProductForm onSaved={() => {}} onClose={() => {}} />, {
      client: fakeClient({ catalog: { createProduct } }),
      locale: "en",
    });
    await settle();

    await userEvent.type(screen.getByLabelText("Arabic name"), "سمنة");
    await userEvent.selectOptions(screen.getByLabelText("Selling unit"), "kg");
    await userEvent.selectOptions(screen.getByLabelText("Price currency"), "USD");
    await userEvent.type(screen.getByLabelText("Price"), "0.01");

    // The margin boxes stay shut until a cost is there to take a margin of.
    expect(screen.getByLabelText("Percent %")).toBeDisabled();
    await userEvent.type(screen.getByLabelText("Cost price (capital)"), "2.00");
    expect(screen.getByLabelText("Percent %")).toBeEnabled();

    await userEvent.type(screen.getByLabelText("Percent %"), "25");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await settle();

    // The percentage was the last box touched, so it is the one sent; the amount is left empty for Go to compute.
    expect(createProduct).toHaveBeenCalledWith(
      expect.objectContaining({ costPrice: "2.00", marginPercent: "25", marginAmount: "" }),
    );
  });

  it("shows the margin Go worked out, and says when a product is sold below cost", async () => {
    const below = aProduct({ price: "1.50", costPrice: "2.00", marginAmount: "-0.50", marginPercent: "-25.0" });
    renderWithProviders(<ProductForm product={below} onSaved={() => {}} onClose={() => {}} />, {
      client: fakeClient(),
      locale: "en",
    });
    await settle();
    const shown = screen.getByTestId("product-margin");
    expect(shown).toHaveTextContent("-0.50");
    expect(shown).toHaveTextContent("-25.0%");
    expect(shown).toHaveTextContent("Sold below cost");
  });

  it("takes a cost price off when the box is emptied", async () => {
    const setPrice = vi.fn(async () => aProduct());
    const withCost = aProduct({ costPrice: "2.00", marginAmount: "0.50", marginPercent: "25.0" });
    renderWithProviders(<ProductForm product={withCost} onSaved={() => {}} onClose={() => {}} />, {
      client: fakeClient({ catalog: { product: async () => withCost, setPrice } }),
      locale: "en",
    });
    await settle();
    await userEvent.clear(screen.getByLabelText("Cost price (capital)"));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await settle();
    // "-" is the word for "take it off"; "" would mean "leave it alone".
    expect(setPrice).toHaveBeenCalledWith(expect.objectContaining({ costPrice: "-" }));
  });
});

describe("ProductForm — an open-price item (owner's request, 2026-09-23)", () => {
  it("takes no price, cost or reorder level, and says the choice cannot be undone", async () => {
    const createProduct = vi.fn(async () => aProduct({ id: "bag", openPrice: true, price: "0" }));
    const onSaved = vi.fn();
    renderWithProviders(<ProductForm onSaved={onSaved} onClose={() => undefined} />, {
      client: fakeClient({ catalog: { createProduct } }),
      locale: "en",
    });
    await settle();
    await userEvent.type(screen.getByLabelText("Arabic name"), "كيس");
    await userEvent.click(screen.getByLabelText("Open price — typed at the till, never counted in stock"));
    expect(screen.getByTestId("product-open-price")).toHaveTextContent("cannot be changed later");
    // The fields that would hold figures the item never uses are gone.
    expect(screen.queryByLabelText("Price")).not.toBeInTheDocument();
    expect(screen.queryByTestId("product-cost")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Tell me when stock reaches")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(createProduct).toHaveBeenCalledWith(expect.objectContaining({ nameAr: "كيس", openPrice: true, price: "", costPrice: "" }));
    expect(onSaved).toHaveBeenCalled();
  });
});

describe("ProductForm — the carton an invoice counts in (0.10.0)", () => {
  it("sends how many of the unit make one carton, on create and when it is changed", async () => {
    const createProduct = vi.fn(async () => aProduct({ id: "new" }));
    renderWithProviders(<ProductForm onSaved={() => undefined} onClose={() => undefined} />, { client: fakeClient({ catalog: { createProduct } }), locale: "en" });
    await settle();
    await userEvent.type(screen.getByLabelText("Arabic name"), "فنجان قهوة");
    await userEvent.selectOptions(screen.getByLabelText("Selling unit"), "jar");
    await userEvent.type(screen.getByLabelText("Price"), "18");
    await userEvent.type(screen.getByLabelText("Units per carton"), "٦");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await settle();
    expect(createProduct).toHaveBeenCalledWith(expect.objectContaining({ unitsPerCarton: "٦" }));
  });

  it("an edited carton size is saved without the owner's PIN — it changes no price", async () => {
    const existing = aProduct({ id: "p1", rowVersion: 2, unitsPerCarton: "6" });
    const updateProduct = vi.fn(async () => aProduct({ id: "p1", rowVersion: 3, unitsPerCarton: "12" }));
    renderWithProviders(<ProductForm product={existing} onSaved={() => undefined} onClose={() => undefined} />, {
      client: fakeClient({ catalog: { updateProduct, product: async () => existing } }),
      locale: "en",
    });
    await settle();
    expect(screen.getByLabelText("Units per carton")).toHaveValue("6");
    await userEvent.clear(screen.getByLabelText("Units per carton"));
    await userEvent.type(screen.getByLabelText("Units per carton"), "12");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await settle();
    expect(updateProduct).toHaveBeenCalledWith(expect.objectContaining({ id: "p1", rowVersion: 2, unitsPerCarton: "12" }));
    expect(screen.queryByRole("dialog", { name: "Owner PIN required" })).not.toBeInTheDocument();
  });
});
