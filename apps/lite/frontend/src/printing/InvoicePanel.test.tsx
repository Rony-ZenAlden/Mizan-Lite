import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { PrintResult } from "@/api/client";
import { BindingError } from "@/api/envelope";
import { fakeClient, renderWithProviders } from "@/api/testing";
import { InvoicePanel } from "./InvoicePanel";

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}

describe("InvoicePanel — a sale's A4 invoice (2026-09-24)", () => {
  it("shows Go's A4 page, prints it on the A4 printer, and saves it as a PDF", async () => {
    const preview = vi.fn(async () => ({ png: "iVBORw0KGgo=", width: 909, height: 1286, copyNo: 1 }));
    const invoice = vi.fn(async () => ({ printer: "Office Laser", copyNo: 1, path: "driver" }));
    const saved = vi.fn(async () => ({ path: "/Users/shop/Documents/كردي - فاتورة رقم 7.pdf", bytes: 9000, cancelled: false }));
    renderWithProviders(<InvoicePanel saleId="sale-7" preview />, {
      client: fakeClient({ print: { preview, invoice }, exports: { invoice: saved } }),
      locale: "en",
    });
    await settle();
    expect(preview).toHaveBeenCalledWith("invoice", "sale-7");
    expect(screen.getByRole("img", { name: "The invoice as it prints on A4" })).toHaveAttribute("width", "909");

    await userEvent.click(screen.getByRole("button", { name: "Print A4 invoice" }));
    expect(await screen.findByTestId("invoice-printed")).toHaveTextContent("The invoice was sent to Office Laser.");
    await userEvent.click(screen.getByRole("button", { name: "Save invoice (PDF)" }));
    expect(await screen.findByTestId("invoice-saved")).toHaveTextContent("كردي - فاتورة رقم 7.pdf");
    expect(invoice).toHaveBeenCalledWith("sale-7");
    expect(saved).toHaveBeenCalledWith("sale-7");
  });

  it("with no A4 printer chosen, says where to choose one — and the PDF is still there", async () => {
    const invoice = vi.fn(async (): Promise<PrintResult> => {
      throw new BindingError({ code: "lite.print.no_invoice_printer", messageKey: "lite.print.no_invoice_printer" });
    });
    renderWithProviders(<InvoicePanel saleId="sale-7" preview={false} />, { client: fakeClient({ print: { invoice } }), locale: "en" });
    await userEvent.click(screen.getByRole("button", { name: "Print A4 invoice" }));
    expect(await screen.findByTestId("invoice-failed")).toHaveTextContent("No A4 printer is chosen for invoices.");
    expect(screen.getByRole("button", { name: "Save invoice (PDF)" })).toBeEnabled();
    expect(screen.queryByTestId("invoice-preview")).not.toBeInTheDocument();
  });
});
