import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useClient } from "@/api/ClientContext";
import type { RateState } from "@/api/client";

/** How often the rate is re-read, so its age and "not updated today" stay true while a screen is open. */
export const RATE_POLL_MS = 60_000;

interface RateContextState {
  /** The rate in force as Go last reported it, or null before the first read. */
  rate: RateState | null;
  /** Re-reads the rate. */
  reload: () => Promise<void>;
  /** Adopts a rate a binding just returned (after setting, refreshing or accepting), without another read. */
  adopt: (next: RateState) => void;
}

const RateContext = createContext<RateContextState | null>(null);

/**
 * The exchange rate in force, shared by the header, the rate screen and the receipt form (L3 §10). Go owns every figure;
 * this holds Go's last answer.
 */
export function RateProvider({ children }: { children: ReactNode }) {
  const client = useClient();
  const [rate, setRate] = useState<RateState | null>(null);
  const mounted = useRef(true);

  const reload = useCallback(async () => {
    try {
      const next = await client.fx.current();
      if (mounted.current) setRate(next);
    } catch {
      // The header keeps the last rate it knew; a failed read is not worth interrupting a sale for.
    }
  }, [client]);

  useEffect(() => {
    mounted.current = true;
    void reload();
    const timer = setInterval(() => void reload(), RATE_POLL_MS);
    return () => {
      mounted.current = false;
      clearInterval(timer);
    };
  }, [reload]);

  const value = useMemo<RateContextState>(() => ({ rate, reload, adopt: setRate }), [rate, reload]);
  return <RateContext.Provider value={value}>{children}</RateContext.Provider>;
}

export function useRate(): RateContextState {
  const state = useContext(RateContext);
  if (!state) throw new Error("useRate called outside RateProvider");
  return state;
}
