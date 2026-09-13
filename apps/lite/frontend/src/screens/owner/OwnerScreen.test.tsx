import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import { OwnerScreen, HISTORY_LIMIT } from "./OwnerScreen";

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
}

describe("OwnerScreen", () => {
  it("shows the history, translated, with the act and its before and after", async () => {
    const events = vi.fn(async () => [
      { id: "1", occurredAt: "2026-09-13T10:15:00.000Z", kind: "guarded_act", action: "catalog.price.change", subjectId: "p", before: "USD 3.25", after: "USD 3.50" },
      { id: "2", occurredAt: "2026-09-13T10:14:00.000Z", kind: "elevation_failed", action: "", subjectId: "", before: "", after: "" },
    ]);
    renderWithProviders(<OwnerScreen />, { client: fakeClient({ owner: { events } }), locale: "ar" });
    await settle();

    expect(events).toHaveBeenCalledWith(HISTORY_LIMIT);
    expect(screen.getByText("إجراء محمي")).toBeInTheDocument();
    expect(screen.getByText("تغيير سعر · USD 3.25 → USD 3.50")).toBeInTheDocument();
    expect(screen.getByText("محاولة خاطئة")).toBeInTheDocument();
  });

  it("says when there is no history", async () => {
    renderWithProviders(<OwnerScreen />, { locale: "en" });
    await settle();
    expect(screen.getByText("Nothing recorded yet.")).toBeInTheDocument();
  });

  it("changes the PIN, refusing a mismatch on the screen", async () => {
    const changePin = vi.fn(async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: 0 }));
    renderWithProviders(<OwnerScreen />, { client: fakeClient({ owner: { changePin } }), locale: "en" });
    await settle();

    await userEvent.type(screen.getByLabelText("Current PIN"), "246813");
    await userEvent.type(screen.getByLabelText("New PIN"), "581937");
    await userEvent.type(screen.getByLabelText("New PIN again"), "581930");
    await userEvent.click(screen.getByRole("button", { name: "Change PIN" }));
    expect(await screen.findByText("The two PINs do not match.")).toBeInTheDocument();
    expect(changePin).not.toHaveBeenCalled();

    await userEvent.clear(screen.getByLabelText("New PIN again"));
    await userEvent.type(screen.getByLabelText("New PIN again"), "581937");
    await userEvent.click(screen.getByRole("button", { name: "Change PIN" }));
    await settle();
    expect(changePin).toHaveBeenCalledWith({ currentPin: "246813", newPin: "581937" });
    expect(screen.getByText("The owner PIN was changed.")).toBeInTheDocument();
    expect(screen.getByLabelText("Current PIN")).toHaveValue("");
  });

  it("shows why a change was refused", async () => {
    const changePin = vi.fn(async () => {
      throw new BindingError({ code: "lite.owner.wrong_pin", messageKey: "lite.owner.wrong_pin", params: { remaining: "2" } });
    });
    renderWithProviders(<OwnerScreen />, { client: fakeClient({ owner: { changePin } }), locale: "en" });
    await settle();
    await userEvent.type(screen.getByLabelText("Current PIN"), "739251");
    await userEvent.type(screen.getByLabelText("New PIN"), "581937");
    await userEvent.type(screen.getByLabelText("New PIN again"), "581937");
    await userEvent.click(screen.getByRole("button", { name: "Change PIN" }));
    expect(await screen.findByText("Wrong PIN. Attempts left before a wait: 2.")).toBeInTheDocument();
  });

  it("enters owner mode from the screen", async () => {
    const elevate = vi.fn(async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: 120 }));
    renderWithProviders(<OwnerScreen />, { client: fakeClient({ owner: { elevate } }), locale: "en" });
    await settle();
    await userEvent.click(screen.getByRole("button", { name: "Enter owner mode" }));
    await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(await screen.findByText("Owner mode is on — 2:00 left")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Enter owner mode" })).not.toBeInTheDocument();
  });

  it("will not offer owner mode while entry is locked", async () => {
    renderWithProviders(<OwnerScreen />, {
      client: fakeClient({ owner: { status: async () => ({ setUp: true, lockedSeconds: 90, elevatedSeconds: 0 }) } }),
      locale: "en",
    });
    await settle();
    expect(screen.getByText("Entry is locked for 1:30")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Enter owner mode" })).toBeDisabled();
  });
});
