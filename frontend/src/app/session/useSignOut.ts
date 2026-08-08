import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSessionStore } from "@/app/session/session";
import { logout } from "@/lib/wails";

/**
 * Signs the current user out.
 *
 * # The cache MUST be cleared (Step 1.10, D3)
 *
 * `queryClient.clear()` is not tidiness. Without it, the next person to sign in at the same
 * terminal sees the previous user's cached lists until every query happens to refetch — and on
 * a shop floor where one machine is shared by three cashiers through a day, that is a real
 * disclosure of data the second cashier is not entitled to.
 *
 * It is also a leak the BACKEND cannot prevent. Every binding re-checks permissions, but a
 * cached answer never reaches a binding: it is already in the browser's memory, rendered from
 * a query key that says nothing about who asked for it. This is the frontend's own
 * responsibility, which is exactly why it has a test.
 *
 * Cleared even when the call FAILS. A user pressing "sign out" must always end up signed out
 * locally; leaving them looking at their own data because the backend hiccuped would be the
 * wrong half to keep.
 */
export function useSignOut() {
  const queryClient = useQueryClient();
  const clearSession = useSessionStore((state) => state.clear);

  return useMutation({
    mutationFn: logout,
    onSettled: () => {
      clearSession();
      queryClient.clear();
    },
  });
}
