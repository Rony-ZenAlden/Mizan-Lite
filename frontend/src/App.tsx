import { useEffect, useState } from "react";
import { BootGate } from "@/app/boot/BootGate";
import { ErrorBoundary } from "@/app/providers/ErrorBoundary";
import { PreferencesProvider } from "@/app/providers/PreferencesProvider";
import { AppShell } from "@/app/shell/AppShell";
import { DIRECTION, initialLocale, type Locale } from "@/i18n/messages";
import { ToastProvider, TooltipProvider } from "@/shared/ui";

/**
 * The provider stack.
 *
 *   ErrorBoundary → Toast → Tooltip → BootGate → Preferences → AppShell
 *
 * ErrorBoundary is outermost so a throw from anywhere below renders a translated error state
 * rather than a blank window. Toast sits above Preferences because Preferences reports a failed
 * write through a toast — a provider cannot use a context declared beneath it.
 *
 * BootGate sits ABOVE Preferences deliberately: reading preferences requires the object graph,
 * and the graph does not exist until boot completes. Mounting them in the other order would
 * mean every launch begins with a guaranteed not-ready error.
 */
export default function App() {
  return (
    <ErrorBoundary>
      <ToastProvider>
        <TooltipProvider>
          <BootShell />
        </TooltipProvider>
      </ToastProvider>
    </ErrorBoundary>
  );
}

function BootShell() {
  // Pre-settings locale, from the OS. PreferencesProvider takes over once boot succeeds.
  const [locale] = useState<Locale>(initialLocale);

  // The boot screen needs lang/dir before PreferencesProvider exists, or a first launch in
  // Arabic renders the migration progress left-to-right.
  useEffect(() => {
    const root = document.documentElement;
    if (!root.lang) {
      root.lang = locale;
      root.dir = DIRECTION[locale] ?? "ltr";
    }
  }, [locale]);

  return (
    <BootGate locale={locale}>
      <PreferencesProvider>
        <AppShell />
      </PreferencesProvider>
    </BootGate>
  );
}
