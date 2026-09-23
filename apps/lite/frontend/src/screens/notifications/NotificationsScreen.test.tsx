import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { AlertsState, Notification, RepriceChange } from "@/api/client";
import { anAlerts, aProduct, fakeClient, renderWithProviders } from "@/api/testing";
import { NotificationProvider, SEEN_KEY } from "@/alerts/NotificationProvider";
import { NotificationsScreen } from "./NotificationsScreen";

const note = (key: string, kind: string, extra: Partial<Notification> = {}): Notification => ({
  key,
  fingerprint: "1",
  kind,
  severity: "warning",
  productId: "",
  nameAr: "",
  nameEn: "",
  count: 0,
  ...extra,
});

/** A shop with one of everything: a stale backup, two prices behind the rate (one at a loss), a month of capital, a low product. */
const everything = (): AlertsState =>
  anAlerts({
    notifications: [
      note("backup_old", "backup_old"),
      note("stale_prices", "stale_prices", { count: 2 }),
      note("capital", "capital"),
      note("low_stock:oil", "low_stock", { productId: "oil", nameAr: "زيت زيتون", nameEn: "Olive oil" }),
    ],
    lowStock: [{ productId: "oil", nameAr: "زيت زيتون", nameEn: "Olive oil", unitCode: "l", onHand: "2", level: "5" }],
    stale: [
      { productId: "jar", nameAr: "دبس رمان", nameEn: "Pomegranate molasses", currency: "SYP", price: "15000", shift: "+10.0", proposed: "16500", replacement: "16500", loss: true },
      { productId: "tea", nameAr: "شاي", nameEn: "Tea", currency: "SYP", price: "30000", shift: "+10.0", proposed: "33000", replacement: "", loss: false },
    ],
    lossCount: 1,
    hasCapital: true,
    capital: {
      thenDate: "2026-08-24",
      nowDate: "2026-09-23",
      thenUsd: "1100.00",
      nowUsd: "1045.45",
      change: "-5.0",
      thenRate: "15000",
      nowRate: "16500",
      rateShift: "+10.0",
      depreciation: "90.91",
      rising: false,
    },
    historyDays: 30,
    backup: "old",
  });

function renderCentre(client = fakeClient({ alerts: { current: async () => everything() } })) {
  return renderWithProviders(
    <NotificationProvider>
      <NotificationsScreen />
    </NotificationProvider>,
    { client, locale: "en", route: "/notifications" },
  );
}

async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30));
  });
}

describe("NotificationsScreen — everything the shop should know, with what to do about it (2026-09-23)", () => {
  it("shows each kind with the step that deals with it", async () => {
    renderCentre();
    const backup = await screen.findByTestId("alerts-backup");
    expect(backup).toHaveTextContent("The last backup is more than a day and a half old.");
    expect(within(backup).getByRole("link", { name: "Open Backups" })).toHaveAttribute("href", "/backups");

    const stale = screen.getByTestId("alerts-stale");
    expect(stale).toHaveTextContent("Products priced at an exchange rate that has since moved: 2.");
    expect(stale).toHaveTextContent("Now selling below what they cost to replace: 1.");
    const [jar, tea] = within(stale).getAllByTestId("alerts-stale-item");
    expect(jar).toHaveTextContent("Pomegranate molasses");
    expect(jar).toHaveTextContent("Sold at a loss");
    expect(jar).toHaveTextContent("16,500 SYP");
    expect(tea).not.toHaveTextContent("Sold at a loss");
    expect(tea).toHaveTextContent("—");

    expect(screen.getByTestId("alerts-capital-change")).toHaveTextContent("-5.0%");
    expect(screen.getByTestId("alerts-depreciation")).toHaveTextContent("90.91 USD");
    expect(screen.getByTestId("alerts-capital")).toHaveReadableText("at 16500 SYP to the dollar");

    const low = screen.getByTestId("alerts-low");
    expect(within(low).getByTestId("alerts-low-item")).toHaveTextContent("Olive oil");
    expect(within(low).getByRole("link", { name: "Open Stock" })).toHaveAttribute("href", "/stock");
    await settle();
  });

  it("opening it marks everything read, and keeps a New mark on what was unread when it opened", async () => {
    renderCentre();
    await screen.findByTestId("alerts-backup");
    await waitFor(() =>
      expect(JSON.parse(window.localStorage.getItem(SEEN_KEY) ?? "{}")).toEqual({
        backup_old: "1",
        stale_prices: "1",
        capital: "1",
        "low_stock:oil": "1",
      }),
    );
    expect(screen.getAllByTestId("alerts-new")).toHaveLength(4);
    await settle();
  });

  it("says plainly when nothing needs attention, and how long until the capital can be compared", async () => {
    renderCentre(fakeClient({ alerts: { current: async () => anAlerts({ historyDays: 3 }) } }));
    expect(await screen.findByTestId("alerts-nothing")).toHaveTextContent("Nothing needs attention right now.");
    expect(screen.getByTestId("alerts-capital-collecting")).toHaveTextContent("Collecting history: 3 of 7 days.");
    await settle();
  });

  it("outside owner mode, the owner's figures wait for the PIN — and then are shown", async () => {
    // Go withholds the figures until owner mode is on.
    let owner = false;
    const current = async () => (owner ? everything() : anAlerts({ ownerHidden: true, historyDays: 30 }));
    const elevate = async () => {
      owner = true;
      return { setUp: true, lockedSeconds: 0, elevatedSeconds: 120 };
    };
    renderCentre(fakeClient({ alerts: { current }, owner: { elevate } }));
    const hidden = await screen.findByTestId("alerts-owner-hidden");
    // Hidden is not the same as not collected yet: the centre does not claim it is still collecting.
    expect(screen.queryByTestId("alerts-capital-collecting")).not.toBeInTheDocument();
    await userEvent.click(within(hidden).getByRole("button", { name: "Show owner figures" }));
    await userEvent.type(await screen.findByLabelText("Owner PIN"), "246813");
    await userEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(await screen.findByTestId("alerts-depreciation")).toHaveTextContent("90.91 USD");
    await settle();
  });

  it("re-pricing is a proposal: closing it changes nothing", async () => {
    const bulkReprice = vi.fn(async (items: RepriceChange[]) => items.map((i) => aProduct({ id: i.id })));
    renderCentre(fakeClient({ alerts: { current: async () => everything() }, catalog: { bulkReprice } }));
    await userEvent.click(await screen.findByRole("button", { name: "Review re-pricing" }));
    const dialog = await screen.findByRole("dialog", { name: "Re-price after the exchange rate moved" });
    await within(dialog).findByTestId("reprice-item");
    await userEvent.click(within(dialog).getByRole("button", { name: "Not now" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(bulkReprice).not.toHaveBeenCalled();
    await settle();
  });

  it("applied, the new prices are confirmed and the centre asks Go again", async () => {
    const current = vi.fn(async () => everything());
    renderCentre(fakeClient({ alerts: { current } }));
    await userEvent.click(await screen.findByRole("button", { name: "Review re-pricing" }));
    const dialog = await screen.findByRole("dialog", { name: "Re-price after the exchange rate moved" });
    await within(dialog).findByTestId("reprice-item");
    const before = current.mock.calls.length;
    await userEvent.click(within(dialog).getByRole("button", { name: "Apply the new prices (1)" }));
    expect(await screen.findByTestId("reprice-done")).toHaveTextContent("Prices updated: 1.");
    await waitFor(() => expect(current.mock.calls.length).toBeGreaterThan(before));
    await settle();
  });
});
