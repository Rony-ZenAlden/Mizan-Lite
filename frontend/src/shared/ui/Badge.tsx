import type { ReactNode } from "react";
import { cn } from "./cn";

/**
 * A badge's tone. The same vocabulary Alert uses, plus a neutral for labels that carry no
 * judgement — "default address" is a fact, not a warning.
 */
export type BadgeTone = "neutral" | "info" | "success" | "warning" | "danger";

const BADGE_TONES: Record<BadgeTone, string> = {
  neutral: "bg-surface-raised text-text-muted border-border",
  info: "bg-info-subtle text-info border-info/30",
  success: "bg-success-subtle text-success border-success/30",
  warning: "bg-warning-subtle text-warning border-warning/30",
  danger: "bg-danger-subtle text-danger border-danger/30",
};

/**
 * A small inline label.
 *
 * Built as a primitive rather than inlined because two screens needed the same thing on the same
 * day — the partner list showing that one row is both a customer and a supplier, and the address
 * list marking the default. A third would have drifted.
 *
 * Purely presentational: it carries no role and no live region, because a badge repeats
 * information the row already states. Announcing "customer" twice helps nobody.
 */
export function Badge({ tone = "neutral", children }: { tone?: BadgeTone; children: ReactNode }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded border px-1.5 py-0.5 text-xs font-medium",
        BADGE_TONES[tone],
      )}
    >
      {children}
    </span>
  );
}
