import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { BindingError } from "@/api/envelope";
import { fakeClient, renderWithProviders } from "@/api/testing";
import type { ImportPreview } from "@/api/client";
import { ProductsScreen } from "./ProductsScreen";

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}

const withProblems: ImportPreview = {
  path: "/Users/shop/products.xlsx",
  digest: "d1",
  rows: [{ row: 2, nameAr: "رز", nameEn: "Rice", barcode: "", unitCode: "kg", currency: "USD", price: "1.10", quantity: "", cost: "" }],
  problems: [
    { row: 5, column: "unit", code: "lite.import.unknown_unit", params: { value: "sack" } },
    { row: 7, column: "cost", code: "lite.import.cost_required", params: {} },
  ],
  cancelled: false,
};

describe("Products — import from Excel (Q-L8.9)", () => {
  it("saves the template, previews a workbook's problems by row and column, and imports nothing until they are fixed", async () => {
    const importTemplate = vi.fn(async () => ({ path: "/Users/shop/template.xlsx", bytes: 1, cancelled: false }));
    const importPreview = vi.fn(async (): Promise<ImportPreview> => withProblems);
    const importApply = vi.fn(async () => ({ created: 1, withStock: 0 }));
    renderWithProviders(<ProductsScreen />, { client: fakeClient({ catalog: { importTemplate, importPreview, importApply } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Import from Excel…" }));
    const dialog = await screen.findByRole("dialog", { name: "Import products from Excel" });

    await userEvent.click(within(dialog).getByRole("button", { name: "Save the template…" }));
    expect(await within(dialog).findByText("/Users/shop/template.xlsx")).toBeInTheDocument();

    await userEvent.click(within(dialog).getByRole("button", { name: "Choose the file…" }));
    await settle();
    const problems = within(dialog).getByTestId("import-problems");
    expect(problems).toHaveTextContent("Row 5 · Unit: The unit “sack” is not known — use a code from the Help sheet.");
    expect(problems).toHaveTextContent("Row 7 · Cost per unit: Give the cost per unit for the quantity on hand.");
    expect(within(dialog).getByRole("button", { name: "Import 1 products" })).toBeDisabled();
    expect(importApply).not.toHaveBeenCalled();
  });

  it("imports a clean workbook through the owner's PIN and says what was loaded", async () => {
    const importApply = vi
      .fn(async () => ({ created: 2, withStock: 1 }))
      .mockImplementationOnce(async () => {
        throw new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
      });
    const products = vi.fn(async () => []);
    renderWithProviders(<ProductsScreen />, { client: fakeClient({ catalog: { importApply, products } }), locale: "ar" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "استيراد من إكسل…" }));
    const dialog = await screen.findByRole("dialog", { name: "استيراد المنتجات من إكسل" });
    await userEvent.click(within(dialog).getByRole("button", { name: "اختر الملف…" }));
    await settle();
    expect(within(dialog).getByRole("status")).toHaveReadableText("جاهز: 2 منتجًا، منها 1 بمخزون افتتاحي.");
    expect(within(dialog).getAllByRole("row")).toHaveLength(3);
    await userEvent.click(within(dialog).getByRole("button", { name: /استورد/ }));
    await userEvent.type(await screen.findByLabelText("رمز المالك"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "تأكيد" }));
    await settle();
    expect(importApply).toHaveBeenLastCalledWith("/Users/shop/Documents/products.xlsx", "abc");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByText((_, el) => el?.tagName === "P" && (el.textContent ?? "").replace(/[⁦-⁩]/g, "") === "استُورد 2 منتجًا، منها 1 بمخزون افتتاحي.")).toBeInTheDocument();
    await waitFor(() => expect(products.mock.calls.length).toBeGreaterThan(1));
  });

  it("a closed file dialog shows no preview", async () => {
    const importPreview = vi.fn(async (): Promise<ImportPreview> => ({ ...withProblems, cancelled: true }));
    renderWithProviders(<ProductsScreen />, { client: fakeClient({ catalog: { importPreview } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Import from Excel…" }));
    await userEvent.click(await screen.findByRole("button", { name: "Choose the file…" }));
    await settle();
    expect(screen.queryByTestId("import-preview")).not.toBeInTheDocument();
  });
});
