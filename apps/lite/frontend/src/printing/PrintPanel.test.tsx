import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";
import { BindingError } from "@/api/envelope";
import { anEntry, fakeClient, printerSettings, renderWithProviders } from "@/api/testing";
import type { PrintResult } from "@/api/client";
import { PrintPanel, printsItself } from "./PrintPanel";
import { VoucherDialog, hasVoucher } from "./VoucherDialog";

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}

const required = () => new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
const sent = (copyNo = 1): PrintResult => ({ printer: "Xprinter XP-80", copyNo, path: "driver" });

describe("printsItself (Q-L7.2)", () => {
  it.each([
    ["credit", "cash_sale", false],
    ["credit", "credit_sale", true],
    ["credit", "voucher", true],
    ["all", "cash_sale", true],
    ["all", "voucher", true],
    ["none", "credit_sale", false],
    ["none", "voucher", false],
  ] as const)("auto print %s: a %s prints itself = %s", (autoPrint, recorded, expected) => {
    expect(printsItself(printerSettings({ autoPrint }), recorded)).toBe(expected);
  });

  it("nothing prints itself with no printer chosen, or before the settings are read", () => {
    expect(printsItself(printerSettings({ printer: "", autoPrint: "all" }), "credit_sale")).toBe(false);
    expect(printsItself(null, "credit_sale")).toBe(false);
  });

  it("only payments and refunds have vouchers", () => {
    expect(["payment", "refund", "charge", "opening", "write_off", "reversal"].filter((kind) => hasVoucher({ kind }))).toEqual(["payment", "refund"]);
  });
});

describe("PrintPanel", () => {
  it("shows Go's bitmap as the preview — not an imitation — and a sent copy becomes Print a copy", async () => {
    let copies = 0;
    const preview = vi.fn(async () => ({ png: "QUJD", width: 576, height: 812, copyNo: copies + 1 }));
    const sale = vi.fn(async () => sent(++copies));
    renderWithProviders(<PrintPanel kind="sale" id="sale-1" preview />, { client: fakeClient({ print: { preview, sale } }), locale: "en" });
    await settle();

    const image = screen.getByRole("img", { name: "The receipt as it will print" });
    expect(image).toHaveAttribute("src", "data:image/png;base64,QUJD");
    expect(image).toHaveAttribute("width", "576");
    expect(preview).toHaveBeenCalledWith("sale", "sale-1");

    await userEvent.click(screen.getByRole("button", { name: "Print the receipt" }));
    await settle();
    expect(sale).toHaveBeenCalledWith("sale-1");
    expect(screen.getByRole("status")).toHaveTextContent("Sent to Xprinter XP-80");
    expect(preview).toHaveBeenCalledTimes(2); // read again: the next copy carries a copy stamp

    await userEvent.click(screen.getByRole("button", { name: "Print a copy" }));
    await settle();
    expect(screen.getByRole("status")).toHaveTextContent("Copy 2 sent to Xprinter XP-80");
  });

  it("a failed print says why, leaves the sale alone, and Print again sends it", async () => {
    const sale = vi
      .fn(async () => sent())
      .mockImplementationOnce(async () => {
        throw new BindingError({ code: "lite.printers.not_found", messageKey: "lite.printers.not_found", params: { printer: "XP-80" } });
      });
    renderWithProviders(<PrintPanel kind="sale" id="sale-1" />, { client: fakeClient({ print: { sale } }), locale: "en" });
    await userEvent.click(screen.getByRole("button", { name: "Print the receipt" }));
    await settle();

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("Not printed — what was recorded stands");
    expect(alert).toHaveTextContent("The printer “XP-80” is not installed on this computer.");
    await userEvent.click(within(alert).getByRole("button", { name: "Print again" }));
    await settle();
    expect(sale).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Sent to Xprinter XP-80");
  });

  it("printing itself happens once, even when React runs effects twice", async () => {
    const sale = vi.fn(async () => sent());
    renderWithProviders(
      <StrictMode>
        <PrintPanel kind="sale" id="sale-1" auto />
      </StrictMode>,
      { client: fakeClient({ print: { sale } }), locale: "ar" },
    );
    await settle();
    expect(sale).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("status")).toHaveTextContent("أُرسل إلى الطابعة Xprinter XP-80");
  });

  it("a refund's voucher is the owner's: the PIN, then it prints", async () => {
    const entry = vi
      .fn(async () => sent())
      .mockImplementationOnce(async () => {
        throw required();
      });
    const refund = anEntry({ id: "refund-1", kind: "refund", customerName: "أبو محمد" });
    renderWithProviders(<VoucherDialog entry={refund} onClose={() => {}} />, { client: fakeClient({ print: { entry } }), locale: "en" });
    const dialog = await screen.findByRole("dialog", { name: "Voucher — Refund" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Print the voucher" }));
    await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await settle();
    expect(entry).toHaveBeenCalledTimes(2);
    expect(entry).toHaveBeenLastCalledWith("refund-1");
    expect(within(dialog).getByText("Sent to Xprinter XP-80")).toBeInTheDocument();
  });

  it("a closed PIN dialog prints nothing and says nothing", async () => {
    const entry = vi.fn(async () => {
      throw required();
    });
    renderWithProviders(<PrintPanel kind="entry" id="refund-1" />, { client: fakeClient({ print: { entry } }), locale: "en" });
    await userEvent.click(screen.getByRole("button", { name: "Print the voucher" }));
    await userEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    await settle();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Print the voucher" })).toBeEnabled();
  });
});
