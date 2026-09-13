import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useClient } from "@/api/ClientContext";
import type { OwnerStatus } from "@/api/client";
import { BindingError } from "@/api/envelope";
import { PinDialog } from "./PinDialog";

/** The code every guarded act returns outside owner mode (owner/domain.CodeRequired). */
export const CODE_OWNER_REQUIRED = "lite.owner.required";

/** Thrown by withOwner when the person closes the PIN dialog. A screen shows nothing for it. */
export class OwnerCancelled extends Error {
  constructor() {
    super("owner PIN cancelled");
    this.name = "OwnerCancelled";
  }
}

interface OwnerState {
  status: OwnerStatus;
  /** Re-reads owner mode and lockout from Go. */
  refresh: () => Promise<void>;
  /** Asks for the PIN, then resolves once owner mode is on. Rejects with OwnerCancelled if dismissed. */
  requestPin: () => Promise<void>;
  /** Leaves owner mode now. */
  lock: () => Promise<void>;
  /**
   * Runs a guarded act. If Go refuses it with lite.owner.required, the PIN dialog opens; a correct PIN
   * enters owner mode and the act is retried ONCE. A second refusal is thrown, never looped (D-L1.10).
   *
   * A screen writes a guarded act the way it writes any act — it does not know it is guarded, so a future
   * guarded act cannot forget to ask.
   */
  withOwner: <T>(act: () => Promise<T>) => Promise<T>;
}

const OwnerContext = createContext<OwnerState | null>(null);

const OFF: OwnerStatus = { setUp: true, lockedSeconds: 0, elevatedSeconds: 0 };

export function OwnerProvider({ children }: { children: ReactNode }) {
  const client = useClient();
  const [status, setStatus] = useState<OwnerStatus>(OFF);
  const [prompt, setPrompt] = useState<{ resolve: () => void; reject: (e: Error) => void } | null>(null);
  const mounted = useRef(true);

  const refresh = useCallback(async () => {
    try {
      const next = await client.owner.status();
      if (mounted.current) setStatus(next);
    } catch {
      // The header keeps its last known state; a status read failing is not worth interrupting a sale.
    }
  }, [client]);

  useEffect(() => {
    mounted.current = true;
    void refresh();
    return () => {
      mounted.current = false;
    };
  }, [refresh]);

  // A local countdown between reads, re-synchronised from Go when it runs out. Seconds come from Go as
  // durations, so the webview's clock is never compared with Go's.
  const counting = status.elevatedSeconds > 0 || status.lockedSeconds > 0;
  useEffect(() => {
    if (!counting) return;
    const timer = setTimeout(() => {
      // A PURE updater. The re-read from Go lives in the effect below, not in here: React may call an
      // updater twice (StrictMode does), and a side effect inside one fires twice.
      setStatus((s) => ({
        ...s,
        elevatedSeconds: Math.max(0, s.elevatedSeconds - 1),
        lockedSeconds: Math.max(0, s.lockedSeconds - 1),
      }));
    }, 1000);
    return () => clearTimeout(timer);
  }, [counting, status]);

  // When a countdown reaches zero, ask Go what is true now — owner mode may have been re-entered elsewhere,
  // or a lock may have been lengthened by another failed attempt.
  const wasCounting = useRef(false);
  useEffect(() => {
    if (wasCounting.current && !counting) void refresh();
    wasCounting.current = counting;
  }, [counting, refresh]);

  const requestPin = useCallback(
    () => new Promise<void>((resolve, reject) => setPrompt({ resolve, reject })),
    [],
  );

  const lock = useCallback(async () => {
    setStatus(await client.owner.endElevation());
  }, [client]);

  const withOwner = useCallback(
    async <T,>(act: () => Promise<T>): Promise<T> => {
      try {
        return await act();
      } catch (error) {
        if (!(error instanceof BindingError) || error.code !== CODE_OWNER_REQUIRED) throw error;
      }
      await requestPin();
      return act(); // once: a second refusal propagates to the screen
    },
    [requestPin],
  );

  const value = useMemo<OwnerState>(() => ({ status, refresh, requestPin, lock, withOwner }), [status, refresh, requestPin, lock, withOwner]);

  return (
    <OwnerContext.Provider value={value}>
      {children}
      {prompt ? (
        <PinDialog
          onElevated={(next) => {
            setStatus(next);
            prompt.resolve();
            setPrompt(null);
          }}
          onCancel={() => {
            prompt.reject(new OwnerCancelled());
            setPrompt(null);
          }}
        />
      ) : null}
    </OwnerContext.Provider>
  );
}

export function useOwner(): OwnerState {
  const state = useContext(OwnerContext);
  if (!state) throw new Error("useOwner called outside OwnerProvider");
  return state;
}
