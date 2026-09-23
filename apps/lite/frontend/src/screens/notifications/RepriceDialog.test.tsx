import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { RepriceChange } from "@/api/client";
import { BindingError } from "@/api/envelope";
import { aProduct, aRepriceProposal, fakeClient, renderWithProviders } from "@/api/testing";
import { RepriceDialog } from "./RepriceDialog";

const two = () =>
  aRepriceProposal({
    items: [
      { id: "jar", rowVersion: 3, nameAr: "دبس رمان", nameEn: "Pomegranate molasses", currency: "SYP", price: "15000", proposed: "16500", shift: "+10.0" },
      { id: "tea", rowVersion: 7, nameAr: "شاي", nameEn: "Tea", currency: "SYP", price: "30000", proposed: "33000", shift: "+10.0" },
    ],
  });

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}

describe("RepriceDialog — optional, and nothing changes until the owner confirms (2026-09-23)", () => {
  it("applies only what the owner left ticked, at the version the owner saw", async () => {
    const bulkReprice = vi.fn(async (items: RepriceChange[]) => items.map((i) => aProduct({ id: i.id })));
    const onDone = vi.fn();
    renderWithProviders(<RepriceDialog onDone={onDone} onClose={() => {}} />, {
      client: fakeClient({ catalog: { repriceProposal: async () => two(), bulkReprice } }),
      locale: "en",
    });
    expect(await screen.findAllByTestId("reprice-item")).toHaveLength(2);
    await userEvent.click(screen.getByRole("checkbox", { name: "Tea" }));
    await userEvent.click(screen.getByRole("button", { name: "Apply the new prices (1)" }));
    expect(bulkReprice).toHaveBeenCalledWith([{ id: "jar", rowVersion: 3, price: "16500" }]);
    expect(onDone).toHaveBeenCalledWith(1);
  });

  it("sends the price as the shop types it — the new figure of a dual reading, which Go takes back to the books", async () => {
    const bulkReprice = vi.fn(async (items: RepriceChange[]) => items.map((i) => aProduct({ id: i.id })));
    const dual = aRepriceProposal({
      items: [{ id: "jar", rowVersion: 3, nameAr: "دبس رمان", nameEn: "Pomegranate molasses", currency: "SYP", price: "150 (15000)", proposed: "165 (16500)", shift: "+10.0" }],
    });
    renderWithProviders(<RepriceDialog onDone={() => {}} onClose={() => {}} />, {
      client: fakeClient({ catalog: { repriceProposal: async () => dual, bulkReprice } }),
      locale: "en",
    });
    await userEvent.click(await screen.findByRole("button", { name: "Apply the new prices (1)" }));
    expect(bulkReprice).toHaveBeenCalledWith([{ id: "jar", rowVersion: 3, price: "165" }]);
  });

  it("one percentage for every price is Go's to work out, and a percentage Go refuses is explained", async () => {
    const repriceProposal = vi.fn(async (percent: string) => {
      if (percent === "500") throw new BindingError({ code: "lite.catalog.reprice_invalid", messageKey: "lite.catalog.reprice_invalid" });
      return two();
    });
    renderWithProviders(<RepriceDialog onDone={() => {}} onClose={() => {}} />, {
      client: fakeClient({ catalog: { repriceProposal } }),
      locale: "en",
    });
    await screen.findAllByTestId("reprice-item");
    expect(repriceProposal).toHaveBeenLastCalledWith("");
    await userEvent.type(screen.getByLabelText("Or change every price by (%)"), "5");
    await userEvent.click(screen.getByRole("button", { name: "Recalculate" }));
    expect(repriceProposal).toHaveBeenLastCalledWith("5");

    await userEvent.type(screen.getByLabelText("Or change every price by (%)"), "00");
    await userEvent.click(screen.getByRole("button", { name: "Recalculate" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    await settle();
  });

  it("asks for the owner PIN at Apply, and applies once the PIN is given", async () => {
    const bulkReprice = vi
      .fn(async (items: RepriceChange[]) => items.map((i) => aProduct({ id: i.id })))
      .mockImplementationOnce(async () => {
        throw new BindingError({ code: "lite.owner.required", messageKey: "lite.owner.required" });
      });
    const onDone = vi.fn();
    renderWithProviders(<RepriceDialog onDone={onDone} onClose={() => {}} />, {
      client: fakeClient({ catalog: { repriceProposal: async () => two(), bulkReprice } }),
      locale: "en",
    });
    await userEvent.click(await screen.findByRole("button", { name: "Apply the new prices (2)" }));
    await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await settle();
    expect(bulkReprice).toHaveBeenCalledTimes(2);
    expect(onDone).toHaveBeenCalledWith(2);
  });

  it("says so when no price is behind the rate, and has nothing to apply", async () => {
    renderWithProviders(<RepriceDialog onDone={() => {}} onClose={() => {}} />, {
      client: fakeClient({ catalog: { repriceProposal: async () => aRepriceProposal({ items: [] }) } }),
      locale: "en",
    });
    expect(await screen.findByTestId("reprice-none")).toHaveTextContent("No price is behind the exchange rate.");
    expect(within(screen.getByTestId("reprice-dialog")).getByRole("button", { name: "Apply the new prices (0)" })).toBeDisabled();
  });
});
