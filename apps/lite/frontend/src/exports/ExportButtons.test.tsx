import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { BindingError } from "@/api/envelope";
import { fakeClient, renderWithProviders } from "@/api/testing";
import type { ExportFormat, ExportResult } from "@/api/client";
import { ExportButtons, RangeExport } from "./ExportButtons";

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}

const saved = (path: string): ExportResult => ({ path, bytes: 5120, cancelled: false });

describe("ExportButtons", () => {
  it("asks Go for the format pressed, confirms the path it saved to, and shows it in its folder", async () => {
    const onExport = vi.fn(async (format: ExportFormat) => saved(`/Users/shop/Documents/Day 2026-09-14.${format}`));
    const showInFolder = vi.fn(async () => true);
    renderWithProviders(<ExportButtons onExport={onExport} />, { client: fakeClient({ exports: { showInFolder } }), locale: "en" });

    await userEvent.click(screen.getByRole("button", { name: "Excel" }));
    await settle();
    expect(onExport).toHaveBeenCalledWith("xlsx");
    const done = screen.getByRole("status");
    expect(done).toHaveTextContent("The file was saved");
    const path = within(done).getByText("/Users/shop/Documents/Day 2026-09-14.xlsx");
    expect(path.tagName).toBe("BDI");
    expect(path).toHaveAttribute("dir", "ltr");

    await userEvent.click(within(done).getByRole("button", { name: "Show in folder" }));
    expect(showInFolder).toHaveBeenCalledWith("/Users/shop/Documents/Day 2026-09-14.xlsx");

    await userEvent.click(screen.getByRole("button", { name: "PDF" }));
    await settle();
    expect(onExport).toHaveBeenLastCalledWith("pdf");
    expect(screen.getByRole("status")).toHaveTextContent("Day 2026-09-14.pdf");
  });

  it("a closed Save dialog says nothing", async () => {
    renderWithProviders(<ExportButtons onExport={async () => ({ path: "", bytes: 0, cancelled: true })} />, { locale: "en" });
    await userEvent.click(screen.getByRole("button", { name: "PDF" }));
    await settle();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("an owner-only export asks for the PIN and runs again; a failure is translated", async () => {
    const onExport = vi
      .fn(async (): Promise<ExportResult> => {
        throw new BindingError({ code: "lite.api.save_failed", messageKey: "lite.api.save_failed" });
      })
      .mockImplementationOnce(async () => {
        throw new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
      });
    renderWithProviders(<ExportButtons onExport={onExport} />, { locale: "ar" });
    await userEvent.click(screen.getByRole("button", { name: "إكسل" }));
    await userEvent.type(await screen.findByLabelText("رمز المالك"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "تأكيد" }));
    await settle();
    expect(onExport).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("alert")).toHaveTextContent("تعذّر حفظ الملف");
  });

  it("a range export sends the dates typed, blank for the month to date", async () => {
    const onExport = vi.fn(async () => saved("/x.xlsx"));
    renderWithProviders(<RangeExport title="Export the sales history" onExport={onExport} />, { locale: "en" });
    const section = screen.getByRole("region", { name: "Export the sales history" });
    await userEvent.click(within(section).getByRole("button", { name: "Excel" }));
    await settle();
    expect(onExport).toHaveBeenLastCalledWith("", "", "xlsx");

    await userEvent.type(within(section).getByLabelText("From"), "2026-09-01");
    await userEvent.type(within(section).getByLabelText("To"), "2026-09-14");
    await userEvent.click(within(section).getByRole("button", { name: "PDF" }));
    await settle();
    expect(onExport).toHaveBeenLastCalledWith("2026-09-01", "2026-09-14", "pdf");
  });
});
