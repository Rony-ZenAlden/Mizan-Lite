import { HashRouter } from "react-router-dom";
import { ClientProvider } from "@/api/ClientContext";
import type { Client } from "@/api/client";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { OwnerProvider } from "@/owner/OwnerProvider";
import { Boot } from "./Boot";
import { ErrorBoundary } from "./ErrorBoundary";
import { FirstRunGate } from "./FirstRun";
import { Shell } from "./Shell";

/**
 * The whole tree. HashRouter, not BrowserRouter: the webview loads a document from the embedded
 * asset server, and a path-based route would 404 on reload (Mizan 10.9).
 */
export function Root({ client }: { client: Client }) {
  return (
    <ErrorBoundary>
      <ClientProvider client={client}>
        <LocaleProvider>
          <Boot>
            <FirstRunGate>
              <OwnerProvider>
                <HashRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
                  <Shell />
                </HashRouter>
              </OwnerProvider>
            </FirstRunGate>
          </Boot>
        </LocaleProvider>
      </ClientProvider>
    </ErrorBoundary>
  );
}
