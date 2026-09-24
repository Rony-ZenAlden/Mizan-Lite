import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aProduct, aPurchase, aPurchaseLine, aSupplier, aSupplierEntry, aSupplierList, fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import type { PurchaseInput, SupplierMoneyInput } from "@/api/client";
import { PURCHASE_QUOTE_DEBOUNCE_MS } from "./PurchaseDialog";
import { SUPPLIER_SEARCH_DEBOUNCE_MS, SuppliersScreen } from "./SuppliersScreen";

const required = () => new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });

async function settle(ms = Math.max(SUPPLIER_SEARCH_DEBOUNCE_MS, PURCHASE_QUOTE_DEBOUNCE_MS) + 60) {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, ms));
  });
}

const oil = aProduct({ id: "p-oil", nameAr: "زيت زيتون", nameEn: "Olive oil", unitCode: "l", barcode: "6291041500213" });

describe("SuppliersScreen", () => {
  it("lists what the shop owes each supplier and all of them, per currency — and what a supplier owes the shop, as such", async () => {
    const list = vi.fn(async () =>
      aSupplierList({
        suppliers: [
          aSupplier(),
          aSupplier({ id: "sup-2", name: "بيت المونة", city: "دمشق", balances: [{ currency: "SYP", balance: "-50000" }] }),
        ],
        totals: [
          { currency: "SYP", balance: "-50000" },
          { currency: "USD", balance: "22.00" },
        ],
      }),
    );
    renderWithProviders(<SuppliersScreen />, { client: fakeClient({ suppliers: { list } }), locale: "en" });
    await settle();
    const rows = screen.getAllByTestId("supplier-row");
    expect(within(rows[0]!).getByTestId("supplier-balance-USD")).toHaveTextContent("The shop owes 22.00 USD");
    expect(within(rows[1]!).getByTestId("supplier-balance-SYP")).toHaveTextContent("Owes the shop 50,000 SYP");
    expect(within(rows[1]!).getByTestId("supplier-balance-SYP")).not.toHaveTextContent("-50,000");
    expect(screen.getByTestId("supplier-total-USD")).toHaveTextContent("The shop owes its suppliers22.00 USD");
    expect(screen.getByTestId("supplier-total-SYP")).toHaveTextContent("Suppliers owe the shop50,000 SYP");

    await userEvent.type(screen.getByLabelText("Search by name or phone"), "المونة");
    await settle();
    expect(list).toHaveBeenLastCalledWith({ text: "المونة", includeInactive: false });
  });

  it("with the PIN switched on, asks for the PIN — and a cancelled PIN shows nothing", async () => {
    const list = vi.fn(async () => {
      throw required();
    });
    renderWithProviders(<SuppliersScreen />, { client: fakeClient({ suppliers: { list } }), locale: "en" });
    await settle();
    await userEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    await settle();
    expect(screen.getByText("The suppliers' accounts show what the shop's goods cost. Enter the owner's PIN to see them.")).toBeInTheDocument();
    expect(screen.queryByTestId("supplier-row")).not.toBeInTheDocument();
  });

  it("pays a supplier from the drawer or the owner's own money, chosen each time", async () => {
    const pay = vi.fn(async (input: SupplierMoneyInput) => aSupplierEntry({ kind: "payment", amount: `-${input.amount}`, source: input.source }));
    renderWithProviders(<SuppliersScreen />, { client: fakeClient({ suppliers: { pay } }), locale: "en" });
    await settle();
    await userEvent.click(within(screen.getAllByTestId("supplier-row")[0]!).getByRole("button", { name: "Account" }));
    const account = await screen.findByRole("dialog", { name: "المروى — account" });
    await settle();
    expect(within(account).getByTestId("supplier-balance")).toHaveTextContent("The shop owes 22.00 USD");
    const entries = within(account).getAllByTestId("supplier-entry");
    expect(entries[0]).toHaveTextContent("Payment");
    expect(entries[0]).toHaveTextContent("The drawer");
    expect(entries[1]).toHaveTextContent("Purchase 7 · their invoice 2970");

    await userEvent.click(within(account).getByRole("button", { name: "Pay the supplier" }));
    const dialog = await screen.findByRole("dialog", { name: "Pay المروى" });
    await userEvent.type(within(dialog).getByLabelText("Amount in US dollar"), "22");
    await userEvent.selectOptions(within(dialog).getByLabelText("Paid from"), "owner");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await settle();
    expect(pay).toHaveBeenCalledWith({ supplierId: "sup-1", currency: "USD", amount: "22", source: "owner", inShopsFavour: false, note: "" });
  });

  it("records a purchase: items found by name or barcode, damage and discounts typed, every figure Go's", async () => {
    const quotePurchase = vi.fn(async (input: PurchaseInput) => ({
      purchase: aPurchase({ id: "", purchaseNo: 0, status: "", lines: input.lines.map((_, i) => aPurchaseLine({ lineNo: i + 1 })) }),
      balanceBefore: "22.00",
      balanceAfter: "31.00",
    }));
    const recordPurchase = vi.fn(async () => aPurchase());
    renderWithProviders(<SuppliersScreen />, {
      client: fakeClient({ suppliers: { quotePurchase, recordPurchase }, catalog: { products: async () => [oil, aProduct({ id: "p-misc", openPrice: true, nameAr: "متفرقات" })] } }),
      locale: "en",
    });
    await settle();
    await userEvent.click(within(screen.getAllByTestId("supplier-row")[0]!).getByRole("button", { name: "Purchase" }));
    const dialog = await screen.findByRole("dialog", { name: "New purchase" });
    await settle();

    // A barcode and Enter add the item; the open-priced item is never offered.
    await userEvent.type(within(dialog).getByLabelText("Add an item — type its name or scan its barcode"), "6291041500213{Enter}");
    expect(within(dialog).getAllByTestId("purchase-line-input")).toHaveLength(1);
    await userEvent.type(within(dialog).getByLabelText("Add an item — type its name or scan its barcode"), "متفرقات");
    expect(within(dialog).getByText("No item matches.")).toBeInTheDocument();
    await userEvent.clear(within(dialog).getByLabelText("Add an item — type its name or scan its barcode"));

    await userEvent.type(within(dialog).getByLabelText("How many Olive oil arrived"), "10");
    await userEvent.type(within(dialog).getByLabelText("How many Olive oil arrived damaged"), "2");
    await userEvent.type(within(dialog).getByLabelText("Price of one Olive oil"), "2");
    await userEvent.type(within(dialog).getByLabelText("Discount on Olive oil"), "10");
    await userEvent.type(within(dialog).getByLabelText("Discount on the whole invoice, in US dollar"), "0.40");
    await userEvent.type(within(dialog).getByLabelText("Paid now, in US dollar"), "5");
    await settle();

    expect(within(dialog).getByTestId("line-due")).toHaveTextContent("14.00 USD");
    expect(within(dialog).getByTestId("line-net-cost")).toHaveTextContent("1.75 USD");
    expect(within(dialog).getByTestId("balance-after")).toHaveTextContent("The shop owes 31.00 USD");
    expect(within(dialog).getByTestId("purchase-totals")).toHaveTextContent("Total due14.00 USD");

    await userEvent.click(within(dialog).getByRole("button", { name: "Record the purchase" }));
    await settle();
    expect(recordPurchase).toHaveBeenCalledWith({
      supplierId: "sup-1",
      currency: "USD",
      rate: "",
      supplierRef: "",
      lines: [{ productId: "p-oil", quantity: "10", damaged: "2", unitCost: "2", discountPercent: "10", discountAmount: "" }],
      invoiceDiscount: "0.40",
      paidNow: "5",
      paidFrom: "drawer",
      note: "",
    });
    // The recorded purchase is shown back: the damaged units, and what each good one cost.
    const view = await screen.findByRole("dialog", { name: "Purchase 7 — المروى" });
    expect(within(view).getByTestId("purchase-line")).toHaveTextContent("1.75 USD");
  });

  it("a barcode scanned before the products have loaded is added once they arrive (found by J13)", async () => {
    let arrive: (products: ReturnType<typeof aProduct>[]) => void = () => {};
    const products = vi.fn(() => new Promise<ReturnType<typeof aProduct>[]>((resolve) => (arrive = resolve)));
    renderWithProviders(<SuppliersScreen />, { client: fakeClient({ catalog: { products } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "New purchase" }));
    const dialog = await screen.findByRole("dialog", { name: "New purchase" });
    await userEvent.type(within(dialog).getByLabelText("Add an item — type its name or scan its barcode"), "6291041500213{Enter}");
    expect(within(dialog).queryAllByTestId("purchase-line-input")).toHaveLength(0);
    await act(async () => arrive([oil]));
    expect(within(dialog).getAllByTestId("purchase-line-input")).toHaveLength(1);
    expect(within(dialog).getByLabelText("Add an item — type its name or scan its barcode")).toHaveValue("");
  });

  it("a refusal from Go lands under the line it names", async () => {
    const quotePurchase = vi.fn(async () => {
      throw new BindingError({
        code: "lite.suppliers.damaged_too_many",
        messageKey: "lite.suppliers.damaged_too_many",
        params: { line: "1" },
        fields: [{ field: "lines.1.damaged", code: "lite.suppliers.damaged_too_many", messageKey: "lite.suppliers.damaged_too_many" }],
      });
    });
    renderWithProviders(<SuppliersScreen />, { client: fakeClient({ suppliers: { quotePurchase }, catalog: { products: async () => [oil] } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "New purchase" }));
    const dialog = await screen.findByRole("dialog", { name: "New purchase" });
    await settle();
    await userEvent.selectOptions(within(dialog).getByLabelText("Supplier"), "sup-1");
    await userEvent.type(within(dialog).getByLabelText("Add an item — type its name or scan its barcode"), "olive{Enter}");
    await userEvent.type(within(dialog).getByLabelText("How many Olive oil arrived"), "1");
    await userEvent.type(within(dialog).getByLabelText("How many Olive oil arrived damaged"), "3");
    await userEvent.type(within(dialog).getByLabelText("Price of one Olive oil"), "2");
    await settle();
    expect(within(dialog).getByText("Line 1: more units are marked damaged than arrived.")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Record the purchase" })).toBeEnabled();
  });

  it("lists purchases and voids one with its reason", async () => {
    const voidPurchase = vi.fn(async (purchaseId: string, reason: string) => aPurchase({ id: purchaseId, status: "voided", voidReason: reason }));
    renderWithProviders(<SuppliersScreen />, { client: fakeClient({ suppliers: { voidPurchase } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("tab", { name: "Purchases" }));
    const row = screen.getByTestId("purchase-row");
    expect(row).toHaveTextContent("المروى");
    expect(row).toHaveTextContent("1 with damage");
    await userEvent.click(within(row).getByRole("button", { name: "View" }));
    const view = await screen.findByRole("dialog", { name: "Purchase 7 — المروى" });
    await userEvent.click(within(view).getByRole("button", { name: "Void the purchase" }));
    const confirm = await screen.findByRole("dialog", { name: "Void purchase 7" });
    await userEvent.type(within(confirm).getByLabelText("Reason"), "entered twice");
    await userEvent.click(within(confirm).getByRole("button", { name: "Void the purchase" }));
    await settle();
    expect(voidPurchase).toHaveBeenCalledWith("pur-7", "entered twice");
    expect(within(view).getByText("Voided: entered twice")).toBeInTheDocument();
  });
});
