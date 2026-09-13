import { createContext, useContext, type ReactNode } from "react";
import type { Client } from "./client";

const ClientContext = createContext<Client | null>(null);

/** Supplies the client. The application passes the real one; tests pass a fake. */
export function ClientProvider({ client, children }: { client: Client; children: ReactNode }) {
  return <ClientContext.Provider value={client}>{children}</ClientContext.Provider>;
}

export function useClient(): Client {
  const client = useContext(ClientContext);
  if (!client) {
    // A programming error, not a runtime condition: every tree is rendered inside a provider.
    throw new Error("useClient called outside ClientProvider");
  }
  return client;
}
