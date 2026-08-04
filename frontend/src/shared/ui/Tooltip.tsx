import * as RadixTooltip from "@radix-ui/react-tooltip";
import type { ReactNode } from "react";

/** Wraps the app once so tooltips share timing. */
export function TooltipProvider({ children }: { children: ReactNode }) {
  return <RadixTooltip.Provider delayDuration={300}>{children}</RadixTooltip.Provider>;
}

/**
 * A hover/focus hint.
 *
 * A tooltip is never the only carrier of information: it is unreachable on touch and
 * easily missed by screen readers, so anything essential belongs in the label itself.
 */
export function Tooltip({ label, children }: { label: string; children: ReactNode }) {
  return (
    <RadixTooltip.Root>
      <RadixTooltip.Trigger asChild>{children}</RadixTooltip.Trigger>
      <RadixTooltip.Portal>
        <RadixTooltip.Content
          sideOffset={6}
          className="z-50 rounded border border-border bg-surface px-2 py-1 text-xs text-text shadow-md"
        >
          {label}
          <RadixTooltip.Arrow className="fill-[rgb(var(--color-surface))]" />
        </RadixTooltip.Content>
      </RadixTooltip.Portal>
    </RadixTooltip.Root>
  );
}
