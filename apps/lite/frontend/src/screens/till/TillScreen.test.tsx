import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aProduct, aQuote, aRate, aSale, fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import type { CartInput, CartQuote, CheckoutInput, Sale } from "@/api/client";
import { QUOTE_DEBOUNCE_MS, TillScreen } from "./TillScreen";

const jam = aProduct({ id: "jam", nameAr: "مربى", nameEn: "Apricot jam", unitCode: "jar", quickSlot: 2, barcode: "6291" });
const oil = aProduct({ quickSlot: 1 });
const scanJam = async () => ({ found: true, productId: "jam", nameAr: "مربى", nameEn: "Apricot jam", unitCode: "jar", unitDecimals: 0, active: true, onHand: "4" });
const products = async () => [oil, jam];

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, QUOTE_DEBOUNCE_MS + 60));
  });
}

async function enterPin() {
  await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
  await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
  await settle();
}

const scanField = () => screen.getByLabelText("Scan or search");

describe("TillScreen — adding", () => {
  it("a scanned barcode is added, priced by Go, and the scan field keeps focus", async () => {
    const quote = vi.fn<(input: CartInput) => Promise<CartQuote>>(async () => aQuote());
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products }, till: { scan: scanJam, quote } }), locale: "en" });
    await settle();
    await userEvent.type(scanField(), "٦٢٩١{Enter}");
    await settle();

    expect(quote).toHaveBeenLastCalledWith({
      lines: [{ productId: "jam", quantity: "1", discountPercent: "" }],
      settlement: "",
      saleDiscount: "",
      tenderCurrency: "",
      tendered: "",
      changeCurrency: "",
    });
    const total = screen.getByTestId("till-total");
    expect(total).toHaveTextContent("73,000 SYP");
    expect(total).toHaveTextContent("In the other currency: 4.88 USD");
    expect(total).toHaveTextContent("Rounded to the nearest 500: -125 SYP");
    expect(scanField()).toHaveFocus();
    expect(scanField()).toHaveValue("");
  });

  it("scanning a counted product again counts it up on the same line", async () => {
    const quote = vi.fn<(input: CartInput) => Promise<CartQuote>>(async () => aQuote());
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products }, till: { scan: scanJam, quote } }), locale: "en" });
    await settle();
    await userEvent.type(scanField(), "6291{Enter}");
    await userEvent.type(scanField(), "6291{Enter}");
    await settle();
    expect(screen.getAllByTestId("cart-line")).toHaveLength(1);
    expect(quote.mock.lastCall![0].lines).toEqual([{ productId: "jam", quantity: "2", discountPercent: "" }]);
  });

  it("a weighed product from a quick button asks how much first, in either digits", async () => {
    const quote = vi.fn<(input: CartInput) => Promise<CartQuote>>(async () => aQuote());
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products }, till: { quote } }), locale: "en" });
    await settle();
    const grid = screen.getByRole("group", { name: "Quick products" });
    expect(within(grid).getAllByRole("button").map((b) => b.textContent)).toEqual(["Olive oil", "Apricot jam"]);

    await userEvent.click(within(grid).getByRole("button", { name: "Olive oil" }));
    const dialog = await screen.findByRole("dialog", { name: "How much Olive oil?" });
    await userEvent.type(within(dialog).getByLabelText("Quantity (Litre)"), "1.2345");
    expect(within(dialog).getByText("This unit takes at most 3 decimal places.")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Add" })).toBeDisabled();
    await userEvent.clear(within(dialog).getByLabelText("Quantity (Litre)"));
    await userEvent.type(within(dialog).getByLabelText("Quantity (Litre)"), "1٫750{Enter}");
    await settle();

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(quote.mock.lastCall![0].lines).toEqual([{ productId: oil.id, quantity: "1٫750", discountPercent: "" }]);
    await waitFor(() => expect(scanField()).toHaveFocus());
  });

  it("a counted product from a quick button is added at once, and focus returns to the scan field for the scanner", async () => {
    const quote = vi.fn<(input: CartInput) => Promise<CartQuote>>(async () => aQuote());
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products }, till: { quote } }), locale: "en" });
    await settle();
    const button = within(screen.getByRole("group", { name: "Quick products" })).getByRole("button", { name: "Apricot jam" });
    await userEvent.click(button);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(scanField()).toHaveFocus();
    await userEvent.click(button);
    await settle();
    expect(quote.mock.lastCall![0].lines).toEqual([{ productId: "jam", quantity: "2", discountPercent: "" }]);
  });

  it("an unknown barcode searches by name and offers what matches, or says nothing matched", async () => {
    const search = vi.fn(async (q: { text: string }) => (q.text === "jam" ? [jam] : q.text === "" ? [oil, jam] : []));
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products: search } }), locale: "en" });
    await settle();
    await userEvent.type(scanField(), "jam{Enter}");
    await settle();
    const results = screen.getByRole("list", { name: "Matching products" });
    await userEvent.click(within(results).getByRole("button", { name: "Apricot jam" }));
    await settle();
    expect(screen.getAllByTestId("cart-line")).toHaveLength(1);

    await userEvent.type(scanField(), "zzz{Enter}");
    await settle();
    expect(screen.getByText("Nothing found for “zzz”.")).toBeInTheDocument();
  });
});

describe("TillScreen — the cart as Go priced it", () => {
  it("shows each line in both currencies and its warnings", async () => {
    const q = aQuote();
    q.lines[0]!.warnings = ["beyond_stock", "no_cost"];
    q.lines[0]!.onHand = "0.500";
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products }, till: { scan: scanJam, quote: async () => q } }), locale: "en" });
    await settle();
    await userEvent.type(scanField(), "6291{Enter}");
    await settle();
    const line = screen.getByTestId("cart-line");
    expect(line).toHaveTextContent("73,125 SYP");
    expect(line).toHaveTextContent("4.88 USD");
    expect(line).toHaveTextContent("More than the 0.500 Jar on the shelf — the sale takes stock below zero.");
    expect(line).toHaveTextContent("This product's cost is not known yet; it is recorded as unknown.");
  });

  it("Go's refusal of a line is shown under that line's quantity, and Pay waits", async () => {
    const quote = vi.fn(async () => {
      throw new BindingError({
        code: "lite.sales.quantity_decimals",
        messageKey: "lite.sales.quantity_decimals",
        params: { decimals: "0", line: "1" },
        fields: [{ field: "lines.1.quantity", code: "lite.sales.quantity_decimals", messageKey: "lite.sales.quantity_decimals" }],
      });
    });
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products }, till: { scan: scanJam, quote } }), locale: "en" });
    await settle();
    await userEvent.type(scanField(), "6291{Enter}");
    await settle();
    const qty = within(screen.getByTestId("cart-line")).getByLabelText("Quantity of Apricot jam");
    expect(qty).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText("This unit takes at most 0 decimal places.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pay" })).toBeDisabled();
  });

  it("an answer to an older cart arriving late never replaces the newer cart's total", async () => {
    let releaseFirst: (q: CartQuote) => void = () => {};
    const quote = vi
      .fn<(input: CartInput) => Promise<CartQuote>>(async () => aQuote({ total: "90000", token: "token-2" }))
      .mockImplementationOnce(() => new Promise<CartQuote>((resolve) => (releaseFirst = resolve)));
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products }, till: { scan: scanJam, quote } }), locale: "en" });
    await settle();
    await userEvent.type(scanField(), "6291{Enter}");
    await settle(); // the first quote is in flight
    await userEvent.type(scanField(), "6291{Enter}");
    await settle(); // the second has answered
    expect(screen.getByTestId("till-total")).toHaveTextContent("90,000 SYP");
    await act(async () => releaseFirst(aQuote({ total: "45000", token: "token-1" })));
    await settle();
    expect(screen.getByTestId("till-total")).toHaveTextContent("90,000 SYP");
  });

  it("dollars handed over for a pound total: the change is Go's, in pounds by default", async () => {
    const quote = vi.fn(async (input: CartInput) =>
      input.tendered ? aQuote({ tenderCurrency: "USD", tendered: "5.00", tenderGiven: true, change: "2000", changeCurrency: "SYP" }) : aQuote(),
    );
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products }, till: { scan: scanJam, quote } }), locale: "en" });
    await settle();
    await userEvent.type(scanField(), "6291{Enter}");
    await userEvent.selectOptions(screen.getByLabelText("Paid in"), "USD");
    await userEvent.type(screen.getByLabelText("Amount handed over"), "5");
    await settle();
    expect(quote).toHaveBeenLastCalledWith(expect.objectContaining({ tenderCurrency: "USD", tendered: "5", changeCurrency: "" }));
    expect(screen.getByTestId("till-change")).toHaveTextContent("Change: 2,000 SYP");
  });

  it("charging in dollars sends the dollar settlement", async () => {
    const quote = vi.fn<(input: CartInput) => Promise<CartQuote>>(async () => aQuote({ settlement: "USD", total: "4.88", rounding: "0.00", cashNote: "", otherCurrency: "SYP", totalOther: "73125" }));
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products }, till: { scan: scanJam, quote } }), locale: "en" });
    await settle();
    await userEvent.type(scanField(), "6291{Enter}");
    await userEvent.click(screen.getByRole("button", { name: "Charge in US dollar" }));
    await settle();
    expect(quote.mock.lastCall![0].settlement).toBe("USD");
    expect(screen.getByRole("button", { name: "Charge in US dollar" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByTestId("till-total")).toHaveTextContent("4.88 USD");
    expect(screen.getByTestId("till-total")).not.toHaveTextContent("Rounded");
  });
});

describe("TillScreen — paying", () => {
  it("pays with the token of the quote on screen, shows the receipt and starts a new sale", async () => {
    const checkout = vi.fn<(input: CheckoutInput) => Promise<Sale>>(async () => aSale());
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products }, till: { scan: scanJam, checkout } }), locale: "en" });
    await settle();
    await userEvent.type(scanField(), "6291{Enter}");
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Pay" }));
    await settle();

    expect(checkout).toHaveBeenCalledTimes(1);
    expect(checkout.mock.calls[0]![0]).toEqual({
      cart: { lines: [{ productId: "jam", quantity: "1", discountPercent: "" }], settlement: "", saleDiscount: "", tenderCurrency: "", tendered: "", changeCurrency: "" },
      token: "token-1",
      payment: "cash",
    });
    const receipt = await screen.findByRole("dialog", { name: "Receipt No. 7" });
    expect(within(receipt).getByTestId("receipt")).toHaveTextContent("Total73,000 SYP");
    await userEvent.click(within(receipt).getByRole("button", { name: "Close" }));
    expect(screen.queryAllByTestId("cart-line")).toHaveLength(0);
    await waitFor(() => expect(scanField()).toHaveFocus());
  });

  it("a quote gone stale is priced again and Pay must be pressed again", async () => {
    let token = "token-1";
    const quote = vi.fn(async () => aQuote({ token, total: token === "token-1" ? "73000" : "74500" }));
    const checkout = vi
      .fn<(input: CheckoutInput) => Promise<Sale>>(async () => aSale())
      .mockImplementationOnce(async () => {
        token = "token-2";
        throw new BindingError({ code: "lite.sales.quote_stale", messageKey: "lite.sales.quote_stale" });
      });
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products }, till: { scan: scanJam, quote, checkout } }), locale: "en" });
    await settle();
    await userEvent.type(scanField(), "6291{Enter}");
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Pay" }));
    await settle();

    expect(screen.getByText("The rate or a price changed — check the new total and press Pay again.")).toBeInTheDocument();
    expect(screen.getByTestId("till-total")).toHaveTextContent("74,500 SYP");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(checkout).toHaveBeenCalledTimes(1);

    await userEvent.click(screen.getByRole("button", { name: "Pay" }));
    await settle();
    expect(checkout).toHaveBeenCalledTimes(2);
    expect(checkout.mock.calls[1]![0].token).toBe("token-2");
  });

  it("a discount asks for the owner PIN at Pay and the sale is retried once", async () => {
    const quote = vi.fn(async (input: CartInput) => aQuote({ discounted: input.lines.some((l) => l.discountPercent !== "") }));
    const checkout = vi
      .fn<(input: CheckoutInput) => Promise<Sale>>(async () => aSale())
      .mockImplementationOnce(async () => {
        throw new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
      });
    const owner = { elevate: async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: 120 }) };
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products }, till: { scan: scanJam, quote, checkout }, owner }), locale: "en" });
    await settle();
    await userEvent.type(scanField(), "6291{Enter}");
    await userEvent.type(screen.getByLabelText("Discount percent for Apricot jam"), "10");
    await settle();
    expect(screen.getByText("A discount needs the owner PIN when you press Pay.")).toBeInTheDocument();
    expect(quote.mock.lastCall![0].lines[0]!.discountPercent).toBe("10");

    await userEvent.click(screen.getByRole("button", { name: "Pay" }));
    await enterPin();
    expect(checkout).toHaveBeenCalledTimes(2);
    expect(await screen.findByRole("dialog", { name: "Receipt No. 7" })).toBeInTheDocument();
  });

  it("with no rate the till says so, links to the rate, and cannot take payment", async () => {
    const quote = vi.fn(async () => {
      throw new BindingError({ code: "lite.sales.no_rate", messageKey: "lite.sales.no_rate" });
    });
    renderWithProviders(<TillScreen />, {
      client: fakeClient({ catalog: { products }, till: { scan: scanJam, quote }, fx: { current: async () => aRate({ set: false, rate: "" }) } }),
      locale: "en",
    });
    await settle();
    await userEvent.type(scanField(), "6291{Enter}");
    await settle();
    expect(screen.getByRole("link", { name: "Go to the exchange rate" })).toHaveAttribute("href", "/rates");
    expect(screen.getAllByText("Set an exchange rate before selling.")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Pay" })).toBeDisabled();
  });

  it("with no rate in force Pay stays disabled even if a quote answered", async () => {
    // The header's rate and a quote are two reads: the till trusts the one that says it cannot sell.
    renderWithProviders(<TillScreen />, {
      client: fakeClient({ catalog: { products }, till: { scan: scanJam }, fx: { current: async () => aRate({ set: false, rate: "" }) } }),
      locale: "en",
    });
    await settle();
    await userEvent.type(scanField(), "6291{Enter}");
    await settle();
    expect(screen.getByTestId("till-total")).toHaveTextContent("73,000 SYP");
    expect(screen.getByRole("button", { name: "Pay" })).toBeDisabled();
  });

  it("reads in Arabic, right to left", async () => {
    renderWithProviders(<TillScreen />, { client: fakeClient({ catalog: { products }, till: { scan: scanJam } }), locale: "ar" });
    await settle();
    await userEvent.type(screen.getByLabelText("امسح أو ابحث"), "6291{Enter}");
    await settle();
    expect(screen.getByTestId("till-total")).toHaveTextContent("73,000 ل.س");
    expect(screen.getByRole("button", { name: "ادفع" })).toBeEnabled();
  });
});
