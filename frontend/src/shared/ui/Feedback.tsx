import type { ReactNode } from "react";
import { cn } from "./cn";

// The four states every screen must have designed versions of (§FE.2, §31): loading, empty,
// error, and permission-denied. They are primitives rather than per-screen markup because
// "where professional is actually decided" is exactly the place teams improvise otherwise.

// ── Spinner ─────────────────────────────────────────────────────────────────────

export function Spinner({ size = "md" }: { size?: "sm" | "md" | "lg" }) {
  const px = { sm: "h-4 w-4", md: "h-5 w-5", lg: "h-8 w-8" }[size];
  return (
    <span
      role="status"
      aria-live="polite"
      className={cn("inline-block animate-spin rounded-full border-2 border-border", px)}
      style={{ borderTopColor: "rgb(var(--color-primary))" }}
    />
  );
}

// ── Skeleton ────────────────────────────────────────────────────────────────────

export function Skeleton({ className }: { className?: string }) {
  return (
    <div
      aria-hidden="true"
      className={cn("animate-pulse rounded bg-surface-sunken", className)}
    />
  );
}

// ── EmptyState ──────────────────────────────────────────────────────────────────

export type EmptyStateTone = "empty" | "error" | "denied";

const TONES: Record<EmptyStateTone, { icon: string; color: string }> = {
  empty: { icon: "◦", color: "text-text-muted" },
  error: { icon: "!", color: "text-danger" },
  // Permission-denied ships now as a variant with no producer: RBAC arrives in Phase 1, and
  // building the *variant* costs nothing while building a permission system to feed it would
  // be Phase 1 work done blind. Phase 1 wires a consumer instead of inventing a pattern.
  denied: { icon: "×", color: "text-warning" },
};

export function EmptyState({
  tone = "empty",
  title,
  description,
  action,
}: {
  tone?: EmptyStateTone;
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  const { icon, color } = TONES[tone];
  return (
    <div className="flex flex-col items-center justify-center gap-2 p-8 text-center">
      <span
        aria-hidden="true"
        className={cn(
          "flex h-10 w-10 items-center justify-center rounded-full bg-surface-sunken text-lg",
          color,
        )}
      >
        {icon}
      </span>
      <p className="font-medium text-text">{title}</p>
      {description ? <p className="max-w-sm text-sm text-text-muted">{description}</p> : null}
      {action ? <div className="mt-2">{action}</div> : null}
    </div>
  );
}

// ── Alert ───────────────────────────────────────────────────────────────────────

export type AlertTone = "info" | "success" | "warning" | "danger";

const ALERT_TONES: Record<AlertTone, string> = {
  info: "bg-info-subtle text-info border-info/30",
  success: "bg-success-subtle text-success border-success/30",
  warning: "bg-warning-subtle text-warning border-warning/30",
  danger: "bg-danger-subtle text-danger border-danger/30",
};

export function Alert({
  tone = "info",
  title,
  children,
}: {
  tone?: AlertTone;
  title?: string;
  children?: ReactNode;
}) {
  return (
    <div
      // "alert" for the tones a user must not miss; "status" is politer for the rest, so a
      // screen reader is not interrupted by a success banner.
      role={tone === "danger" || tone === "warning" ? "alert" : "status"}
      className={cn("rounded border p-3 text-sm", ALERT_TONES[tone])}
    >
      {title ? <p className="font-medium">{title}</p> : null}
      {children ? <div className={cn(title && "mt-1")}>{children}</div> : null}
    </div>
  );
}
