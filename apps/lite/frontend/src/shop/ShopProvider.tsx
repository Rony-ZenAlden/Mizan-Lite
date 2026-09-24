import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useClient } from "@/api/ClientContext";
import type { ShopState } from "@/api/client";

/** The shop as its header shows it: its name, and its logo as base64 PNG ("" when it has none). */
export interface ShopIdentity {
  name: string;
  logo: string;
}

interface ShopContextState {
  /** Null until the first read. */
  shop: ShopIdentity | null;
  /** Adopts what a binding just returned — after the owner changes the store information or the logo. */
  adopt: (next: ShopState) => void;
}

const ShopContext = createContext<ShopContextState | null>(null);

/**
 * The shop's name and logo, shared by the header and Settings → Store information (0.10.1). Whatever logo the owner
 * uploads heads the screen as it heads the invoice — the application holds no shop's logo of its own — and a change in
 * Settings shows in the header at once, not at the next start.
 */
export function ShopProvider({ children }: { children: ReactNode }) {
  const client = useClient();
  const [shop, setShop] = useState<ShopIdentity | null>(null);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    client.settings
      .shop()
      .then((s) => {
        if (mounted.current) setShop({ name: s.name, logo: s.logo });
      })
      .catch(() => {
        // The header falls back to the application's name; a failed read is not worth a banner.
      });
    return () => {
      mounted.current = false;
    };
  }, [client]);

  const adopt = useCallback((next: ShopState) => setShop({ name: next.name, logo: next.logo }), []);
  const value = useMemo<ShopContextState>(() => ({ shop, adopt }), [shop, adopt]);
  return <ShopContext.Provider value={value}>{children}</ShopContext.Provider>;
}

/** The shop's identity; outside a ShopProvider (a screen rendered on its own) there is none, and adopting does nothing. */
export function useShop(): ShopContextState {
  return useContext(ShopContext) ?? { shop: null, adopt: () => {} };
}
