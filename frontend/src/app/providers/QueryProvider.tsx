import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

/**
 * Builds the query client.
 *
 * # Why retries are off (Step 1.10, D4)
 *
 * TanStack Query's defaults are tuned for HTTP over a network: retry three times with backoff,
 * because a request may fail for reasons that resolve on their own. **None of that applies
 * here.** A binding call is an IPC to a process on the same machine. If it failed, it failed
 * because the graph is not ready, the session ended, or a business rule refused — and none of
 * those changes in 200ms.
 *
 * Retrying would turn one clear error into three identical ones plus a delay, and would make a
 * permission denial take a second and a half to appear.
 *
 * # Why staleTime is not zero
 *
 * The default refetches on every window focus. On a desktop app that a cashier alt-tabs
 * through all day, that is a burst of database reads for data that has not changed. Screens
 * that need immediacy invalidate explicitly after a mutation, which is both cheaper and
 * correct — the write is what changes the data, not the focus event.
 */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        refetchOnWindowFocus: false,
        staleTime: 30_000,
      },
      mutations: {
        retry: false,
      },
    },
  });
}

/**
 * Provides the query client.
 *
 * Created in state rather than at module scope so each test — and each mounted app — gets its
 * own cache. A module-level client would let one test's cached answers leak into the next,
 * which is the same class of bug as the sign-out leak D3 guards against, only quieter.
 */
export function QueryProvider({
  children,
  client,
}: {
  children: ReactNode;
  client?: QueryClient;
}) {
  const [fallback] = useState(createQueryClient);
  return <QueryClientProvider client={client ?? fallback}>{children}</QueryClientProvider>;
}
