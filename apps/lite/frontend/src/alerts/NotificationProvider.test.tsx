import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AlertsState, Notification } from "@/api/client";
import { anAlerts, fakeClient, renderWithProviders } from "@/api/testing";
import { Bell } from "./Bell";
import { NotificationProvider, SEEN_KEY, TOAST_MS, useNotifications } from "./NotificationProvider";
import { Toasts } from "./Toasts";

const low = (id: string, name: string, fingerprint = "low"): Notification => ({
  key: `low_stock:${id}`,
  fingerprint,
  kind: "low_stock",
  severity: "warning",
  productId: id,
  nameAr: name,
  nameEn: name,
  count: 0,
});

/** The part of a screen that asks again — the till after a sale. */
function Again() {
  const { refresh } = useNotifications();
  return (
    <button type="button" onClick={() => void refresh()}>
      again
    </button>
  );
}

function renderBell(current: () => Promise<AlertsState>) {
  return renderWithProviders(
    <NotificationProvider>
      <Bell />
      <Again />
      <Toasts />
    </NotificationProvider>,
    { client: fakeClient({ alerts: { current } }), locale: "en" },
  );
}

const seenInStorage = () => JSON.parse(window.localStorage.getItem(SEEN_KEY) ?? "{}") as Record<string, string>;

afterEach(() => {
  vi.useRealTimers();
});

describe("NotificationProvider — one list, counted, announced once (2026-09-23)", () => {
  it("counts what is unread, and the first look of a session is one summary rather than a toast each", async () => {
    renderBell(async () => anAlerts({ notifications: [low("oil", "Olive oil"), low("jam", "Apricot jam")] }));
    expect(await screen.findByTestId("bell-count")).toHaveTextContent("2");
    const toasts = await screen.findAllByTestId("toast");
    expect(toasts).toHaveLength(1);
    expect(toasts[0]).toHaveTextContent("Notifications that need your attention: 2.");
  });

  it("a notification already read is not counted or announced again — until it changes", async () => {
    window.localStorage.setItem(SEEN_KEY, JSON.stringify({ "low_stock:oil": "low" }));
    let fingerprint = "low";
    const current = vi.fn(async () => anAlerts({ notifications: [low("oil", "Olive oil", fingerprint)] }));
    renderBell(current);
    await waitFor(() => expect(current).toHaveBeenCalled());
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20));
    });
    expect(screen.queryByTestId("bell-count")).not.toBeInTheDocument();
    expect(screen.queryByTestId("toast")).not.toBeInTheDocument();

    // Worse than before: Go gives it a new fingerprint, and it is news again.
    fingerprint = "lower";
    await userEvent.click(screen.getByRole("button", { name: "again" }));
    expect(await screen.findByTestId("bell-count")).toHaveTextContent("1");
    expect(await screen.findByTestId("toast")).toHaveTextContent("Olive oil is running low.");
  });

  it("after the first look, each new notification has a toast of its own", async () => {
    let list = [low("oil", "Olive oil")];
    renderBell(async () => anAlerts({ notifications: list }));
    expect(await screen.findByTestId("toast")).toHaveTextContent("Olive oil is running low.");
    await userEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByTestId("toast")).not.toBeInTheDocument();

    list = [low("oil", "Olive oil"), low("jam", "Apricot jam")];
    await userEvent.click(screen.getByRole("button", { name: "again" }));
    const toasts = await screen.findAllByTestId("toast");
    expect(toasts).toHaveLength(1);
    expect(toasts[0]).toHaveTextContent("Apricot jam is running low.");
    expect(screen.getByTestId("bell-count")).toHaveTextContent("2");
  });

  it("a toast leaves on its own, never needing to be dismissed at a busy counter", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    renderBell(async () => anAlerts({ notifications: [low("oil", "Olive oil")] }));
    expect(await screen.findByTestId("toast")).toBeInTheDocument();
    await act(async () => {
      vi.advanceTimersByTime(TOAST_MS + 50);
    });
    expect(screen.queryByTestId("toast")).not.toBeInTheDocument();
    // The bell still has it: a toast leaving is not the same as having read it.
    expect(screen.getByTestId("bell-count")).toHaveTextContent("1");
  });

  it("forgets what was read of a notification that went away, so its coming back is news", async () => {
    window.localStorage.setItem(SEEN_KEY, JSON.stringify({ "low_stock:oil": "low", "low_stock:jam": "low" }));
    const current = vi.fn(async () => anAlerts({ notifications: [low("jam", "Apricot jam")] }));
    renderBell(current);
    await waitFor(() => expect(seenInStorage()).toEqual({ "low_stock:jam": "low" }));
  });

  it("does not forget what was read of the owner's figures while they are merely hidden", async () => {
    window.localStorage.setItem(SEEN_KEY, JSON.stringify({ capital: "-1" }));
    const current = vi.fn(async () => anAlerts({ ownerHidden: true }));
    renderBell(current);
    await waitFor(() => expect(current).toHaveBeenCalled());
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20));
    });
    expect(seenInStorage()).toEqual({ capital: "-1" });
  });

  it("the bell says how many are unread to a screen reader, and links to the centre", async () => {
    renderBell(async () => anAlerts({ notifications: [low("oil", "Olive oil")] }));
    const bell = await screen.findByRole("link", { name: "Open notifications — 1 unread" });
    expect(bell).toHaveAttribute("href", "/notifications");
  });
});
