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
    { currency: "SYP", sales: 1, charged: "73000", cashIn: "73000", changeOut: "0", voids: 1, refunded: "15000" },
    { currency: "USD", sales: 1, charged: "10.50", cashIn: "20.00", changeOut: "9.50", voids: 0, refunded: "0.00" },
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
