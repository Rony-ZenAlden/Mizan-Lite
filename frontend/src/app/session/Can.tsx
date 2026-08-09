import type { ReactNode } from "react";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { useCan } from "@/app/session/session";
import { EmptyState } from "@/shared/ui";

/**
 * Shows its children only when the signed-in user holds a permission.
 *
 * # COSMETIC ONLY (§14.3)
 *
 * This hides a button. It protects nothing. Every binding re-checks on the Go side, where the
 * guard is the only path to the object graph (1.5 D1), so a method that skips its policy is
 * non-functional rather than unprotected.
 *
 * If you are here because you are considering removing a backend check "since the UI already
 * hides it": don't. This is a webview, and the JavaScript in it is not trusted.
 *
 * What it IS for: not offering actions that would fail. A cashier seeing a "Manage roles"
 * button that always errors learns to ignore errors, which is a worse outcome than not seeing
 * the button.
 */
export function Can({
  permission,
  children,
  fallback = null,
}: {
  permission: string;
  children: ReactNode;
  fallback?: ReactNode;
}) {
  return useCan(permission) ? <>{children}</> : <>{fallback}</>;
}

/**
 * A route body that renders a denied state instead of its children.
 *
 * The `EmptyState` `denied` variant built in Step 0.11 (D6) finally gets its producer. It
 * exists because a screen reached without permission should EXPLAIN itself — a blank page or a
 * silent redirect leaves the user unable to say what to ask for.
 *
 * Cosmetic, for the same reasons as Can.
 */
export function RequirePermission({
  permission,
  children,
}: {
  permission: string;
  children: ReactNode;
}) {
  const { t } = useTranslation();
  const allowed = useCan(permission);

  if (!allowed) {
    return (
      <EmptyState
        tone="denied"
        title={t("permission.denied.title")}
        description={t("permission.denied.body", { permission })}
      />
    );
  }
  return <>{children}</>;
}
