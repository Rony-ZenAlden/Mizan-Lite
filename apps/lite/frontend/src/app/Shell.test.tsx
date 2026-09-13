import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeClient, renderWithProviders } from "@/api/testing";
import { BindingError } from "@/api/envelope";
import { ROUTES } from "./routes";
import { Shell } from "./Shell";

/** Waits for the routed status screen to finish loading, so no update lands after the test ends. */
async function settled(locale: "ar" | "en") {
  await screen.findByText(locale === "ar" ? "متصل بمحرّك التطبيق" : "Connected to the application engine");
}

describe("Shell", () => {
  it("renders one navigation link per route, labelled in the active language", async () => {
    const client = fakeClient({ settings: { get: async () => ({ locale: "en", shopName: "بقالية المونة", direction: "ltr" }) } });
    renderWithProviders(<Shell />, { client, locale: "en" });
    await settled("en");
    const nav = screen.getByRole("navigation", { name: "Main navigation" });
    expect(nav.querySelectorAll("a")).toHaveLength(ROUTES.length);
    expect(screen.getByRole("link", { name: "Status" })).toBeInTheDocument();
  });

  it("switches to English at once, in the same render, and saves it", async () => {
    const update = vi.fn(async () => ({ locale: "en", shopName: "بقالية المونة", direction: "ltr" }));
    renderWithProviders(<Shell />, { client: fakeClient({ settings: { update } }), locale: "ar" });
    await waitFor(() => expect(document.documentElement).toHaveAttribute("dir", "rtl"));

    await userEvent.click(screen.getByRole("button", { name: "English" }));

    expect(document.documentElement).toHaveAttribute("lang", "en");
    expect(document.documentElement).toHaveAttribute("dir", "ltr");
    expect(update).toHaveBeenCalledWith({ locale: "en" });
    expect(screen.getByRole("button", { name: "English" })).toHaveAttribute("aria-pressed", "true");
    expect(await screen.findByText("Mizan Lite")).toBeInTheDocument();
    await settled("en");
  });

  it("reverts and explains when the language cannot be saved", async () => {
    const update = vi.fn(async () => {
      throw new BindingError({ code: "lite.api.internal", messageKey: "lite.api.internal" });
    });
    renderWithProviders(<Shell />, { client: fakeClient({ settings: { update } }), locale: "ar" });

    await userEvent.click(screen.getByRole("button", { name: "English" }));

    // Back in Arabic, so the screen never claims a language that will not survive a restart.
    await waitFor(() => expect(document.documentElement).toHaveAttribute("dir", "rtl"));
    expect(await screen.findByRole("alert")).toHaveTextContent("حدث خطأ داخلي");
    await settled("ar");
  });

  it("adopts the stored language when the document was served in a different one", async () => {
    const client = fakeClient({ settings: { get: async () => ({ locale: "en", shopName: "بقالية المونة", direction: "ltr" }) } });
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

  it("names each language in its own language, marked with its own lang attribute", async () => {
    const client = fakeClient({ settings: { get: async () => ({ locale: "en", shopName: "بقالية المونة", direction: "ltr" }) } });
    renderWithProviders(<Shell />, { client, locale: "en" });
    await settled("en");
    expect(screen.getByRole("button", { name: "العربية" })).toHaveAttribute("lang", "ar");
    expect(screen.getByRole("button", { name: "English" })).toHaveAttribute("lang", "en");
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
      settings: { get: async () => ({ locale: "en", shopName: "The Pantry", direction: "ltr" }) },
      owner: { status: async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: elevated }), endElevation },
    });
    renderWithProviders(<Shell />, { client, locale: "en" });
    expect(await screen.findByText("Owner mode 1:35")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Lock" }));
    expect(endElevation).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByText(/Owner mode/)).not.toBeInTheDocument());
    await settled("en");
  });

  it("shows no owner indicator outside owner mode", async () => {
    const client = fakeClient({ settings: { get: async () => ({ locale: "en", shopName: "The Pantry", direction: "ltr" }) } });
    renderWithProviders(<Shell />, { client, locale: "en" });
    await settled("en");
    expect(screen.queryByRole("button", { name: "Lock" })).not.toBeInTheDocument();
  });
});
