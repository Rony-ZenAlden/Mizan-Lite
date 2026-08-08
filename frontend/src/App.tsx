import { useEffect, useState } from "react";
import { HashRouter } from "react-router-dom";
import { BootGate } from "@/app/boot/BootGate";
import { AuthGate } from "@/app/gates/AuthGate";
import { SetupGate } from "@/app/gates/SetupGate";
import { ErrorBoundary } from "@/app/providers/ErrorBoundary";
import { PreferencesProvider } from "@/app/providers/PreferencesProvider";
import { QueryProvider } from "@/app/providers/QueryProvider";
import { AppShell } from "@/app/shell/AppShell";
import { DIRECTION, initialLocale, type Locale } from "@/i18n/messages";
import { ToastProvider, TooltipProvider } from "@/shared/ui";

/**
 * The provider and gate stack.
 *
 *   ErrorBoundary → Query → Toast → Tooltip → Router
 *     → BootGate → Preferences → SetupGate → AuthGate → AppShell
 *
 * ErrorBoundary is outermost so a throw from anywhere below renders a translated error state
 * rather than a blank window. Toast sits above Preferences because Preferences reports a failed
 * write through a toast — a provider cannot use a context declared beneath it.
 *
 * The three GATES are the §FE.1 stack, and their order is the design (Step 1.10, D1):
 *
 *   BootGate    graph ready?      → boot screen
 *   SetupGate   company exists?   → wizard
 *   AuthGate    session valid?    → login
 *
 * Each mounts NOTHING below it until satisfied. That is a mount boundary rather than a
 * redirect: mounting the shell and unmounting it a tick later would flash a UI the user is not
 * entitled to see and fire a burst of calls that all fail.
 *
 * BootGate is above Preferences deliberately: reading preferences requires the object graph,
 * and the graph does not exist until boot completes. SetupGate is BELOW Preferences, because
 * the wizard's first screen is a language picker and it needs the preference machinery to make
 * that choice take effect immediately (§C.2 step 1).
 *
 * None of the gates is a security boundary. §14.3 is explicit that the backend is the sole
 * enforcement point, and Step 1.5 made it structural there. These decide what to SHOW.
 */
/**
 * Opted into v7 behaviour now rather than on the upgrade.
 *
 * Both flags change how navigation resolves, and discovering that on a version bump — in an
 * application meant to be maintained for a decade — is strictly worse than discovering it here,
 * with two routes and no users. It also keeps the warnings out of every test run, where noise
 * that is always present is noise nobody reads.
 */
const ROUTER_FUTURE = { v7_startTransition: true, v7_relativeSplatPath: true } as const;

export default function App() {
  return (
    <ErrorBoundary>
      <QueryProvider>
        <ToastProvider>
          <TooltipProvider>
            {/* HashRouter, not BrowserRouter: this is a webview loading from a file, so there
                is no server to answer a deep path on reload. Hash routing is also honest about
                what routing means in a desktop app — in-window navigation, not URLs anyone
                types. */}
            <HashRouter future={ROUTER_FUTURE}>
              <BootShell />
            </HashRouter>
          </TooltipProvider>
        </ToastProvider>
      </QueryProvider>
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
        <SetupGate>
          <AuthGate>
            <AppShell />
          </AuthGate>
        </SetupGate>
      </PreferencesProvider>
    </BootGate>
  );
}
