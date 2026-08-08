import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, type RenderResult } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import type { ReactElement, ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { PreferencesProvider } from "@/app/providers/PreferencesProvider";
import { DIRECTION, type Locale } from "@/i18n/messages";
import type { SessionInfo } from "@/lib/wails";
import { ToastProvider, TooltipProvider } from "@/shared/ui";

/** What renderApp returns: the usual queries, plus a user-event driver and the cache. */
export type AppRenderResult = RenderResult & { user: UserEvent; client: QueryClient };

/** A signed-in principal, for tests that need one. */
export const SIGNED_IN: SessionInfo = {
  signedIn: true,
  userId: "u-1",
  username: "nadia",
  displayName: "Nadia Haddad",
  mustChange: false,
  permissions: ["identity.user.view"],
};

export const SIGNED_OUT: SessionInfo = {
  signedIn: false,
  userId: "",
  username: "",
  displayName: "",
  mustChange: false,
  permissions: [],
};

/**
 * Renders a component inside the real provider stack.
 *
 * The REAL providers, not stubs: PreferencesProvider is what supplies `t`, and a test that
 * swapped it for a fake dictionary would stop covering the thing Step 0.11 built it for — that
 * the frontend's active language and the backend's `ui.locale` setting are one fact.
 *
 * A fresh QueryClient per render, so one test's cached answers cannot leak into the next. That
 * is the same failure mode D3 guards against in production, and it is worth not reproducing in
 * the test harness.
 */
export function renderApp(
  ui: ReactElement,
  {
    locale = "en" as Locale,
    client,
  }: { locale?: Locale; client?: QueryClient } = {},
): AppRenderResult {
  document.documentElement.lang = locale;
  document.documentElement.dir = DIRECTION[locale];

  const queryClient =
    client ??
    new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });

  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <ToastProvider>
          <TooltipProvider>
            <MemoryRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
              <PreferencesProvider>{children}</PreferencesProvider>
            </MemoryRouter>
          </TooltipProvider>
        </ToastProvider>
      </QueryClientProvider>
    );
  }

  const result = render(ui, { wrapper: Wrapper });
  return Object.assign(result, { user: userEvent.setup(), client: queryClient });
}
