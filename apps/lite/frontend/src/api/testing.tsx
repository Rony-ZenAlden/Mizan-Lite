// TEST-ONLY. A fake client and a render helper.
//
// Nothing outside a test may import this file. Two gates hold that: a source gate (no non-test file
// imports it) and G5, which fails if the sentinel below appears anywhere in the built bundle.
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter } from "react-router-dom";
import { ClientProvider } from "./ClientContext";
import type { Client } from "./client";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { Locale } from "@/i18n/messages";
import { FAKE_CLIENT_SENTINEL } from "./sentinel";

type Overrides = { [G in keyof Client]?: Partial<Client[G]> };

/** A client answering as a healthy, started application, with any method replaceable. */
export function fakeClient(overrides: Overrides = {}): Client {
  const base: Client = {
    app: {
      bootStatus: async () => ({ state: "ready", phase: "done", current: 0, total: 0 }),
      health: async () => ({ version: "1.0.0-test", schemaVersion: 1, platform: "darwin", dataDir: `/data/${FAKE_CLIENT_SENTINEL}` }),
    },
    settings: {
      get: async () => ({ locale: "ar", direction: "rtl" }),
      update: async (input) => ({
        locale: input.locale ?? "ar",
        direction: input.locale === "en" ? "ltr" : "rtl",
      }),
    },
  };
  return {
    app: { ...base.app, ...overrides.app },
    settings: { ...base.settings, ...overrides.settings },
  };
}

/** Renders inside the providers the application has, in a chosen locale and route. */
export function renderWithProviders(
  ui: ReactElement,
  { client = fakeClient(), locale = "ar", route = "/" }: { client?: Client; locale?: Locale; route?: string } = {},
) {
  return render(
    <ClientProvider client={client}>
      <LocaleProvider initial={locale}>
        <MemoryRouter initialEntries={[route]} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
          {ui}
        </MemoryRouter>
      </LocaleProvider>
    </ClientProvider>,
  );
}
