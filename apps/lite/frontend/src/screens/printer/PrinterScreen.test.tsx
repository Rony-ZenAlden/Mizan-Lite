import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { BindingError } from "@/api/envelope";
import { fakeClient, printerSettings, renderWithProviders } from "@/api/testing";
import type { PrinterSettings, PrinterSettingsInput } from "@/api/client";
import { PrinterScreen } from "./PrinterScreen";

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}

const required = () => new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });

describe("PrinterScreen", () => {
  it("lists the system's printers and saves every setting through the owner's PIN", async () => {
    const save = vi
      .fn(async (input: PrinterSettingsInput): Promise<PrinterSettings> => printerSettings({ ...input, paperMm: Number(input.paperMm) }))
      .mockImplementationOnce(async () => {
        throw required();
      });
    renderWithProviders(<PrinterScreen />, { client: fakeClient({ printers: { save } }), locale: "en" });
    await settle();

    const printer = screen.getByLabelText("Receipt printer");
    expect(printer).toHaveValue("Xprinter XP-80");
    expect(within(printer).getAllByRole("option").map((o) => o.textContent)).toEqual(["No printer", "Xprinter XP-80 (default)", "Office Laser"]);
    expect(screen.getByLabelText("How receipts reach the printer")).toHaveValue("driver");
    expect(screen.getByLabelText("Automatic printing")).toHaveValue("credit");

    await userEvent.selectOptions(printer, "Office Laser");
    await userEvent.selectOptions(screen.getByLabelText("Paper width"), "58");
    await userEvent.selectOptions(screen.getByLabelText("How receipts reach the printer"), "raw");
    await userEvent.selectOptions(screen.getByLabelText("Automatic printing"), "all");
    await userEvent.click(screen.getByLabelText("Open the cash drawer when a receipt prints"));
    await userEvent.type(screen.getByLabelText("Phone"), "011 222 3344");
    await userEvent.type(screen.getByLabelText("Address"), "دمشق — المزة");
    await userEvent.type(screen.getByLabelText("Line at the foot of the receipt"), "شكراً لزيارتكم");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await settle();

    expect(save).toHaveBeenCalledTimes(2);
    expect(save).toHaveBeenLastCalledWith({
      printer: "Office Laser",
      paperMm: "58",
      path: "raw",
      autoPrint: "all",
      drawer: true,
      phone: "011 222 3344",
      address: "دمشق — المزة",
      footer: "شكراً لزيارتكم",
    });
    expect(screen.getByRole("status")).toHaveTextContent("The printer settings were saved");
  });

  it("keeps a chosen printer that is no longer installed visible, and says the drawer needs the direct path", async () => {
    const client = fakeClient({ printers: { settings: async () => printerSettings({ printer: "Old XP-58", drawer: true }), list: async () => [] } });
    renderWithProviders(<PrinterScreen />, { client, locale: "en" });
    await settle();
    expect(screen.getByLabelText("Receipt printer")).toHaveValue("Old XP-58");
    expect(screen.getByRole("option", { name: "Old XP-58 (not found now)" })).toBeInTheDocument();
    expect(screen.getByText("No printers are installed on this computer.")).toBeInTheDocument();
    expect(screen.getByText("The drawer opens only when printing straight to the printer.")).toBeInTheDocument();
  });

  it("puts a refusal under the field Go names", async () => {
    const save = vi.fn(async (): Promise<PrinterSettings> => {
      throw new BindingError({
        code: "lite.settings.text_too_long",
        messageKey: "lite.settings.text_too_long",
        params: { max: "120" },
        fields: [{ field: "footer", code: "lite.settings.text_too_long", messageKey: "lite.settings.text_too_long" }],
      });
    });
    renderWithProviders(<PrinterScreen />, { client: fakeClient({ printers: { save } }), locale: "ar" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "حفظ" }));
    await settle();
    const footer = screen.getByLabelText("سطر أسفل الإيصال");
    expect(footer).toHaveAttribute("aria-invalid", "true");
  });

  it("prints a test page, and a failure is translated", async () => {
    const test = vi
      .fn(async () => ({ printer: "Xprinter XP-80", copyNo: 1, path: "driver" }))
      .mockImplementationOnce(async () => {
        throw new BindingError({ code: "lite.printers.no_printer", messageKey: "lite.printers.no_printer" });
      });
    renderWithProviders(<PrinterScreen />, { client: fakeClient({ printers: { test } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Print a test page" }));
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent("No receipt printer is chosen. Choose one in Printer settings.");
    await userEvent.click(screen.getByRole("button", { name: "Print a test page" }));
    await settle();
    expect(screen.getByRole("status")).toHaveTextContent("Sent to Xprinter XP-80");
  });

  it("a printer list that cannot be read says so and can be read again", async () => {
    const list = vi
      .fn(async () => [{ name: "Xprinter XP-80", default: false }])
      .mockImplementationOnce(async () => {
        throw new BindingError({ code: "lite.printers.list_failed", messageKey: "lite.printers.list_failed" });
      });
    renderWithProviders(<PrinterScreen />, { client: fakeClient({ printers: { list } }), locale: "en" });
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent("The printers could not be listed.");
    await userEvent.click(screen.getByRole("button", { name: "Refresh the list" }));
    await settle();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Xprinter XP-80" })).toBeInTheDocument();
  });
});
