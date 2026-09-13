import { act, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { fakeClient, renderWithProviders } from "@/api/testing";
import type { Locale } from "@/i18n/messages";
import { ROUTES } from "./routes";
import { Shell } from "./Shell";

// Every routed screen mounts in BOTH directions. Mizan 10.3 D2: this proves a screen mounts and lays
// out its document direction, not that it looks right — L0's Definition of Done also requires the
// packaged app to be opened and looked at, in both languages.
const cases: [Locale, "rtl" | "ltr"][] = [
  ["ar", "rtl"],
  ["en", "ltr"],
];

describe.each(cases)("every route in %s", (locale, direction) => {
  it.each(ROUTES.map((r) => [r.path, r] as const))("mounts %s", async (path) => {
    // The stored language matches the one under test; otherwise the shell correctly adopts the
    // stored one and the case would measure that instead.
    const client = fakeClient({ settings: { get: async () => ({ locale, shopName: "بقالية المونة", direction }) } });
    renderWithProviders(<Shell />, { client, locale, route: path });
    await waitFor(() => expect(document.documentElement).toHaveAttribute("dir", direction));
    expect(document.documentElement).toHaveAttribute("lang", locale);
    expect(await screen.findByRole("heading", { level: 2 })).toBeInTheDocument();
    // Let every screen's own loading — including a debounced search — finish inside the test, so an update
    // cannot land after it (which the console gate would fail).
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 300));
    });
  });
});
