import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aCustomer, anEntry, aSale, aStatement, fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import type { DebtAmountInput, Entry, PaymentInput, PaymentQuote, ReverseEntryInput, Statement } from "@/api/client";
import { isNegative, unsigned } from "./Balances";
import { CUSTOMER_SEARCH_DEBOUNCE_MS, CustomersScreen } from "./CustomersScreen";
import { PAYMENT_DEBOUNCE_MS } from "./PaymentDialog";

const required = () => new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
const elevated = { setUp: true, lockedSeconds: 0, elevatedSeconds: 120 };

async function settle(ms = Math.max(CUSTOMER_SEARCH_DEBOUNCE_MS, PAYMENT_DEBOUNCE_MS) + 60) {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, ms));
  });
}

async function enterPin() {
  await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
  await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
  await settle();
}

const twoCurrencies = aCustomer({
  balances: [
    { currency: "SYP", balance: "150000", owedSince: "2026-09-01", lastPayment: "", reference: "10.00", referenceCurrency: "USD" },
    { currency: "USD", balance: "9.58", owedSince: "2026-09-12", lastPayment: "2026-09-14", reference: "143700", referenceCurrency: "SYP" },
  ],
});

async function openStatement(client = fakeClient()) {
  renderWithProviders(<CustomersScreen />, { client, locale: "en" });
  await settle();
  await userEvent.click(within(screen.getAllByRole("row")[1]!).getByRole("button", { name: "Statement" }));
  const dialog = await screen.findByRole("dialog", { name: "Statement — أبو محمد" });
  await settle();
  return dialog;
}

describe("CustomersScreen", () => {
  it("lists each balance in its own currency, never added together, each with its reference at today's rate", async () => {
    const search = vi.fn(async () => [twoCurrencies]);
    renderWithProviders(<CustomersScreen />, { client: fakeClient({ customers: { search } }), locale: "en" });
    await settle();
    const row = screen.getAllByRole("row")[1]!;
    expect(within(row).getByTestId("balance-SYP")).toHaveTextContent("150,000 SYP≈ 10.00 USD at 15,000, for reference");
    expect(within(row).getByTestId("balance-USD")).toHaveTextContent("9.58 USD≈ 143,700 SYP at 15,000, for reference");
    expect(row).not.toHaveTextContent("20.00"); // no combined figure anywhere
    expect(within(row).getByText("2026-09-01 · 2026-09-12")).toBeInTheDocument();
    expect(screen.getByTestId("debt-today-USD")).toHaveTextContent("Payments (1)6.67 USD");

    await userEvent.click(screen.getByLabelText("Only customers with a balance"));
    await userEvent.type(screen.getByLabelText("Search by name or phone"), "حلاق");
    await settle();
    expect(search).toHaveBeenLastCalledWith({ text: "حلاق", owingOnly: true, includeInactive: false });
  });

  it("a balance below zero reads in the customer's favour in the list, never as a debt", async () => {
    const owed = aCustomer({ balances: [{ currency: "SYP", balance: "-30000", owedSince: "", lastPayment: "2026-09-14", reference: "-2.00", referenceCurrency: "USD" }] });
    renderWithProviders(<CustomersScreen />, { client: fakeClient({ customers: { search: async () => [owed] } }), locale: "en" });
    await settle();
    const cell = screen.getByTestId("balance-SYP");
    expect(cell).toHaveTextContent("In the customer's favour: 30,000 SYP");
    expect(cell).not.toHaveTextContent("-30,000");
  });

  it("creates a customer, and shows Go's refusal of a duplicate name under the name", async () => {
    const create = vi
      .fn(async (input: { name: string; phone: string; note: string }) => aCustomer({ id: "new", name: input.name, balances: [] }))
      .mockImplementationOnce(async () => {
        throw new BindingError({
          code: "lite.customers.duplicate_name",
          messageKey: "lite.customers.duplicate_name",
          params: { existingName: "أبو محمد" },
          fields: [{ field: "name", code: "lite.customers.duplicate_name", messageKey: "lite.customers.duplicate_name" }],
        });
      });
    const statement = vi.fn(async (_id: string, currency: string) => aStatement({ currency, customer: aCustomer({ id: "new", name: "أبو محمد - الحلاق", balances: [] }), balance: "0", entries: [] }));
    renderWithProviders(<CustomersScreen />, { client: fakeClient({ customers: { create, statement } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "New customer" }));
    const dialog = await screen.findByRole("dialog", { name: "New customer" });
    await userEvent.type(within(dialog).getByLabelText("Name"), "ابو محمد");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await within(dialog).findByText("A customer with this name already exists: “أبو محمد”. Add a nickname to tell them apart.")).toBeInTheDocument();
    await userEvent.clear(within(dialog).getByLabelText("Name"));
    await userEvent.type(within(dialog).getByLabelText("Name"), "أبو محمد - الحلاق");
    await userEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("dialog", { name: "Statement — أبو محمد - الحلاق" })).toBeInTheDocument();
    expect(statement).toHaveBeenCalledWith("new", "USD");
  });

  it("the statement has a tab per currency and every entry with its balance after", async () => {
    const statement = vi.fn(async (_id: string, currency: string): Promise<Statement> =>
      currency === "SYP" ? aStatement({ currency, balance: "0", owedSince: "", entries: [] }) : aStatement({ currency }),
    );
    const dialog = await openStatement(fakeClient({ customers: { statement } }));
    expect(statement).toHaveBeenLastCalledWith(aCustomer().id, "USD");
    expect(within(dialog).getByRole("tab", { name: "US dollar" })).toHaveAttribute("aria-selected", "true");
    expect(within(dialog).getByTestId("statement-balance")).toHaveTextContent("Balance: 9.58 USDowed since 2026-09-12");
    const entries = within(dialog).getAllByTestId("statement-entry");
    expect(entries).toHaveLength(2);
    expect(entries[0]).toHaveTextContent("Payment");
    expect(entries[0]).toHaveTextContent("100,000 SYP handed over, 0 SYP change, at 15,000");
    expect(entries[1]).toHaveTextContent("Credit sale");
    expect(within(entries[1]!).queryByRole("button", { name: "Reverse" })).not.toBeInTheDocument();

    await userEvent.click(within(dialog).getByRole("tab", { name: "Syrian pound" }));
    await settle();
    expect(statement).toHaveBeenLastCalledWith(aCustomer().id, "SYP");
    expect(within(dialog).getByText("No entries in this currency.")).toBeInTheDocument();
    expect(within(dialog).queryByRole("button", { name: "Take payment" })).not.toBeInTheDocument();
  });

  it("opens a credit sale's receipt from its entry", async () => {
    const receipt = vi.fn(async () => aSale({ payment: "credit", creditCustomerId: aCustomer().id, creditCustomerName: "أبو محمد", creditCurrency: "USD", creditAmount: "16.25", creditBalanceAfter: "16.25" }));
    const dialog = await openStatement(fakeClient({ sales: { receipt } }));
    await userEvent.click(within(within(dialog).getAllByTestId("statement-entry")[1]!).getByRole("button", { name: "Receipt" }));
    expect(await screen.findByRole("dialog", { name: "Receipt No. 7" })).toBeInTheDocument();
    expect(receipt).toHaveBeenCalledWith(aSale().id);
  });
});

describe("CustomersScreen — payments", () => {
  it("quotes a payment in pounds at today's rate and records it with the quote's token", async () => {
    const quotePayment = vi.fn(async (input: PaymentInput): Promise<PaymentQuote> => ({
      currency: "USD", tenderCurrency: input.tenderCurrency || "USD", tendered: input.all ? "143500" : "100000", changeCurrency: "SYP", change: "0",
      settled: input.all ? "9.58" : "6.67", balanceBefore: "9.58", balanceAfter: input.all ? "0.00" : "2.91", all: input.all, rate: "15000", token: input.all ? "all" : "part",
    }));
    const recordPayment = vi.fn<(input: PaymentInput) => Promise<Entry>>(async () => anEntry());
    const dialog = await openStatement(fakeClient({ customers: { quotePayment, recordPayment } }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Take payment" }));
    const pay = await screen.findByRole("dialog", { name: "Payment — أبو محمد (US dollar)" });
    await userEvent.selectOptions(within(pay).getByLabelText("Paid in"), "SYP");
    await userEvent.type(within(pay).getByLabelText("Amount handed over"), "١٠٠٠٠٠");
    await settle();
    expect(quotePayment).toHaveBeenLastCalledWith(expect.objectContaining({ customerId: aCustomer().id, currency: "USD", tenderCurrency: "SYP", amount: "١٠٠٠٠٠", all: false }));
    expect(within(pay).getByTestId("payment-quote")).toHaveTextContent("Take100,000 SYPSettles6.67 USDBalance after2.91 USD");

    await userEvent.click(within(pay).getByLabelText("Pay the whole balance"));
    await settle();
    expect(within(pay).queryByLabelText("Amount handed over")).not.toBeInTheDocument();
    expect(within(pay).getByTestId("payment-quote")).toHaveTextContent("Take143,500 SYPSettles9.58 USDBalance after0.00 USD");
    await userEvent.click(within(pay).getByRole("button", { name: "Record payment" }));
    await settle();
    expect(recordPayment).toHaveBeenCalledWith(expect.objectContaining({ all: true, amount: "", token: "all" }));
    expect(screen.queryByRole("dialog", { name: /Payment/ })).not.toBeInTheDocument();
  });

  it("a payment gone stale is quoted again and must be recorded again", async () => {
    let token = "t1";
    const quotePayment = vi.fn(async (input: PaymentInput): Promise<PaymentQuote> => ({
      currency: "USD", tenderCurrency: "USD", tendered: "5.00", changeCurrency: "USD", change: "0.00", settled: "5.00",
      balanceBefore: "9.58", balanceAfter: token === "t1" ? "4.58" : "1.00", all: input.all, rate: "15000", token,
    }));
    const recordPayment = vi
      .fn<(input: PaymentInput) => Promise<Entry>>(async () => anEntry())
      .mockImplementationOnce(async () => {
        token = "t2";
        throw new BindingError({ code: "lite.customers.payment_stale", messageKey: "lite.customers.payment_stale" });
      });
    const dialog = await openStatement(fakeClient({ customers: { quotePayment, recordPayment } }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Take payment" }));
    const pay = await screen.findByRole("dialog", { name: "Payment — أبو محمد (US dollar)" });
    await userEvent.type(within(pay).getByLabelText("Amount handed over"), "5");
    await settle();
    await userEvent.click(within(pay).getByRole("button", { name: "Record payment" }));
    await settle();
    expect(within(pay).getByText("The rate or the balance changed — check the new figures and record again.")).toBeInTheDocument();
    expect(within(pay).getByTestId("payment-quote")).toHaveTextContent("Balance after1.00 USD");
    await userEvent.click(within(pay).getByRole("button", { name: "Record payment" }));
    await settle();
    expect(recordPayment).toHaveBeenLastCalledWith(expect.objectContaining({ token: "t2" }));
  });
});

describe("CustomersScreen — the owner's acts", () => {
  it("a write-off asks for the PIN and a reason", async () => {
    const writeOff = vi
      .fn(async (input: DebtAmountInput) => anEntry({ kind: "write_off", note: input.note }))
      .mockImplementationOnce(async () => {
        throw required();
      });
    const dialog = await openStatement(fakeClient({ customers: { writeOff }, owner: { elevate: async () => elevated } }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Write off" }));
    const form = await screen.findByRole("dialog", { name: "Write off — أبو محمد" });
    await userEvent.click(within(form).getByLabelText("Write off the whole balance"));
    expect(within(form).getByRole("button", { name: "Save" })).toBeDisabled();
    await userEvent.type(within(form).getByLabelText("Reason"), "سافر");
    await userEvent.click(within(form).getByRole("button", { name: "Save" }));
    await enterPin();
    expect(writeOff).toHaveBeenCalledTimes(2);
    expect(writeOff).toHaveBeenLastCalledWith({ customerId: aCustomer().id, currency: "USD", amount: "", all: true, note: "سافر" });
  });

  it("an opening balance from the paper book, in either currency, through the PIN", async () => {
    const opening = vi.fn(async (input: DebtAmountInput) => anEntry({ kind: "opening", amount: input.amount }));
    const dialog = await openStatement(fakeClient({ customers: { opening }, owner: { status: async () => elevated } }));
    await userEvent.click(within(dialog).getByRole("button", { name: "Opening balance" }));
    const form = await screen.findByRole("dialog", { name: "Opening balance — أبو محمد" });
    await userEvent.selectOptions(within(form).getByLabelText("Currency"), "SYP");
    await userEvent.type(within(form).getByLabelText("Amount (Syrian pound)"), "50000");
    await userEvent.type(within(form).getByLabelText("Note (for example: page 12)"), "صفحة 12");
    await userEvent.click(within(form).getByRole("button", { name: "Save" }));
    await settle();
    expect(opening).toHaveBeenCalledWith({ customerId: aCustomer().id, currency: "SYP", amount: "50000", all: false, note: "صفحة 12" });
  });

  it("a balance in the customer's favour reads so, offers a refund instead of a payment, and an entry is reversed through the PIN", async () => {
    const statement = vi.fn(async (): Promise<Statement> => aStatement({ balance: "-4.00", owedSince: "", customer: aCustomer({ balances: [{ currency: "USD", balance: "-4.00", owedSince: "", lastPayment: "", reference: "-60000", referenceCurrency: "SYP" }] }) }));
    const reverse = vi.fn(async (input: ReverseEntryInput) => anEntry({ kind: "reversal", reversesId: input.entryId })).mockImplementationOnce(async () => {
      throw required();
    });
    const refund = vi.fn(async () => anEntry({ kind: "refund" }));
    const dialog = await openStatement(fakeClient({ customers: { statement, reverse, refund }, owner: { elevate: async () => elevated } }));
    expect(within(dialog).getByTestId("statement-balance")).toHaveTextContent("In the customer's favour: 4.00 USD");
    expect(within(dialog).queryByRole("button", { name: "Take payment" })).not.toBeInTheDocument();

    await userEvent.click(within(dialog).getByRole("button", { name: "Refund" }));
    const refundForm = await screen.findByRole("dialog", { name: "Refund — أبو محمد" });
    await userEvent.selectOptions(within(refundForm).getByLabelText("Paid out in"), "SYP");
    await userEvent.type(within(refundForm).getByLabelText("Reason"), "نقداً");
    await userEvent.click(within(refundForm).getByRole("button", { name: "Refund" }));
    await settle();
    expect(refund).toHaveBeenCalledWith({ customerId: aCustomer().id, currency: "USD", tenderCurrency: "SYP", amount: "", all: true, reason: "نقداً" });

    await userEvent.click(within(within(dialog).getAllByTestId("statement-entry")[0]!).getByRole("button", { name: "Reverse" }));
    const reverseForm = await screen.findByRole("dialog", { name: "Reverse — Payment" });
    await userEvent.type(within(reverseForm).getByLabelText("Reason"), "دفعة مكررة");
    await userEvent.click(within(reverseForm).getByRole("button", { name: "Reverse" }));
    await enterPin();
    expect(reverse).toHaveBeenCalledTimes(2);
    expect(reverse).toHaveBeenLastCalledWith({ entryId: anEntry().id, reason: "دفعة مكررة" });
  });

  it("reads in Arabic, balances in their own currencies", async () => {
    renderWithProviders(<CustomersScreen />, { client: fakeClient({ customers: { search: async () => [twoCurrencies] } }), locale: "ar" });
    await settle();
    expect(screen.getByRole("heading", { name: "الزبائن والديون" })).toBeInTheDocument();
    expect(screen.getByTestId("balance-SYP")).toHaveTextContent("150,000 ل.س");
    expect(screen.getByTestId("balance-USD")).toHaveTextContent("9.58 دولار");
  });
});

describe("balance text", () => {
  it("reads a sign from Go's text without arithmetic", () => {
    expect(isNegative("-4.00") && !isNegative("-0.00") && !isNegative("4.00")).toBe(true);
    expect(unsigned("-4.00")).toBe("4.00");
    expect(unsigned("4.00")).toBe("4.00");
  });
});
