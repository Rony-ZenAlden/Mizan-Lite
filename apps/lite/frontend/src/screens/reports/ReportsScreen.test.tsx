import { act, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { BindingError } from "@/api/envelope";
import { aDayReport, aProfit, aStockReport, anAmount, fakeClient, renderWithProviders } from "@/api/testing";
import type { ProductRow, ProductsReport } from "@/api/client";
import { ReportsScreen } from "./ReportsScreen";

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

/** Fails once as Go does outside owner mode, then answers. */
function guarded<A extends unknown[], T>(answer: (...args: A) => Promise<T>) {
  return vi.fn(answer).mockImplementationOnce(async () => {
    throw required();
  });
}

const row = (overrides: Partial<ProductRow>): ProductRow => ({
  productId: "p",
  nameAr: "رز",
  nameEn: "Rice",
  unitCode: "kg",
  quantity: "1.000",
  revenueUsd: "1.00",
  costUsd: "0.50",
  profitUsd: "0.50",
  marginUsd: "50.0",
  revenueLocal: "15000",
  costLocal: "7500",
  profitLocal: "7500",
  marginLocal: "50.0",
  unknownLines: 0,
  unknownQuantity: "0.000",
  unknownUsd: "0.00",
  unknownLocal: "0",
  ...overrides,
});

describe("ReportsScreen", () => {
  it("asks for the PIN, then reads the day's statement top to bottom in dollars and pounds", async () => {
    const day = guarded(async (date: string) => aDayReport({ date: date || "2026-09-14" }));
    renderWithProviders(<ReportsScreen />, { client: fakeClient({ reports: { day }, owner: { elevate: async () => elevated } }), locale: "en" });
    await settle();
    await enterPin();
    expect(day).toHaveBeenCalledTimes(2);
    const rows = within(screen.getByTestId("statement")).getAllByRole("row").slice(1).map((r) => r.textContent);
    expect(rows).toEqual([
      "Revenue — 1 sales6.50 USD97,500 SYP",
      "Cost of goods sold4.00 USD60,000 SYP",
      "Gross profit2.50 USD37,500 SYP",
      "Margin38.5%38.5%",
      "Stock losses, at cost0.40 USD6,000 SYP",
      "Spoiled or expired0.40 USD6,000 SYP",
      "Debts written off0.00 USD0 SYP",
      "Expenses1.00 USD15,000 SYP",
      "Electricity1.00 USD15,000 SYP",
      "Net profit1.10 USD16,500 SYP",
    ]);
    expect(screen.getByText(/converted at the rate of the day, 15,000/)).toBeInTheDocument();
    expect(screen.getByTestId("takings-SYP")).toHaveTextContent("Sales (1)97,500 SYP");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows sales of goods with no recorded cost apart, as a banner", async () => {
    const day = async () => aDayReport({ profit: aProfit({ unknownLines: 3, unknownUsd: "3.20", unknownLocal: "48000" }) });
    renderWithProviders(<ReportsScreen />, { client: fakeClient({ reports: { day }, owner: { status: async () => elevated } }), locale: "en" });
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent("3 lines sold with no recorded cost — 3.20 USD / 48,000 SYP — are not in profit or margin");
  });

  it("a cancelled PIN shows no figures, and the owner can ask again", async () => {
    const day = vi.fn(async () => {
      throw required();
    });
    renderWithProviders(<ReportsScreen />, { client: fakeClient({ reports: { day } }), locale: "en" });
    await settle();
    await userEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    await settle();
    expect(screen.queryByTestId("statement")).not.toBeInTheDocument();
    expect(screen.getByText("Profit, costs, stock value and expenses are shown in owner mode.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Show the reports" })).toBeInTheDocument();
  });

  it("clears the figures when owner mode ends", async () => {
    vi.useFakeTimers();
    try {
      let reads = 0;
      const status = vi.fn(async () => {
        reads += 1;
        return { ...elevated, elevatedSeconds: reads === 1 ? 2 : 0 };
      });
      await act(async () => {
        renderWithProviders(<ReportsScreen />, { client: fakeClient({ owner: { status } }), locale: "en" });
        await vi.advanceTimersByTimeAsync(0);
      });
      expect(screen.getByTestId("statement")).toBeInTheDocument();
      for (let tick = 0; tick < 2; tick++) {
        await act(async () => {
          await vi.advanceTimersByTimeAsync(1000);
        });
      }
      expect(screen.queryByTestId("statement")).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Show the reports" })).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("a month lists its days with the totals, and a day opens its statement", async () => {
    const day = vi.fn(async (date: string) => aDayReport({ date: date || "2026-09-14" }));
    renderWithProviders(<ReportsScreen />, { client: fakeClient({ reports: { day }, owner: { status: async () => elevated } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("tab", { name: "Month" }));
    await settle();
    expect(screen.getByTestId("month-total")).toHaveTextContent("Month total16.50 USD2.50 USD1.10 USD37,500 SYP16,500 SYP");
    fireEvent.change(screen.getByLabelText("Month"), { target: { value: "2026-08" } });
    await settle();
    await userEvent.click(within(screen.getByTestId("month")).getByRole("button", { name: "14/09/2026" }));
    await settle();
    expect(screen.getByRole("tab", { name: "Day" })).toHaveAttribute("aria-selected", "true");
    expect(day).toHaveBeenLastCalledWith("2026-09-14");
  });

  it("products sort by profit, quantity or margin, and end with the reconciling row", async () => {
    const report = (from: string, to: string): ProductsReport => ({
      from: from || "2026-09-01",
      to: to || "2026-09-14",
      localCurrency: "SYP",
      rows: [
        row({ productId: "a", nameEn: "Rice", quantity: "9.000", profitUsd: "1.00", marginUsd: "10.0" }),
        row({ productId: "b", nameEn: "Oil", quantity: "2.000", profitUsd: "12.00", marginUsd: "5.0", unknownLines: 2, unknownUsd: "4.00" }),
        row({ productId: "c", nameEn: "Tea", quantity: "3.000", profitUsd: "-0.50", marginUsd: "60.0" }),
      ],
      discountUsd: "0.30",
      discountLocal: "4500",
      roundingLocal: "-250",
      total: aProfit({ revenueUsd: "20.70", sales: 4 }),
    });
    const products = vi.fn(async (from: string, to: string) => report(from, to));
    renderWithProviders(<ReportsScreen />, { client: fakeClient({ reports: { products }, owner: { status: async () => elevated } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("tab", { name: "Products" }));
    await settle();
    const names = () => within(screen.getByTestId("products")).getAllByRole("row").slice(1, 4).map((r) => r.firstChild?.textContent);
    expect(names()).toEqual(["Oil", "Rice", "Tea"]);
    expect(within(screen.getByTestId("products")).getAllByRole("row")[1]).toHaveTextContent("2 lines, 4.00 USD");
    await userEvent.selectOptions(screen.getByLabelText("Sort by"), "quantity");
    expect(names()).toEqual(["Rice", "Tea", "Oil"]);
    await userEvent.selectOptions(screen.getByLabelText("Sort by"), "margin");
    expect(names()).toEqual(["Tea", "Rice", "Oil"]);
    expect(screen.getByTestId("reconciling")).toHaveTextContent("Less whole-sale discounts, and the pounds cash rounding0.30 USDdiscounts 4,500, rounding -250 SYP");
    expect(screen.getByTestId("products-total")).toHaveTextContent("Total (4 sales)20.70 USD");
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-08-01" } });
    await settle();
    expect(products).toHaveBeenLastCalledWith("2026-08-01", "");
  });

  it("the stock report reconciles opening to closing value and names a difference's cause", async () => {
    const stock = async () =>
      aStockReport({
        reconciliation: { ...aStockReport().reconciliation, negativeStock: "1.20", rounding: "0.01", closing: "17.21" },
        belowZero: [{ ...aStockReport().lines[0]!, productId: "z", nameEn: "Sugar", onHand: "-2.000" }],
        leftOut: [{ productId: "x", nameAr: "جبنة", nameEn: "Cheese", reason: "no_cost" }],
      });
    renderWithProviders(<ReportsScreen />, { client: fakeClient({ reports: { stock }, owner: { status: async () => elevated } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("tab", { name: "Stock" }));
    await settle();
    const rec = screen.getByTestId("reconciliation");
    expect(rec).toHaveTextContent("Value at the start0.00 USD");
    expect(rec).toHaveTextContent("Sold, less voids-4.00 USD");
    expect(screen.getByTestId("negative-stock")).toHaveTextContent("1.20 USD");
    expect(rec).toHaveTextContent("Difference: stock sold beyond zero has no value1.20 USD");
    expect(rec).toHaveTextContent("Difference: rounding to the cent0.01 USD");
    expect(screen.getByTestId("closing")).toHaveTextContent("17.21 USD");
    expect(screen.getByText("1 products were sold beyond their stock and count as no value")).toBeInTheDocument();
    expect(screen.getByTestId("shelf-total")).toHaveTextContent("10.00 USD · 150,000 SYP at 15,000");
    expect(screen.getByTestId("shelf-excluded")).toHaveTextContent("Cheese — no cost recorded");
  });

  it("a balanced reconciliation shows no difference lines", async () => {
    renderWithProviders(<ReportsScreen />, { client: fakeClient({ owner: { status: async () => elevated } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("tab", { name: "Stock" }));
    await settle();
    expect(screen.queryByTestId("negative-stock")).not.toBeInTheDocument();
    expect(screen.queryByTestId("rounding")).not.toBeInTheDocument();
    expect(screen.getByText("The movements add up exactly from the start value to the end value.")).toBeInTheDocument();
  });

  it("a report Go refuses shows why", async () => {
    const day = async () => {
      throw new BindingError({ code: "lite.reports.bad_date", messageKey: "lite.reports.bad_date" });
    };
    renderWithProviders(<ReportsScreen />, { client: fakeClient({ reports: { day }, owner: { status: async () => elevated } }), locale: "en" });
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent("The date is not valid.");
  });

  it("reads in Arabic, dollars first", async () => {
    const day = async () => aDayReport({ losses: { ...aDayReport().losses, surplus: anAmount("0.10", "1500") } });
    renderWithProviders(<ReportsScreen />, { client: fakeClient({ reports: { day }, owner: { status: async () => elevated } }), locale: "ar" });
    await settle();
    expect(screen.getByRole("heading", { name: "التقارير" })).toBeInTheDocument();
    const head = within(screen.getByTestId("statement")).getAllByRole("columnheader").map((h) => h.textContent);
    expect(head).toEqual(["البند", "بالدولار", "بالليرة، بسعر كل بيع"]);
    expect(screen.getByTestId("net")).toHaveTextContent("صافي الربح1.10 دولار16,500 ل.س");
    expect(screen.getByText("زيادة عند الجرد (تُضاف)")).toBeInTheDocument();
  });
});

describe("ReportsScreen — L7 exports", () => {
  it("exports the report on screen: its tab, its date or range, the format pressed", async () => {
    const report = vi.fn(async () => ({ path: "/Users/shop/report.xlsx", bytes: 1, cancelled: false }));
    renderWithProviders(<ReportsScreen />, { client: fakeClient({ exports: { report }, owner: { status: async () => elevated } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Excel" }));
    await settle();
    expect(report).toHaveBeenLastCalledWith({ kind: "day", date: "2026-09-14", month: "", from: "", to: "", format: "xlsx" });

    await userEvent.click(screen.getByRole("tab", { name: "Month" }));
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "PDF" }));
    await settle();
    expect(report).toHaveBeenLastCalledWith(expect.objectContaining({ kind: "month", month: "2026-09", format: "pdf" }));

    await userEvent.click(screen.getByRole("tab", { name: "Stock" }));
    await settle();
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-09-01" } });
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2026-09-10" } });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Excel" }));
    await settle();
    expect(report).toHaveBeenLastCalledWith(expect.objectContaining({ kind: "stock", from: "2026-09-01", to: "2026-09-10", format: "xlsx" }));
  });

  it("offers no export while the figures are hidden", async () => {
    const day = vi.fn(async () => {
      throw required();
    });
    renderWithProviders(<ReportsScreen />, { client: fakeClient({ reports: { day } }), locale: "en" });
    await settle();
    await userEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    await settle();
    expect(screen.queryByRole("button", { name: "Excel" })).not.toBeInTheDocument();
  });
});

describe("ReportsScreen — any period (owner's request, 2026-09-17)", () => {
  it("offers a week, a year and a range of its own, and asks Go for the dates each one means", async () => {
    const period = vi.fn(async (from: string, to: string) => ({
      month: "", from, to, localCurrency: "SYP", days: [aDayReport()], total: aDayReport({ rate: "" }),
    }));
    renderWithProviders(<ReportsScreen />, { client: fakeClient({ reports: { period } }), locale: "en" });
    await settle();

    await userEvent.click(screen.getByRole("tab", { name: "Period" }));
    await settle();

    const picker = screen.getByLabelText("Period");
    expect([...picker.querySelectorAll("option")].map((o) => o.textContent)).toEqual([
      "This week",
      "This month",
      "This year",
      "The last 7 days",
      "The last 30 days",
      "A range I choose",
    ]);

    // A named span sends the dates it means; the last 7 days is 6 days back through today.
    await userEvent.selectOptions(picker, "last7");
    await settle();
    const [from, to] = period.mock.lastCall!;
    const days = (new Date(`${to}T12:00:00Z`).getTime() - new Date(`${from}T12:00:00Z`).getTime()) / 86_400_000;
    expect(days).toBe(6);

    // "A range I choose" shows the two date boxes instead.
    await userEvent.selectOptions(picker, "custom");
    await settle();
    expect(screen.getByLabelText("From")).toBeInTheDocument();
    expect(screen.getByLabelText("To")).toBeInTheDocument();
  });

  it("shows the stock at its selling price beside what it cost", async () => {
    renderWithProviders(<ReportsScreen />, { client: fakeClient(), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("tab", { name: "Stock" }));
    await settle();
    expect(screen.getByTestId("stock-retail")).toHaveTextContent("Stock at its selling price");
    expect(screen.getByTestId("stock-retail")).toHaveTextContent("26.00");
  });
});
