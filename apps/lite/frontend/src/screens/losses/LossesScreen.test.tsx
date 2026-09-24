import { act, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aLossReport, aProduct, fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import type { AdjustInput } from "@/api/client";
import { LossesScreen } from "./LossesScreen";

async function settle(ms = 60) {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, ms));
  });
}

const labneh = aProduct({ id: "p-labneh", nameAr: "لبنة بلدية", nameEn: "Labneh", unitCode: "kg", barcode: "6290001000100" });

describe("LossesScreen", () => {
  it("reads each reason's losses at cost, every line, and the goods that arrived damaged apart", async () => {
    const losses = vi.fn(async (from: string, to: string) => aLossReport({ from: from || "2026-09-01", to: to || "2026-09-24" }));
    renderWithProviders(<LossesScreen />, { client: fakeClient({ reports: { losses } }), locale: "en" });
    await settle();
    expect(losses).toHaveBeenCalledWith("", "");
    expect(screen.getByTestId("loss-total-spoiled")).toHaveTextContent("Spoiled (gone bad)");
    expect(screen.getByTestId("loss-total-spoiled")).toHaveTextContent("6.00 USD");
    expect(screen.getByTestId("loss-total")).toHaveTextContent("90,000 SYP");
    const line = screen.getByTestId("loss-line");
    expect(line).toHaveTextContent("Labneh");
    expect(line).toHaveTextContent("1.5");
    expect(line).toHaveTextContent("من الحر");
    const arrival = screen.getByTestId("arrival-line");
    expect(arrival).toHaveTextContent("المروى");
    expect(arrival).toHaveTextContent("5.00 USD");

    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-09-10" } });
    await settle();
    expect(losses).toHaveBeenLastCalledWith("2026-09-10", "");
  });

  it("with the PIN switched on, a cancelled PIN shows no cost", async () => {
    const losses = vi.fn(async () => {
      throw new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
    });
    renderWithProviders(<LossesScreen />, { client: fakeClient({ reports: { losses } }), locale: "en" });
    await settle();
    await userEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    await settle();
    expect(screen.getByText("Losses are shown at what the goods cost. Enter the owner's PIN to see them.")).toBeInTheDocument();
    expect(screen.queryByTestId("loss-line")).not.toBeInTheDocument();
  });

  it("records spoilage through the stock book's write-off: an item found, how much, and why", async () => {
    const adjust = vi.fn(async (input: AdjustInput) => ({ productId: input.productId, onHand: "3.500" }));
    const losses = vi.fn(async () => aLossReport());
    renderWithProviders(<LossesScreen />, {
      client: fakeClient({ stock: { adjust }, reports: { losses }, catalog: { products: async () => [labneh, aProduct({ id: "p-misc", nameAr: "متفرقات", openPrice: true })] } }),
      locale: "en",
    });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Record spoilage" }));
    const dialog = await screen.findByRole("dialog", { name: "Record spoilage" });
    await settle();
    expect(within(dialog).getByRole("button", { name: "Write off" })).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText("Item — type its name or scan its barcode"), "متفرقات");
    expect(within(dialog).getByText("No item matches.")).toBeInTheDocument();
    await userEvent.clear(within(dialog).getByLabelText("Item — type its name or scan its barcode"));
    await userEvent.type(within(dialog).getByLabelText("Item — type its name or scan its barcode"), "labneh{Enter}");
    expect(within(dialog).getByTestId("spoilage-item")).toHaveTextContent("Labneh");
    await userEvent.type(within(dialog).getByLabelText("Quantity lost (Kilogram)"), "1.5");
    expect(within(dialog).getByLabelText("Why")).toHaveValue("spoiled");
    await userEvent.selectOptions(within(dialog).getByLabelText("Why"), "expired");
    await userEvent.type(within(dialog).getByLabelText("Note (optional)"), "past its date");
    await userEvent.click(within(dialog).getByRole("button", { name: "Write off" }));
    await settle();
    expect(adjust).toHaveBeenCalledWith({ productId: "p-labneh", direction: "out", quantity: "1.5", reason: "expired", note: "past its date" });
    expect(screen.getByText("Recorded: 1.5 Kilogram of Labneh.")).toBeInTheDocument();
    expect(losses).toHaveBeenCalledTimes(2);
  });
});
