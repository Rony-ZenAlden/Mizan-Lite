import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { aRate, aSettings, aShop, fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import { ROUTES } from "./routes";
import { Shell } from "./Shell";

/** Waits for the Till — the screen the counter lands on — to finish loading, so no update lands after the test ends. */
async function settled(locale: "ar" | "en") {
  await screen.findByLabelText(locale === "ar" ? "امسح أو ابحث" : "Scan or search");
  await new Promise((resolve) => setTimeout(resolve, 20));
}

describe("Shell", () => {
  it("renders one navigation link per route, labelled in the active language", async () => {
    const client = fakeClient({ settings: { get: async () => (aSettings({ locale: "en", direction: "ltr" })) } });
    renderWithProviders(<Shell />, { client, locale: "en" });
    await settled("en");
    const nav = screen.getByRole("navigation", { name: "Main navigation" });
    expect(nav.querySelectorAll("a")).toHaveLength(ROUTES.length);
    expect(screen.getByRole("link", { name: "About" })).toHaveAttribute("href", "/about");
    expect(screen.getByRole("link", { name: "Till" })).toHaveAttribute("href", "/");
  });

  // The owner's request of 2026-09-17: the header carries the shop, the rate and owner mode, and nothing else. The
  // language moved into Settings > Language and general preferences, where it is set once rather than sat beside the
  // till being pressed by accident.
  it("offers no language control in the header", async () => {
    renderWithProviders(<Shell />, { client: fakeClient(), locale: "ar" });
    await settled("ar");
    expect(screen.queryByRole("button", { name: "العربية" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "English" })).not.toBeInTheDocument();
    expect(screen.queryByRole("group", { name: "اللغة" })).not.toBeInTheDocument();
  });

  it("still explains a language that could not be saved, wherever it was set from", async () => {
    const update = vi.fn(async () => {
      throw new BindingError({ code: "lite.api.internal", messageKey: "lite.api.internal" });
    });
    renderWithProviders(<Shell />, { client: fakeClient({ settings: { update } }), locale: "ar" });
    await settled("ar");

    await userEvent.click(screen.getByRole("link", { name: "الإعدادات" }));
    await userEvent.selectOptions(await screen.findByLabelText("اللغة"), "en");

    // Back in Arabic, so the screen never claims a language that will not survive a restart, and the header says why.
    await waitFor(() => expect(document.documentElement).toHaveAttribute("dir", "rtl"));
    expect(await screen.findByRole("alert")).toHaveTextContent("حدث خطأ داخلي");
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 30));
    });
  });

  it("adopts the stored language when the document was served in a different one", async () => {
    const client = fakeClient({ settings: { get: async () => (aSettings({ locale: "en", direction: "ltr" })) } });
    renderWithProviders(<Shell />, { client, locale: "ar" });
    await waitFor(() => expect(document.documentElement).toHaveAttribute("lang", "en"));
    expect(document.documentElement).toHaveAttribute("dir", "ltr");
    await settled("en");
  });

  it("keeps the current language if the stored one cannot be read", async () => {
    const client = fakeClient({
      settings: {
        get: async () => {
          throw new BindingError({ code: "database.internal", messageKey: "database.internal" });
        },
      },
    });
    renderWithProviders(<Shell />, { client, locale: "ar" });
    await settled("ar");
    expect(document.documentElement).toHaveAttribute("lang", "ar");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

});

describe("Shell header", () => {
  it("names the shop, with the product name beneath it", async () => {
    renderWithProviders(<Shell />, { locale: "ar" });
    expect(await screen.findByText("بقالية المونة")).toBeInTheDocument();
    expect(screen.getByText("ميزان لايت")).toBeInTheDocument();
    await settled("ar");
  });

  it("shows owner mode with its time left, and Lock ends it", async () => {
    // Go's side, remembered: once locked, a re-read says so. The shell re-reads when a countdown stops.
    let elevated = 95;
    const endElevation = vi.fn(async () => {
      elevated = 0;
      return { setUp: true, lockedSeconds: 0, elevatedSeconds: 0 };
    });
    const client = fakeClient({
      settings: { get: async () => (aSettings({ locale: "en", shopName: "The Pantry", direction: "ltr" })) },
      owner: { status: async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: elevated }), endElevation },
    });
    renderWithProviders(<Shell />, { client, locale: "en" });
    expect(await screen.findByText("Owner mode 1:35")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Lock" }));
    expect(endElevation).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByText(/Owner mode/)).not.toBeInTheDocument());
    await settled("en");
  });

  it("heads the screen with the shop's own logo when it has one, and with its name alone when not (0.10.1)", async () => {
    const client = fakeClient({ settings: { shop: async () => aShop({ name: "الكردي", logo: "iVBORw0KGgo=", logoWidth: 800, logoHeight: 320 }) } });
    renderWithProviders(<Shell />, { client, locale: "ar" });
    const logo = await screen.findByTestId("header-logo");
    expect(logo).toHaveAttribute("src", "data:image/png;base64,iVBORw0KGgo=");
    expect(screen.getByTestId("header-shop-name")).toHaveTextContent("الكردي");
    await settled("ar");
  });

  it("shows no logo for a shop that has none", async () => {
    renderWithProviders(<Shell />, { locale: "ar" });
    expect(await screen.findByText("بقالية المونة")).toBeInTheDocument();
    expect(screen.queryByTestId("header-logo")).not.toBeInTheDocument();
    await settled("ar");
  });

  it("a logo saved in Settings heads the screen at once, without a restart", async () => {
    const client = fakeClient({ settings: { get: async () => aSettings({ locale: "en", direction: "ltr" }) } });
    renderWithProviders(<Shell />, { client, locale: "en", route: "/settings" });
    await screen.findByTestId("store-logo-choose");
    expect(screen.queryByTestId("header-logo")).not.toBeInTheDocument();
    await userEvent.click(screen.getByTestId("store-logo-choose"));
    expect(await screen.findByTestId("header-logo")).toHaveAttribute("src", "data:image/png;base64,iVBORw0KGgo=");
  });

  it("shows no owner indicator outside owner mode", async () => {
    const client = fakeClient({ settings: { get: async () => (aSettings({ locale: "en", shopName: "The Pantry", direction: "ltr" })) } });
    renderWithProviders(<Shell />, { client, locale: "en" });
    await settled("en");
    expect(screen.queryByRole("button", { name: "Lock" })).not.toBeInTheDocument();
  });
});

describe("Shell — the exchange rate in the header", () => {
  it("shows the rate and its age to everyone, linking to the rate screen", async () => {
    renderWithProviders(<Shell />, { client: fakeClient({ settings: { get: async () => (aSettings({ locale: "en", shopName: "المونة", direction: "ltr" })) } }), locale: "en" });
    await settled("en");
    const chip = await screen.findByTestId("header-rate");
    expect(chip).toHaveTextContent("1 USD = 15,000 SYP · 3 hours ago");
    expect(chip).toHaveAttribute("href", "/rates");
  });

  it("marks a rate not updated today, and says when there is none", async () => {
    const stale = fakeClient({
      settings: { get: async () => (aSettings({ locale: "en", shopName: "المونة", direction: "ltr" })) },
      fx: { current: async () => aRate({ stale: true }) },
    });
    const { unmount } = renderWithProviders(<Shell />, { client: stale, locale: "en" });
    await settled("en");
    expect(await screen.findByTestId("header-rate")).toHaveTextContent("not updated today");
    unmount();

    const none = fakeClient({
      settings: { get: async () => (aSettings({ locale: "en", shopName: "المونة", direction: "ltr" })) },
      fx: { current: async () => aRate({ set: false, rate: "" }) },
    });
    renderWithProviders(<Shell />, { client: none, locale: "en" });
    await settled("en");
    expect(await screen.findByTestId("header-rate")).toHaveTextContent("No exchange rate");
  });
});

describe("Shell — one notification engine (2026-09-23)", () => {
  const noBackup = {
    key: "backup_none",
    fingerprint: "2026-09-23",
    kind: "backup_none",
    severity: "warning",
    productId: "",
    nameAr: "",
    nameEn: "",
    count: 0,
  };

  // L8 A-L8.3 put the backups' state on every screen as a red banner; the owner removed the USB part of it in 0.9.8 for
  // standing across the counter. What is left now comes through the engine: the bell counts it, a toast says it once,
  // and it waits in the notification centre — the counter itself is never covered.
  it("tells the counter about a missing backup through the bell and a toast, never a banner", async () => {
    const { anAlerts } = await import("@/api/testing");
    const health = vi.fn(async () => ({ version: "0.9.9", schemaVersion: 11, platform: "darwin", dataDir: "/d" }));
    const current = vi.fn(async () => anAlerts({ notifications: [noBackup], backup: "none" }));
    renderWithProviders(<Shell />, {
      client: fakeClient({ app: { health }, alerts: { current }, settings: { get: async () => aSettings({ locale: "en", shopName: "x", direction: "ltr" }) } }),
      locale: "en",
    });
    expect(await screen.findByTestId("bell-count")).toHaveTextContent("1");
    expect(screen.getByTestId("bell")).toHaveAccessibleName("Open notifications — 1 unread");
    expect(await screen.findByTestId("toast")).toHaveTextContent("There is no backup yet");
    expect(screen.queryByTestId("backup-warning")).not.toBeInTheDocument();
    expect(health).toHaveBeenCalledTimes(1);
    await settled("en");
  });

  it("opens the notification centre from the bell, which then has nothing unread", async () => {
    const { anAlerts } = await import("@/api/testing");
    const current = vi.fn(async () => anAlerts({ notifications: [noBackup], backup: "none" }));
    renderWithProviders(<Shell />, {
      client: fakeClient({ alerts: { current }, settings: { get: async () => aSettings({ locale: "en", shopName: "x", direction: "ltr" }) } }),
      locale: "en",
    });
    await settled("en");
    await userEvent.click(await screen.findByTestId("bell"));
    expect(await screen.findByTestId("alerts-backup")).toHaveTextContent("There is no backup yet");
    await waitFor(() => expect(screen.queryByTestId("bell-count")).not.toBeInTheDocument());
    expect(screen.getByTestId("alerts-new")).toBeInTheDocument();
  });

  it("asks the engine again on every change of screen", async () => {
    const { anAlerts } = await import("@/api/testing");
    const current = vi.fn(async () => anAlerts());
    renderWithProviders(<Shell />, {
      client: fakeClient({ alerts: { current }, settings: { get: async () => aSettings({ locale: "en", shopName: "x", direction: "ltr" }) } }),
      locale: "en",
    });
    await settled("en");
    const before = current.mock.calls.length;
    await userEvent.click(screen.getByRole("link", { name: "Stock" }));
    await waitFor(() => expect(current.mock.calls.length).toBeGreaterThan(before));
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 30));
    });
  });
});
