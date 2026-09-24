import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aMovement, aProduct, fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import type { AdjustInput, CountInput, ReceiveInput } from "@/api/client";
import { STOCK_SEARCH_DEBOUNCE_MS, StockScreen } from "./StockScreen";

const required = () => new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
const elevated = { setUp: true, lockedSeconds: 0, elevatedSeconds: 120 };

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, STOCK_SEARCH_DEBOUNCE_MS + 50));
  });
}

const oilRow = () => screen.getByRole("row", { name: /Olive oil/ });

async function openDialog(button: string, title: string) {
  await userEvent.click(within(oilRow()).getByRole("button", { name: button }));
  return screen.findByRole("dialog", { name: title });
}

async function enterPin() {
  await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
  await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
  await settle();
}

describe("StockScreen — quantities", () => {
  it("leaves out open-priced items, which are never counted (2026-09-23)", async () => {
    const misc = aProduct({ id: "misc", nameAr: "متفرقات", nameEn: "Miscellaneous", unitCode: "piece", openPrice: true });
    renderWithProviders(<StockScreen />, { client: fakeClient({ catalog: { products: async () => [aProduct(), misc] } }), locale: "en" });
    await settle();
    expect(oilRow()).toBeInTheDocument();
    expect(screen.queryByRole("row", { name: /Miscellaneous/ })).not.toBeInTheDocument();
  });

  it("shows what is on hand, formatted by Go, with no cost column outside owner mode", async () => {
    renderWithProviders(<StockScreen />, { locale: "en" });
    await settle();
    expect(within(oilRow()).getByText("12.500")).toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Average cost (USD)" })).not.toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: "Value (USD)" })).not.toBeInTheDocument();
    expect(screen.getByText("Costs and values are shown in owner mode only.")).toBeInTheDocument();
  });

  it("a product that never moved shows zero, and its first receipt offers opening stock", async () => {
    const opening = vi.fn(async (input: ReceiveInput) => ({ productId: input.productId, onHand: "40.000" }));
    const receive = vi.fn(async (input: ReceiveInput) => ({ productId: input.productId, onHand: "40.000" }));
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { levels: async () => [], opening, receive } }), locale: "en" });
    await settle();
    expect(within(oilRow()).getByText("0")).toBeInTheDocument();

    const dialog = await openDialog("Receive", "Receive stock — Olive oil");
    expect(within(dialog).getByLabelText("What is this?")).toHaveValue("opening");
    await userEvent.type(within(dialog).getByLabelText("Quantity (Litre)"), "40");
    await userEvent.type(within(dialog).getByLabelText("Total cost"), "48");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await settle();

    expect(opening).toHaveBeenCalledWith({ productId: aProduct().id, quantity: "40", costMode: "total", cost: "48", currency: "USD", rate: "", note: "", discountPercent: "" });
    expect(receive).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});

describe("StockScreen — receiving", () => {
  it("receives by invoice total by default, and by unit cost when chosen", async () => {
    const receive = vi.fn(async (input: ReceiveInput) => ({ productId: input.productId, onHand: "20.000" }));
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { receive } }), locale: "en" });
    await settle();

    let dialog = await openDialog("Receive", "Receive stock — Olive oil");
    expect(within(dialog).queryByLabelText("What is this?")).not.toBeInTheDocument(); // it has moved: a delivery only
    expect(within(dialog).getByLabelText("Cost entered as")).toHaveValue("total");
    await userEvent.type(within(dialog).getByLabelText("Quantity (Litre)"), "٢٥");
    await userEvent.type(within(dialog).getByLabelText("Total cost"), "63.10");
    await userEvent.type(within(dialog).getByLabelText("Note (optional)"), "Abu Khalil");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await settle();
    expect(receive).toHaveBeenLastCalledWith({ productId: aProduct().id, quantity: "٢٥", costMode: "total", cost: "63.10", currency: "USD", rate: "", note: "Abu Khalil", discountPercent: "" });

    dialog = await openDialog("Receive", "Receive stock — Olive oil");
    await userEvent.selectOptions(within(dialog).getByLabelText("Cost entered as"), "unit");
    await userEvent.type(within(dialog).getByLabelText("Quantity (Litre)"), "5");
    await userEvent.type(within(dialog).getByLabelText("Cost per unit"), "1.262");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await settle();
    expect(receive).toHaveBeenLastCalledWith(expect.objectContaining({ costMode: "unit", cost: "1.262", quantity: "5" }));

    // A supplier's discount travels as typed; Go costs the delivery at what was really paid (0.10.0).
    dialog = await openDialog("Receive", "Receive stock — Olive oil");
    await userEvent.type(within(dialog).getByLabelText("Quantity (Litre)"), "10");
    await userEvent.type(within(dialog).getByLabelText("Total cost"), "20");
    await userEvent.type(within(dialog).getByLabelText("Supplier's discount, %"), "5");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await settle();
    expect(receive).toHaveBeenLastCalledWith(expect.objectContaining({ cost: "20", discountPercent: "5" }));
  });

  it("a delivery in pounds asks for the rate it was paid at, and cannot be saved without it", async () => {
    const receive = vi.fn(async (input: ReceiveInput) => ({ productId: input.productId, onHand: "37.500" }));
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { receive } }), locale: "en" });
    await settle();
    const dialog = await openDialog("Receive", "Receive stock — Olive oil");
    expect(within(dialog).queryByLabelText("Exchange rate paid (pounds per dollar)")).not.toBeInTheDocument();

    await userEvent.selectOptions(within(dialog).getByLabelText("Currency"), "SYP");
    await userEvent.type(within(dialog).getByLabelText("Quantity (Litre)"), "25");
    await userEvent.type(within(dialog).getByLabelText("Total cost"), "450000");
    // Pre-filled from the rate in force (D-L3.12), and editable: the rate the shop paid at is the one kept.
    const rateField = within(dialog).getByLabelText("Exchange rate paid (pounds per dollar)");
    expect(rateField).toHaveValue("15000");
    expect(within(dialog).getByText("Rate in force: 15,000")).toBeInTheDocument();
    await userEvent.clear(rateField);
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeDisabled();

    await userEvent.type(rateField, "15000");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await settle();
    expect(receive).toHaveBeenCalledWith({ productId: aProduct().id, quantity: "25", costMode: "total", cost: "450000", currency: "SYP", rate: "15000", note: "", discountPercent: "" });
  });

  it("a quantity with more decimals than its unit takes is named as it is typed", async () => {
    const receive = vi.fn();
    const units = async () => [{ code: "l", kind: "volume", inputDecimals: 3 }];
    renderWithProviders(<StockScreen />, { client: fakeClient({ catalog: { units }, stock: { receive } }), locale: "en" });
    await settle();
    const dialog = await openDialog("Receive", "Receive stock — Olive oil");
    await userEvent.type(within(dialog).getByLabelText("Quantity (Litre)"), "1.2345");
    await userEvent.type(within(dialog).getByLabelText("Total cost"), "5");
    expect(within(dialog).getByText("This unit takes at most 3 decimal places.")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeDisabled();
    expect(receive).not.toHaveBeenCalled();
  });

  it("Go's refusal is shown under the field it names", async () => {
    const receive = vi.fn(async () => {
      throw new BindingError({
        code: "lite.stock.cost_decimals",
        messageKey: "lite.stock.cost_decimals",
        params: { currency: "USD", decimals: "2" },
        fields: [{ field: "cost", code: "lite.stock.cost_decimals", messageKey: "lite.stock.cost_decimals" }],
      });
    });
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { receive } }), locale: "en" });
    await settle();
    const dialog = await openDialog("Receive", "Receive stock — Olive oil");
    await userEvent.type(within(dialog).getByLabelText("Quantity (Litre)"), "1");
    await userEvent.type(within(dialog).getByLabelText("Total cost"), "1.005");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await within(dialog).findByText("A USD cost takes at most 2 decimal places.")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Total cost")).toHaveAttribute("aria-invalid", "true");
  });
});

describe("StockScreen — counts and write-offs", () => {
  it("a count asks what is on the shelf, and a lowering count goes through the owner PIN", async () => {
    let calls = 0;
    const count = vi.fn(async (input: CountInput) => {
      calls += 1;
      if (calls === 1) throw required();
      return { productId: input.productId, onHand: "10.000" };
    });
    const elevate = vi.fn(async () => elevated);
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { count }, owner: { elevate } }), locale: "en" });
    await settle();

    const dialog = await openDialog("Count", "Count stock — Olive oil");
    expect(within(dialog).getByText("Recorded on hand: 12.500 Litre")).toBeInTheDocument();
    await userEvent.type(within(dialog).getByLabelText("Quantity on the shelf (Litre)"), "10");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await enterPin();

    expect(elevate).toHaveBeenCalledTimes(1);
    expect(count).toHaveBeenCalledTimes(2);
    expect(count).toHaveBeenLastCalledWith({ productId: aProduct().id, counted: "10", note: "" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("a count that raises stock asks for nothing", async () => {
    const count = vi.fn(async (input: CountInput) => ({ productId: input.productId, onHand: input.counted }));
    const elevate = vi.fn();
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { count }, owner: { elevate } }), locale: "en" });
    await settle();
    const dialog = await openDialog("Count", "Count stock — Olive oil");
    await userEvent.type(within(dialog).getByLabelText("Quantity on the shelf (Litre)"), "14");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await settle();
    expect(count).toHaveBeenCalledTimes(1);
    expect(elevate).not.toHaveBeenCalled();
  });

  it("a write-off takes a reason, gift included, and 'other' needs a note", async () => {
    const adjust = vi.fn(async (input: AdjustInput) => ({ productId: input.productId, onHand: "11.500" }));
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { adjust } }), locale: "en" });
    await settle();

    let dialog = await openDialog("Adjust", "Adjust stock — Olive oil");
    const reasons = within(within(dialog).getByLabelText("Reason")).getAllByRole("option").map((o) => o.textContent);
    expect(reasons).toEqual(["Damaged", "Expired or spoiled", "Own use", "Gift or sample", "Other"]);
    await userEvent.type(within(dialog).getByLabelText("Quantity (Litre)"), "1");
    await userEvent.selectOptions(within(dialog).getByLabelText("Reason"), "gift");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await settle();
    expect(adjust).toHaveBeenLastCalledWith({ productId: aProduct().id, direction: "out", quantity: "1", reason: "gift", note: "" });

    dialog = await openDialog("Adjust", "Adjust stock — Olive oil");
    await userEvent.type(within(dialog).getByLabelText("Quantity (Litre)"), "1");
    await userEvent.selectOptions(within(dialog).getByLabelText("Reason"), "other");
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText("Note"), "fell off the shelf");
    await userEvent.selectOptions(within(dialog).getByLabelText("Direction"), "in");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await settle();
    expect(adjust).toHaveBeenLastCalledWith({ productId: aProduct().id, direction: "in", quantity: "1", reason: "other", note: "fell off the shelf" });
  });

  it("cancelling the PIN leaves the write-off dialog open and says nothing", async () => {
    const adjust = vi.fn(async () => {
      throw required();
    });
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { adjust } }), locale: "en" });
    await settle();
    const dialog = await openDialog("Adjust", "Adjust stock — Olive oil");
    await userEvent.type(within(dialog).getByLabelText("Quantity (Litre)"), "1");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await userEvent.click(within(await screen.findByRole("dialog", { name: "Owner PIN required" })).getByRole("button", { name: "Cancel" }));
    await settle();
    expect(screen.getByRole("dialog", { name: "Adjust stock — Olive oil" })).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

describe("StockScreen — costs are the owner's", () => {
  it("show costs asks for the PIN, then shows cost, value, total and any check findings", async () => {
    let calls = 0;
    const valuation = vi.fn(async () => {
      calls += 1;
      if (calls === 1) throw required();
      return {
        lines: [{ productId: aProduct().id, onHand: "12.500", averageCost: "3.20", value: "40.00", valueLocal: "600000" }],
        total: "1140.00",
        totalLocal: "17100000",
        localCurrency: "SYP",
        rate: "15000",
      };
    });
    const verify = vi.fn(async () => [{ code: "lite.stock.verify.level_mismatch", productId: aProduct().id, movementId: "" }]);
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { valuation, verify }, owner: { elevate: async () => elevated } }), locale: "en" });
    await settle();

    await userEvent.click(screen.getByRole("button", { name: "Show costs" }));
    await enterPin();

    expect(screen.getByRole("columnheader", { name: "Average cost (USD)" })).toBeInTheDocument();
    expect(within(oilRow()).getByText("3.20")).toBeInTheDocument();
    expect(within(oilRow()).getByText("40.00")).toBeInTheDocument();
    expect(screen.getByText("Total stock value: 1,140.00 USD")).toBeInTheDocument();
    const banner = screen.getByRole("alert");
    expect(banner).toHaveTextContent("The stock check found 1 problem(s).");
    expect(banner).toHaveTextContent("A quantity on hand does not match its last movement. — Olive oil");

    await userEvent.click(screen.getByRole("button", { name: "Hide costs" }));
    expect(screen.queryByRole("columnheader", { name: "Average cost (USD)" })).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("cancelling the PIN shows no costs and no error", async () => {
    const valuation = vi.fn(async () => {
      throw required();
    });
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { valuation } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Show costs" }));
    await userEvent.click(within(await screen.findByRole("dialog", { name: "Owner PIN required" })).getByRole("button", { name: "Cancel" }));
    await settle();
    expect(screen.queryByRole("columnheader", { name: "Value (USD)" })).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

describe("StockScreen — history", () => {
  it("offers a reversal only on the receipt Go names, through the PIN", async () => {
    const newest = aMovement({ id: "m3", kind: "receipt", quantity: "10.000", onHandAfter: "22.500" });
    const older = aMovement({ id: "m1", kind: "receipt", quantity: "12.500", onHandAfter: "12.500" });
    const movements = vi.fn(async () => ({ movements: [newest, aMovement({ id: "m2", kind: "count", quantity: "0.000", reason: "count" }), older], costsVisible: false, reversibleId: "m3" }));
    let calls = 0;
    const reverseReceipt = vi.fn(async () => {
      calls += 1;
      if (calls === 1) throw required();
      return { productId: aProduct().id, onHand: "12.500" };
    });
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { movements, reverseReceipt }, owner: { elevate: async () => elevated } }), locale: "en" });
    await settle();

    const dialog = await openDialog("History", "Stock movements — Olive oil");
    await settle();
    const rows = within(dialog).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(3);
    expect(within(rows[0]!).getByRole("button", { name: "Reverse this delivery" })).toBeInTheDocument();
    expect(within(rows[1]!).queryByRole("button")).not.toBeInTheDocument();
    expect(within(rows[2]!).queryByRole("button")).not.toBeInTheDocument();
    expect(within(rows[1]!).getByText("Count difference")).toBeInTheDocument();
    expect(within(dialog).queryByRole("columnheader", { name: "Unit cost (USD)" })).not.toBeInTheDocument();

    await userEvent.click(within(rows[0]!).getByRole("button", { name: "Reverse this delivery" }));
    await enterPin();
    expect(reverseReceipt).toHaveBeenCalledTimes(2);
    expect(reverseReceipt).toHaveBeenLastCalledWith({ movementId: "m3", note: "" });
    expect(movements.mock.calls.length).toBeGreaterThanOrEqual(2); // read again after the reversal
  });

  it("shows costs and what was typed on a pound receipt when Go sends them", async () => {
    const movements = async () => ({
      movements: [aMovement({ unitCost: "1.20", averageCostAfter: "1.20", enteredCurrency: "SYP", enteredUnitCost: "18000", rate: "15000" })],
      costsVisible: true,
      reversibleId: "",
    });
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { movements } }), locale: "en" });
    await settle();
    const dialog = await openDialog("History", "Stock movements — Olive oil");
    await settle();
    expect(within(dialog).getByRole("columnheader", { name: "Unit cost (USD)" })).toBeInTheDocument();
    expect(within(dialog).getByText("Entered: 18,000 Syrian pound at 15,000")).toBeInTheDocument();
  });

  it("a cost correction takes a figure and a note, through the PIN", async () => {
    const correctCost = vi.fn(async () => ({ productId: aProduct().id, onHand: "12.500" }));
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { correctCost } }), locale: "en" });
    await settle();
    const history = await openDialog("History", "Stock movements — Olive oil");
    await userEvent.click(within(history).getByRole("button", { name: "Correct average cost" }));
    const dialog = await screen.findByRole("dialog", { name: "Correct average cost — Olive oil" });
    await userEvent.type(within(dialog).getByLabelText("Correct average cost (USD per unit)"), "95");
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText("Note"), "typed 9.50");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await settle();
    expect(correctCost).toHaveBeenCalledWith({ productId: aProduct().id, averageCost: "95", note: "typed 9.50" });
    expect(await screen.findByRole("dialog", { name: "Stock movements — Olive oil" })).toBeInTheDocument();
  });
});

describe("StockScreen — opening a package", () => {
  it("is offered only on a linked product, and shows what is on hand afterwards", async () => {
    const tin = aProduct({ id: "tin", nameAr: "تنكة زيت", nameEn: "Olive oil tin", unitCode: "tin", packageContentId: "loose", packageContentQuantity: "16.000" });
    const loose = aProduct({ id: "loose", nameAr: "زيت فرط", nameEn: "Loose olive oil", unitCode: "l" });
    const products = async () => [tin, loose];
    const units = async () => [
      { code: "tin", kind: "count", inputDecimals: 0 },
      { code: "l", kind: "volume", inputDecimals: 3 },
    ];
    const openPackage = vi.fn(async () => [
      { productId: "tin", onHand: "2" },
      { productId: "loose", onHand: "16.000" },
    ]);
    renderWithProviders(<StockScreen />, { client: fakeClient({ catalog: { products, units }, stock: { openPackage } }), locale: "en" });
    await settle();

    expect(within(screen.getByRole("row", { name: /Loose olive oil/ })).queryByRole("button", { name: "Open" })).not.toBeInTheDocument();
    await userEvent.click(within(screen.getByRole("row", { name: /Olive oil tin/ })).getByRole("button", { name: "Open" }));
    const dialog = await screen.findByRole("dialog", { name: "Open packages — Olive oil tin" });
    expect(within(dialog).getByText("Each package opens into 16.000 Litre of “Loose olive oil”.")).toBeInTheDocument();

    await userEvent.clear(within(dialog).getByLabelText("Number of packages"));
    await userEvent.type(within(dialog).getByLabelText("Number of packages"), "1.5");
    expect(within(dialog).getByRole("button", { name: "Open" })).toBeDisabled();
    await userEvent.clear(within(dialog).getByLabelText("Number of packages"));
    await userEvent.type(within(dialog).getByLabelText("Number of packages"), "1");
    await userEvent.click(within(dialog).getByRole("button", { name: "Open" }));
    await settle();

    expect(openPackage).toHaveBeenCalledWith({ packageProductId: "tin", packages: "1" });
    expect(within(dialog).getByText("Opened. Now on hand: 2 Tin and 16.000 Litre.")).toBeInTheDocument();
  });
});

describe("StockScreen — value in pounds", () => {
  it("in owner mode shows each line and the total in pounds at the rate Go used", async () => {
    renderWithProviders(<StockScreen />, { client: fakeClient({ owner: { status: async () => elevated } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Show costs" }));
    await settle();
    expect(screen.getByRole("columnheader", { name: "Value (Syrian pound at today's rate)" })).toBeInTheDocument();
    expect(within(oilRow()).getByText("600,000")).toBeInTheDocument();
    expect(screen.getByText(/≈ 600,000 Syrian pound at 15,000/)).toBeInTheDocument();
  });

  it("with no rate shows no pound column at all", async () => {
    const valuation = async () => ({
      lines: [{ productId: aProduct().id, onHand: "12.500", averageCost: "3.20", value: "40.00", valueLocal: "" }],
      total: "40.00",
      totalLocal: "",
      localCurrency: "",
      rate: "",
    });
    renderWithProviders(<StockScreen />, { client: fakeClient({ stock: { valuation } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Show costs" }));
    await settle();
    expect(screen.getByRole("columnheader", { name: "Value (USD)" })).toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: /today's rate/ })).not.toBeInTheDocument();
  });
});
