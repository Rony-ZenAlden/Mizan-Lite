import { create } from "zustand";
import type { SessionInfo } from "@/lib/wails";

/**
 * The signed-in principal, as the UI knows it.
 *
 * # What this store holds, and what it deliberately does not
 *
 * It holds the principal and the effective permission set. It holds **no token** — the session
 * token lives in the Go process and never crosses the boundary (Step 1.5, D3), so there is
 * nothing here for an injected script or a devtools inspection to steal.
 *
 * It also holds no server data. Lists, entities, and audit entries belong in TanStack Query,
 * which caches, dedupes, and invalidates them. Keeping a second copy here would create two
 * answers to the same question that drift the moment one is refreshed and the other is not.
 *
 * So this store is small on purpose. Zustand is here for the one piece of genuinely global
 * client state — who is signed in — and not as a general dumping ground.
 */
interface SessionState {
  session: SessionInfo | null;
  setSession: (session: SessionInfo | null) => void;
  clear: () => void;
}

export const useSessionStore = create<SessionState>((set) => ({
  session: null,
  setSession: (session) => set({ session }),
  clear: () => set({ session: null }),
}));

/** The signed-in principal, or null. */
export function useSession(): SessionInfo | null {
  return useSessionStore((state) => state.session);
}

/**
 * Whether the signed-in user holds a permission.
 *
 * # COSMETIC ONLY (§14.3)
 *
 * This decides what the UI OFFERS. It decides nothing about what the system permits: every
 * binding re-checks on the Go side, where the check is structural (Step 1.5, D1 — the guard is
 * the only path to the object graph, so a method that skips it is non-functional).
 *
 * Read that twice before removing a backend check because "the UI already hides it". Hiding a
 * button is not access control; it is politeness.
 *
 * Wildcards are grant-side only, matching the backend's rule (1.4): a grant of `identity.*`
 * satisfies `identity.user.view`, but a grant of `identity.user.view` never satisfies a
 * question about `identity.*`. Asking with a wildcard would be asking "may I do anything in
 * this area?", which is not a question any screen should be deciding on.
 */
export function hasPermission(permissions: string[], permission: string): boolean {
  if (!permission) return false;
  for (const granted of permissions) {
    if (granted === "*" || granted === permission) return true;
    if (granted.endsWith(".*") && permission.startsWith(granted.slice(0, -1))) return true;
  }
  return false;
}

/** Hook form of hasPermission, reading the current session. */
export function useCan(permission: string): boolean {
  const session = useSession();
  return session ? hasPermission(session.permissions, permission) : false;
}
