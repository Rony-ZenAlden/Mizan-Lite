import { act, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aSale, fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import type { Day, VoidInput } from "@/api/client";
import { isZero } from "./Money";
import { SalesScreen } from "./SalesScreen";

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

const dollarSale = aSale({
  id: "usd",
  receiptNo: 8,
  settlement: "USD",
  total: "10.50",
  rounding: "0.00",
  cashNote: "",
  tenderCurrency: "USD",
  tendered: "20.00",
  changeCurrency: "USD",
  change: "9.50",
});

const aDay = (overrides: Partial<Day> = {}): Day => ({
  businessDate: "2026-09-14",
  sales: [aSale(), dollarSale],
  totals: [
    { currency: "SYP", sales: 1, charged: "73000", cashIn: "73000", changeOut: "0", voids: 1, voided: "15000", onCredit: "0" },
    { currency: "USD", sales: 1, charged: "10.50", cashIn: "20.00", changeOut: "9.50", voids: 0, voided: "0.00", onCredit: "0.00" },
  ],
  ...overrides,
});

describe("SalesScreen", () => {
  it("lists the day's sales with Go's totals per currency", async () => {
    renderWithProviders(<SalesScreen />, { client: fakeClient({ sales: { list: async () => aDay() } }), locale: "en" });
    await settle();
    expect(screen.getByLabelText("Day")).toHaveValue("2026-09-14");
    const pounds = screen.getByTestId("totals-SYP");
    expect(pounds).toHaveTextContent("Sales (1)73,000 SYP");
    expect(pounds).toHaveTextContent("Cash received73,000 SYP");
    expect(pounds).toHaveTextContent("Voids (1)15,000 SYP");
    const dollars = screen.getByTestId("totals-USD");
    expect(dollars).toHaveTextContent("Change given9.50 USD");

    const rows = screen.getAllByRole("row").slice(1);
    expect(rows).toHaveLength(2);
    expect(within(rows[1]!).getByText("10.50 USD")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("9.50 USD")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("Sold")).toBeInTheDocument();
  });

  it("an earlier day is asked for by its date", async () => {
    const list = vi.fn(async (date: string) => aDay({ businessDate: date || "2026-09-14", sales: [] }));
    renderWithProviders(<SalesScreen />, { client: fakeClient({ sales: { list } }), locale: "en" });
    await settle();
    expect(list).toHaveBeenLastCalledWith("");
    // A date picker delivers the whole date at once; typing it key by key into type="date" is not something a person can do.
    fireEvent.change(screen.getByLabelText("Day"), { target: { value: "2026-09-10" } });
    await settle();
    expect(list).toHaveBeenLastCalledWith("2026-09-10");
    expect(screen.getByText("No sales on this day.")).toBeInTheDocument();
  });

  it("opens a receipt as Go recorded it, 80 mm wide, with its rounding, tender and change", async () => {
    const receipt = vi.fn(async () => aSale({ tenderCurrency: "USD", tendered: "5.00", change: "2000" }));
    renderWithProviders(<SalesScreen />, { client: fakeClient({ sales: { list: async () => aDay(), receipt } }), locale: "en" });
    await settle();
    await userEvent.click(within(screen.getAllByRole("row")[1]!).getByRole("button", { name: "Receipt" }));
    const dialog = await screen.findByRole("dialog", { name: "Receipt No. 7" });
    expect(receipt).toHaveBeenCalledWith(aSale().id);
    const paper = within(dialog).getByTestId("receipt");
    expect(paper).toHaveClass("max-w-[80mm]");
    expect(paper).toHaveTextContent("بقالية المونة");
    expect(paper).toHaveTextContent("Olive oil");
    expect(paper).toHaveTextContent("1.500 Litre × 3.25 USD");
    expect(paper).toHaveTextContent("Rounding to 500-125 SYP");
    expect(paper).toHaveTextContent("Total73,000 SYP");
    expect(paper).toHaveTextContent("Paid5.00 USD");
    expect(paper).toHaveTextContent("Change2,000 SYP");
    expect(paper).toHaveTextContent("Rate: 1 USD = 15,000 SYP");
    expect(paper).not.toHaveTextContent("Discount");
  });

  it("voids a whole sale with the owner's PIN and a reason, and reloads the day", async () => {
    const voidSale = vi
      .fn(async (input: VoidInput) => aSale({ status: "voided", voidReason: input.reason, voidedAt: "2026-09-14T09:00:00.000Z" }))
      .mockImplementationOnce(async () => {
        throw required();
      });
    const list = vi.fn(async () => aDay());
    renderWithProviders(<SalesScreen />, {
      client: fakeClient({ sales: { list, void: voidSale }, owner: { elevate: async () => elevated } }),
      locale: "en",
    });
    await settle();
    await userEvent.click(within(screen.getAllByRole("row")[1]!).getByRole("button", { name: "Receipt" }));
    const dialog = await screen.findByRole("dialog", { name: "Receipt No. 7" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Void sale" }));
    expect(within(dialog).getByTestId("void-hand-back")).toHaveTextContent("Hand back 73,000 SYP");
    expect(within(dialog).getByRole("button", { name: "Void the whole sale" })).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText("Reason for the void"), "wrong quantity");
    await userEvent.click(within(dialog).getByRole("button", { name: "Void the whole sale" }));
    await enterPin();

    expect(voidSale).toHaveBeenCalledTimes(2);
    expect(voidSale).toHaveBeenLastCalledWith({ saleId: aSale().id, reason: "wrong quantity" });
    const again = screen.getByRole("dialog", { name: "Receipt No. 7" });
    expect(within(again).getByTestId("receipt")).toHaveTextContent("VOIDED — wrong quantity");
    expect(within(again).queryByRole("button", { name: "Void sale" })).not.toBeInTheDocument();
    expect(list).toHaveBeenCalledTimes(2);
  });

  it("a void Go refuses shows why", async () => {
    const voidSale = vi.fn(async () => {
      throw new BindingError({ code: "lite.sales.already_voided", messageKey: "lite.sales.already_voided", params: { receiptNo: "7" } });
    });
    renderWithProviders(<SalesScreen />, { client: fakeClient({ sales: { list: async () => aDay(), void: voidSale } }), locale: "en" });
    await settle();
    await userEvent.click(within(screen.getAllByRole("row")[1]!).getByRole("button", { name: "Receipt" }));
    const dialog = await screen.findByRole("dialog", { name: "Receipt No. 7" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Void sale" }));
    await userEvent.type(within(dialog).getByLabelText("Reason for the void"), "x");
    await userEvent.click(within(dialog).getByRole("button", { name: "Void the whole sale" }));
    expect(await within(dialog).findByText("Receipt 7 is already voided.")).toBeInTheDocument();
  });

  it("runs the sales check only in owner mode and names the receipt a finding concerns", async () => {
    const verify = vi.fn(async () => [{ code: "lite.sales.verify.stock_missing", saleId: "s", receiptNo: 7 }]);
    const { unmount } = renderWithProviders(<SalesScreen />, { client: fakeClient({ sales: { verify } }), locale: "en" });
    await settle();
    expect(verify).not.toHaveBeenCalled();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    unmount();

    renderWithProviders(<SalesScreen />, { client: fakeClient({ sales: { verify }, owner: { status: async () => elevated } }), locale: "en" });
    await settle();
    expect(verify).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("alert")).toHaveTextContent("The sales check found 1 problem(s)");
    expect(screen.getByRole("alert")).toHaveTextContent("A sale or void has no matching stock movement. (receipt 7)");
  });

  it("reads in Arabic", async () => {
    renderWithProviders(<SalesScreen />, { client: fakeClient({ sales: { list: async () => aDay() } }), locale: "ar" });
    await settle();
    expect(screen.getByRole("heading", { name: "المبيعات" })).toBeInTheDocument();
    expect(screen.getByTestId("totals-SYP")).toHaveTextContent("73,000 ل.س");
  });
});

describe("isZero", () => {
  it("is a comparison of Go's text", () => {
    expect(["0", "0.00", "-0", "-0.00"].every(isZero)).toBe(true);
    expect(["-125", "0.01", "10", "500"].some(isZero)).toBe(false);
  });
});

describe("ReceiptView — on credit", () => {
  it("shows who the sale is on, what it added to the debt and the balance after, and that a void reversed it", async () => {
    const credit = aSale({ payment: "credit", tendered: "20000", creditCustomerId: "c1", creditCustomerName: "أبو محمد", creditCurrency: "SYP", creditAmount: "53000", creditBalanceAfter: "103000", status: "voided", voidReason: "أعاده", creditReversed: true });
    renderWithProviders(<SalesScreen />, { client: fakeClient({ sales: { list: async () => aDay({ sales: [credit] }), receipt: async () => credit } }), locale: "en" });
    await settle();
    const row = screen.getAllByRole("row")[1]!;
    expect(row).toHaveTextContent("On credit — أبو محمد");
    await userEvent.click(within(row).getByRole("button", { name: "Receipt" }));
    const dialog = await screen.findByRole("dialog", { name: "Receipt No. 7" });
    const block = within(dialog).getByTestId("receipt-credit");
    expect(block).toHaveTextContent("On credit — أبو محمد");
    expect(block).toHaveTextContent("Added to the debt53,000 SYP");
    expect(block).toHaveTextContent("Balance now103,000 SYP");
    expect(block).toHaveTextContent("The debt was reversed with the void.");
    expect(within(dialog).getByTestId("receipt")).toHaveTextContent("Paid now20,000 SYP");
    expect(within(dialog).getByTestId("receipt")).not.toHaveTextContent("Change");
  });
});

describe("ReceiptView — what a void hands back", () => {
  it("says the cash to hand back before the PIN: a credit sale's paid now, in the currency it was charged in (Q-L6.5)", async () => {
    // 76,000 pounds on credit, $2.00 handed over and worth 30,000: the void hands back 30,000 pounds, not the $2 note.
    const credit = aSale({ payment: "credit", total: "76000", tenderCurrency: "USD", tendered: "2.00", creditCustomerId: "c1", creditCustomerName: "أبو محمد",
      creditCurrency: "SYP", creditAmount: "46000", creditBalanceAfter: "46000", voidReturn: "30000", voidReturnCurrency: "SYP" });
    renderWithProviders(<SalesScreen />, { client: fakeClient({ sales: { list: async () => aDay({ sales: [credit] }), receipt: async () => credit } }), locale: "en" });
    await settle();
    await userEvent.click(within(screen.getAllByRole("row")[1]!).getByRole("button", { name: "Receipt" }));
    const dialog = await screen.findByRole("dialog", { name: "Receipt No. 7" });
    expect(within(dialog).queryByTestId("void-hand-back")).not.toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Void sale" }));
    expect(within(dialog).getByTestId("void-hand-back")).toHaveTextContent("Hand back 30,000 SYP");
    expect(within(dialog).getByTestId("void-hand-back")).not.toHaveTextContent("USD");
  });
});

describe("SalesScreen — L7", () => {
  it("exports the sales history over the range typed", async () => {
    const salesHistory = vi.fn(async () => ({ path: "/Users/shop/sales.xlsx", bytes: 1, cancelled: false }));
    renderWithProviders(<SalesScreen />, { client: fakeClient({ sales: { list: async () => aDay() }, exports: { salesHistory } }), locale: "en" });
    await settle();
    const section = screen.getByRole("region", { name: "Export the sales history" });
    fireEvent.change(within(section).getByLabelText("From"), { target: { value: "2026-09-01" } });
    await userEvent.click(within(section).getByRole("button", { name: "Excel" }));
    await settle();
    expect(salesHistory).toHaveBeenCalledWith("2026-09-01", "", "xlsx");
    expect(within(section).getByRole("status")).toHaveTextContent("/Users/shop/sales.xlsx");
  });

  it("a receipt shows the bitmap Go prints on its own tab, and prints from there", async () => {
    const preview = vi.fn(async () => ({ png: "UE5H", width: 576, height: 900, copyNo: 2 }));
    const sale = vi.fn(async () => ({ printer: "Xprinter XP-80", copyNo: 2, path: "raw" }));
    renderWithProviders(<SalesScreen />, { client: fakeClient({ sales: { list: async () => aDay() }, print: { preview, sale } }), locale: "en" });
    await settle();
    await userEvent.click(within(screen.getAllByRole("row")[1]!).getByRole("button", { name: "Receipt" }));
    const dialog = await screen.findByRole("dialog", { name: "Receipt No. 7" });
    expect(preview).not.toHaveBeenCalled(); // an old sale opened to read: nothing printed, nothing asked of the printer
    expect(sale).not.toHaveBeenCalled();

    await userEvent.click(within(dialog).getByRole("tab", { name: "As printed" }));
    await settle();
    expect(within(dialog).getByRole("img", { name: "The receipt as it will print" })).toHaveAttribute("src", "data:image/png;base64,UE5H");
    expect(within(dialog).getByTestId("receipt")).not.toBeVisible();
    await userEvent.click(within(dialog).getByRole("button", { name: "Print a copy" }));
    await settle();
    expect(sale).toHaveBeenCalledWith(aSale().id);
    expect(within(dialog).getByText("Copy 2 sent to Xprinter XP-80")).toBeInTheDocument();
  });
});
