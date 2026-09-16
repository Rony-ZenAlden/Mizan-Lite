import { act, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { BindingError } from "@/api/envelope";
import { aCashEntry, aDrawer, fakeClient, renderWithProviders } from "@/api/testing";
import type { CashCountInput, CashRecordInput, Drawer } from "@/api/client";
import { CashScreen } from "./CashScreen";

const required = () => new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
const elevated = { setUp: true, lockedSeconds: 0, elevatedSeconds: 120 };

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}

const counted = (base: Drawer): Drawer => ({
  ...base,
  currencies: base.currencies.map((c) =>
    c.currency === "SYP"
      ? { ...c, opening: "40000", openingCountDate: "2026-09-13", changeOut: "15000", voidReturns: "20000", withdrawalsOut: "5000", expected: "97500", counted: true, count: "97000", countExpected: "97500", difference: "-500", countedAt: "2026-09-14T17:00:00.000Z" }
      : c,
  ),
});

describe("CashScreen", () => {
  it("shows per currency what should be in the drawer and how it was made up, nothing converted", async () => {
    renderWithProviders(<CashScreen />, { client: fakeClient({ cash: { drawer: async () => counted(aDrawer()) } }), locale: "en" });
    await settle();
    const pounds = screen.getByTestId("drawer-SYP");
    expect(pounds).toHaveTextContent("Start of day, from the count of 13/09/202640,000 SYP");
    expect(pounds).toHaveTextContent("plus cash received for sales97,500 SYP");
    expect(pounds).toHaveTextContent("less change given15,000 SYP");
    expect(pounds).toHaveTextContent("less handed back on voids20,000 SYP");
    expect(pounds).toHaveTextContent("less taken out by the owner5,000 SYP");
    expect(pounds).not.toHaveTextContent("repayments");
    expect(screen.getByTestId("expected-SYP")).toHaveTextContent("97,500 SYP");
    expect(screen.getByTestId("difference-SYP")).toHaveTextContent("-500 SYP");
    const dollars = screen.getByTestId("drawer-USD");
    expect(dollars).toHaveTextContent("Start of day (the drawer was never counted before)0.00 USD");
    expect(dollars).toHaveTextContent("Not counted yet on this day.");
    expect(screen.getByText(/Expenses, withdrawals and deposits are listed in owner mode/)).toBeInTheDocument();
  });

  it("the counter counts without a PIN and sees the difference", async () => {
    const count = vi.fn(async (input: CashCountInput) => aCashEntry({ currency: input.currency, amount: input.counted }));
    const drawer = vi.fn(async () => aDrawer());
    const elevate = vi.fn(async () => elevated);
    renderWithProviders(<CashScreen />, { client: fakeClient({ cash: { drawer, count }, owner: { elevate } }), locale: "en" });
    await settle();
    await userEvent.click(within(screen.getByTestId("drawer-SYP")).getByRole("button", { name: "Count the drawer" }));
    const dialog = await screen.findByRole("dialog", { name: "Count — Syrian pound" });
    expect(dialog).toHaveTextContent("The drawer should hold 97,500 SYP.");
    await userEvent.type(within(dialog).getByLabelText("Counted"), "97000");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save the count" }));
    await settle();
    expect(count).toHaveBeenCalledWith({ currency: "SYP", counted: "97000", note: "" });
    expect(elevate).not.toHaveBeenCalled();
    expect(screen.queryByLabelText("Owner PIN")).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Count saved. Difference: -500 SYP.");
    expect(drawer).toHaveBeenCalledTimes(2);
  });

  it("an expense goes through the owner's PIN with its category and whether it left the drawer", async () => {
    const record = vi
      .fn(async (input: CashRecordInput) => aCashEntry({ kind: input.kind, amount: input.amount }))
      .mockImplementationOnce(async () => {
        throw required();
      });
    renderWithProviders(<CashScreen />, { client: fakeClient({ cash: { record }, owner: { elevate: async () => elevated } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Expense" }));
    const dialog = await screen.findByRole("dialog", { name: "Record an expense" });
    await userEvent.type(within(dialog).getByLabelText("Amount"), "250000");
    await userEvent.selectOptions(within(dialog).getByLabelText("Category"), "electricity");
    await userEvent.click(within(dialog).getByLabelText("Paid from the drawer"));
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await settle();
    expect(record).toHaveBeenCalledTimes(2);
    expect(record).toHaveBeenLastCalledWith({ kind: "expense", currency: "SYP", amount: "250000", category: "electricity", fromDrawer: false, note: "" });
    expect(screen.queryByRole("dialog", { name: "Record an expense" })).not.toBeInTheDocument();
  });

  it("a withdrawal sends no category; a refusal stays on the form under its field", async () => {
    const record = vi.fn(async () => {
      throw new BindingError({ code: "lite.cashbook.amount_decimals", messageKey: "lite.cashbook.amount_decimals", params: { currency: "SYP", decimals: "0" }, fields: [{ field: "amount", code: "lite.cashbook.amount_decimals", messageKey: "lite.cashbook.amount_decimals" }] });
    });
    renderWithProviders(<CashScreen />, { client: fakeClient({ cash: { record }, owner: { status: async () => elevated } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Take money out" }));
    const dialog = await screen.findByRole("dialog", { name: "Take money out of the drawer" });
    expect(within(dialog).queryByLabelText("Category")).not.toBeInTheDocument();
    await userEvent.type(within(dialog).getByLabelText("Amount"), "1.5");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await settle();
    expect(record).toHaveBeenCalledWith({ kind: "withdrawal", currency: "SYP", amount: "1.5", category: "", fromDrawer: false, note: "" });
    expect(within(dialog).getByRole("alert")).toHaveTextContent("A SYP amount takes at most 0 decimal places.");
  });

  it("the owner sees the cash book and reverses an entry with a reason", async () => {
    const expense = aCashEntry({ id: "x1", kind: "expense", amount: "25000", expected: "", difference: "", category: "electricity", fromDrawer: true, note: "فاتورة", reversible: true });
    const drawer = async () => ({ ...aDrawer(), ownerView: true, entries: [aCashEntry(), expense] });
    const reverse = vi.fn(async (entryId: string, reason: string) => aCashEntry({ id: `${entryId}-r`, kind: "reversal", note: reason }));
    renderWithProviders(<CashScreen />, { client: fakeClient({ cash: { drawer, reverse }, owner: { status: async () => elevated } }), locale: "en" });
    await settle();
    const rows = within(screen.getByTestId("cash-book")).getAllByRole("row");
    expect(rows[1]).toHaveTextContent("Expected 97,500, difference -500");
    expect(rows[2]).toHaveTextContent("Electricity");
    expect(rows[2]).toHaveTextContent("paid from the drawer");
    expect(within(rows[1]!).queryByRole("button", { name: "Reverse" })).not.toBeInTheDocument();
    await userEvent.click(within(rows[2]!).getByRole("button", { name: "Reverse" }));
    const dialog = await screen.findByRole("dialog", { name: "Reverse — Expense" });
    await userEvent.type(within(dialog).getByLabelText("Reason"), "مكررة");
    await userEvent.click(within(dialog).getByRole("button", { name: "Reverse the entry" }));
    await settle();
    expect(reverse).toHaveBeenCalledWith("x1", "مكررة");
  });

  it("a past day is shown as it was, with no count or entry buttons", async () => {
    const drawer = vi.fn(async (date: string) => aDrawer({ date: date || "2026-09-14" }));
    renderWithProviders(<CashScreen />, { client: fakeClient({ cash: { drawer } }), locale: "en" });
    await settle();
    fireEvent.change(screen.getByLabelText("Day"), { target: { value: "2026-09-10" } });
    await settle();
    expect(drawer).toHaveBeenLastCalledWith("2026-09-10");
    expect(screen.queryByRole("button", { name: "Count the drawer" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Expense" })).not.toBeInTheDocument();
    expect(screen.getByText(/A past day is shown as it was/)).toBeInTheDocument();
  });

  it("reads in Arabic", async () => {
    renderWithProviders(<CashScreen />, { client: fakeClient({ cash: { drawer: async () => counted(aDrawer()) } }), locale: "ar" });
    await settle();
    expect(screen.getByRole("heading", { name: "حركة الصندوق" })).toBeInTheDocument();
    expect(screen.getByTestId("drawer-SYP")).toHaveReadableText("ناقص مُعاد عند إلغاء فواتير20,000 ل.س");
    expect(screen.getByTestId("expected-SYP")).toHaveTextContent("97,500 ل.س");
  });
});

describe("CashScreen — L7", () => {
  it("exports the drawer of the day on screen, through the owner's PIN", async () => {
    const report = vi
      .fn(async () => ({ path: "/Users/shop/drawer.pdf", bytes: 1, cancelled: false }))
      .mockImplementationOnce(async () => {
        throw required();
      });
    renderWithProviders(<CashScreen />, { client: fakeClient({ exports: { report } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "PDF" }));
    await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await settle();
    expect(report).toHaveBeenCalledTimes(2);
    expect(report).toHaveBeenLastCalledWith({ kind: "drawer", date: aDrawer().date, month: "", from: "", to: "", format: "pdf" });
    expect(screen.getByText("/Users/shop/drawer.pdf")).toBeInTheDocument();
  });
});

describe("CashScreen — moving between days (owner's testing, 2026-09-16)", () => {
  it("steps a day back and forward, and returns to today, without opening the date picker", async () => {
    const drawer = vi.fn(async (date: string) => aDrawer({ date: date || "2026-09-14", today: "2026-09-14" }));
    renderWithProviders(<CashScreen />, { client: fakeClient({ cash: { drawer } }), locale: "en" });
    await settle();
    expect(drawer).toHaveBeenLastCalledWith("");

    await userEvent.click(screen.getByRole("button", { name: "The day before" }));
    await settle();
    expect(drawer).toHaveBeenLastCalledWith("2026-09-13");

    await userEvent.click(screen.getByRole("button", { name: "The day before" }));
    await settle();
    expect(drawer).toHaveBeenLastCalledWith("2026-09-12");

    await userEvent.click(screen.getByRole("button", { name: "The day after" }));
    await settle();
    expect(drawer).toHaveBeenLastCalledWith("2026-09-13");

    await userEvent.click(screen.getByRole("button", { name: "Today" }));
    await settle();
    expect(drawer).toHaveBeenLastCalledWith("2026-09-14");
  });

  it("steps across the end of a month, and cannot walk past today", async () => {
    const drawer = vi.fn(async (date: string) => aDrawer({ date: date || "2026-09-01", today: "2026-09-01" }));
    renderWithProviders(<CashScreen />, { client: fakeClient({ cash: { drawer } }), locale: "en" });
    await settle();
    // On today, forward and Today are both spent.
    expect(screen.getByRole("button", { name: "The day after" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Today" })).toBeDisabled();

    await userEvent.click(screen.getByRole("button", { name: "The day before" }));
    await settle();
    expect(drawer).toHaveBeenLastCalledWith("2026-08-31");
  });
});

describe("CountDialog — what counting is for (owner's testing, 2026-09-16)", () => {
  it("says in plain words what the cashier is being asked to do, in both languages", async () => {
    for (const locale of ["ar", "en"] as const) {
      const { unmount } = renderWithProviders(<CashScreen />, { client: fakeClient(), locale });
      await settle();
      const action = locale === "ar" ? "عُدّ الصندوق" : "Count the drawer";
      await userEvent.click(within(screen.getByTestId("drawer-SYP")).getByRole("button", { name: action }));
      const dialog = await screen.findByRole("dialog");
      const explanation =
        locale === "ar"
          ? "أدخل المبلغ الفعلي الموجود حالياً في الدرج لمطابقته مع المبيعات واكتشاف أي زيادة أو نقص في الصندوق."
          : "Type the money actually in the drawer now, so it can be matched against the sales and any surplus or shortfall is found.";
      expect(dialog).toHaveTextContent(explanation);
      unmount();
    }
  });
});
