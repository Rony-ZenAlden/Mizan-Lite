import { render, type RenderResult } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";
import { DIRECTION, type Locale } from "@/i18n/messages";
import { ToastProvider, TooltipProvider } from "@/shared/ui";

/**
 * Renders a component with the providers primitives depend on, in a chosen direction.
 *
 * Direction is a first-class parameter because RTL correctness is verified by rendering in
 * BOTH directions, not by reading the markup (§22.3, DoD 8). Retrofitting RTL across a
 * finished UI is weeks of work, so the gate exists from the first primitive.
 */
export function renderIn(
  ui: ReactElement,
  { locale = "en" as Locale }: { locale?: Locale } = {},
): RenderResult {
  document.documentElement.lang = locale;
  document.documentElement.dir = DIRECTION[locale];

  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <ToastProvider>
        <TooltipProvider>{children}</TooltipProvider>
      </ToastProvider>
    );
  }

  return render(ui, { wrapper: Wrapper });
}

/** Both directions, for tests that must pass identically in each. */
export const BOTH_DIRECTIONS: Array<{ locale: Locale; dir: string }> = [
  { locale: "en", dir: "ltr" },
  { locale: "ar", dir: "rtl" },
];
