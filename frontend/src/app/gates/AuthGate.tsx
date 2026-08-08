import { useQuery } from "@tanstack/react-query";
import { useEffect, type ReactNode } from "react";
import { GateScreen } from "@/app/gates/GateScreen";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { useSessionStore } from "@/app/session/session";
import { LoginScreen } from "@/modules/auth/LoginScreen";
import { me } from "@/lib/wails";
import { Alert } from "@/shared/ui";

/** The query key for the current session. Exported so login and logout can invalidate it. */
export const SESSION_KEY = ["auth", "me"] as const;

/**
 * Decides between the login screen and the application.
 *
 * # A mount boundary, again (D1)
 *
 * Nothing below renders until the session is known. The shell's screens call permissioned
 * bindings on mount; letting them render against an unresolved session would produce a burst
 * of session-invalid errors and a flash of a UI the user is not entitled to see.
 *
 * # This is not the security boundary
 *
 * A determined caller can reach the bindings directly — this is a webview, and the JavaScript
 * in it is not trusted. Every binding therefore re-checks on the Go side, where the guard is
 * the only path to the object graph (1.5 D1). This gate decides what to SHOW, and nothing else.
 */
export function AuthGate({ children }: { children: ReactNode }) {
  const setSession = useSessionStore((state) => state.setSession);

  const session = useQuery({
    queryKey: SESSION_KEY,
    // `me()` resolves with signedIn:false rather than throwing when nobody is signed in — an
    // ended session is a state, not a failure (1.5).
    queryFn: me,
  });

  // Mirrored into the store so components can read the principal without threading it or
  // re-querying. The query stays the source of truth; this is a projection of it.
  useEffect(() => {
    if (session.data) setSession(session.data);
  }, [session.data, setSession]);

  if (session.isPending) return <GateScreen kind="loading" />;
  if (session.isError) return <GateScreen kind="error" error={session.error} />;
  if (!session.data.signedIn) return <LoginScreen />;
  if (session.data.mustChange) return <MustChangePassword />;

  return <>{children}</>;
}

/**
 * The dead end for a user whose password must be changed.
 *
 * # Why this is a dead end rather than a pass-through (D6)
 *
 * `must_change` exists for exactly one situation: an administrator set someone's password, so
 * that person is signing in with a credential another human knows. Letting them through
 * "temporarily" would make the flag a lie precisely when it matters.
 *
 * The change-password path is Step 1.11, with the user screens. Until it exists, this screen
 * says so plainly rather than pretending the flag was honoured. A visible, explained dead end
 * is a bug report; a silent pass-through is a security hole nobody notices.
 */
function MustChangePassword() {
  const { t } = useTranslation();
  return (
    <div className="flex h-full items-center justify-center p-8">
      <div className="w-full max-w-lg">
        <Alert tone="warning" title={t("auth.mustChange.title")}>
          {t("auth.mustChange.body")}
        </Alert>
      </div>
    </div>
  );
}
