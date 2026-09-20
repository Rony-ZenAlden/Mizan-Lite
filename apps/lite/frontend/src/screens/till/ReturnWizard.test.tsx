import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aReturnable, aSaleReturn, fakeClient, renderWithProviders } from "@/api/testing";
import { ReturnWizard } from "./ReturnWizard";

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}

describe("ReturnWizard — F8 at the counter (owner's request, 2026-09-20)", () => {
  it("finds the sale by the number on the paper, prices what is ticked, then records it", async () => {
    const returnable = vi.fn(async () => aReturnable());
    const quoteReturn = vi.fn(async () => aSaleReturn({ id: "", refund: "36500" }));
    const recordReturn = vi.fn(async () => aSaleReturn({ returnNo: "3" }));
    const onDone = vi.fn();
    renderWithProviders(<ReturnWizard onClose={() => {}} onDone={onDone} />, {
      client: fakeClient({ sales: { returnable, quoteReturn, recordReturn } }),
      locale: "en",
    });

    await userEvent.type(screen.getByLabelText("Receipt number"), "1042");
    await userEvent.click(screen.getByRole("button", { name: "Find the sale" }));
    await settle();
    expect(returnable).toHaveBeenCalledWith("1042");

    const sale = screen.getByTestId("return-sale");
    expect(sale).toHaveTextContent("Receipt 1042");
    // The line says what is left, so the counter can see it without arithmetic.
    expect(within(sale).getByTestId("return-line")).toHaveTextContent("2.000");

    await userEvent.type(screen.getByLabelText("Quantity of Olive oil coming back"), "1");
    await userEvent.type(screen.getByLabelText("Why it is coming back"), "منتفخة");
    await userEvent.click(screen.getByRole("button", { name: "To hand back" }));
    await settle();

    // Nothing has been recorded yet: the customer is told the figure first.
    expect(recordReturn).not.toHaveBeenCalled();
    expect(quoteReturn).toHaveBeenCalledWith(
      expect.objectContaining({ lines: [{ saleLineId: "line-1", quantity: "1", restock: true }], settlement: "cash" }),
    );
    expect(screen.getByTestId("return-quote")).toHaveTextContent("36,500");

    await userEvent.click(screen.getByRole("button", { name: "Record the return" }));
    await settle();
    expect(recordReturn).toHaveBeenCalledTimes(1);
    expect(onDone).toHaveBeenCalledWith(expect.objectContaining({ returnNo: "3" }));
  });

  it("clears a priced figure the moment what is ticked changes, so nobody is handed a stale amount", async () => {
    renderWithProviders(<ReturnWizard onClose={() => {}} onDone={() => {}} />, {
      client: fakeClient({ sales: { returnable: async () => aReturnable(), quoteReturn: async () => aSaleReturn() } }),
      locale: "en",
    });
    await userEvent.type(screen.getByLabelText("Receipt number"), "1042");
    await userEvent.click(screen.getByRole("button", { name: "Find the sale" }));
    await settle();
    await userEvent.type(screen.getByLabelText("Quantity of Olive oil coming back"), "1");
    await userEvent.type(screen.getByLabelText("Why it is coming back"), "سبب");
    await userEvent.click(screen.getByRole("button", { name: "To hand back" }));
    await settle();
    expect(screen.getByTestId("return-quote")).toBeInTheDocument();

    await userEvent.type(screen.getByLabelText("Quantity of Olive oil coming back"), "2");
    expect(screen.queryByTestId("return-quote")).not.toBeInTheDocument();
    // And the confirm button is gone with it: there is nothing agreed to confirm.
    expect(screen.queryByRole("button", { name: "Record the return" })).not.toBeInTheDocument();
  });

  it("offers the debt only on a credit sale, because a cash sale has no balance to reduce", async () => {
    renderWithProviders(<ReturnWizard onClose={() => {}} onDone={() => {}} />, {
      client: fakeClient({ sales: { returnable: async () => aReturnable({ payment: "cash" }) } }),
      locale: "en",
    });
    await userEvent.type(screen.getByLabelText("Receipt number"), "1042");
    await userEvent.click(screen.getByRole("button", { name: "Find the sale" }));
    await settle();
    const how = screen.getByLabelText("How the money goes back");
    expect([...how.querySelectorAll("option")].map((o) => o.textContent)).toEqual(["Cash from the drawer"]);
  });

  it("says plainly when a sale was voided or has nothing left, instead of offering a form that cannot work", async () => {
    const { unmount } = renderWithProviders(<ReturnWizard onClose={() => {}} onDone={() => {}} />, {
      client: fakeClient({ sales: { returnable: async () => aReturnable({ voided: true }) } }),
      locale: "en",
    });
    await userEvent.type(screen.getByLabelText("Receipt number"), "1042");
    await userEvent.click(screen.getByRole("button", { name: "Find the sale" }));
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent("voided");
    expect(screen.queryByTestId("return-line")).not.toBeInTheDocument();
    unmount();

    const spent = aReturnable();
    spent.lines[0]!.returnable = false;
    spent.lines[0]!.left = "0.000";
    renderWithProviders(<ReturnWizard onClose={() => {}} onDone={() => {}} />, {
      client: fakeClient({ sales: { returnable: async () => spent } }),
      locale: "en",
    });
    await userEvent.type(screen.getByLabelText("Receipt number"), "1042");
    await userEvent.click(screen.getByRole("button", { name: "Find the sale" }));
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent("already been returned");
  });
});
