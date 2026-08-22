import type { ReactNode } from "react";
import { cn } from "./cn";

/**
 * The layout layer (Step 10.13).
 *
 * # What this is for
 *
 * The 10.12 audit counted the problem precisely: **24 of 30 screens were a heading, an optional
 * filter, and one table.** Not one of them was wrong; together they made the product read as a
 * database viewer, because nothing distinguished a three-field form from a forty-row ledger.
 *
 * These four components are the vocabulary that distinguishes them. They add no behaviour and no
 * state — everything here is spacing, hierarchy, and a border — which is deliberate: a layout
 * layer that owns data is a layer that has to be understood before a screen can be read.
 */

/**
 * A page's title, its explanation, and what you can do about it.
 *
 * The actions sit in the header rather than above the table, because the primary action on a
 * screen is a property of the SCREEN, not of the list beneath it — and a button that moves when
 * a table is empty is a button people stop finding.
 */
export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-start justify-between gap-3 pb-1">
      <div className="flex min-w-0 flex-col gap-1">
        <h2 className="text-lg font-semibold tracking-tight text-text">{title}</h2>
        {description ? (
          <p className="max-w-2xl text-sm leading-relaxed text-text-muted">{description}</p>
        ) : null}
      </div>
      {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
    </header>
  );
}

/**
 * A bounded region with its own heading.
 *
 * The audit's finding was that everything sat at one level. A card is what says "these things
 * belong together and this other thing does not" — which is most of what a reader needs before
 * they read anything.
 */
export function Card({
  title,
  description,
  actions,
  children,
  className,
  padded = true,
}: {
  title?: string;
  description?: string;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
  /** A table sets its own padding; a form does not. */
  padded?: boolean;
}) {
  return (
    <section
      className={cn(
        "flex flex-col overflow-hidden rounded-xl border border-border bg-surface",
        "shadow-[0_1px_2px_rgba(0,0,0,0.04)]",
        className,
      )}
    >
      {title || actions ? (
        <div className="flex flex-wrap items-start justify-between gap-2 border-b border-border px-4 py-3">
          <div className="flex min-w-0 flex-col gap-0.5">
            {title ? <h3 className="text-sm font-medium text-text">{title}</h3> : null}
            {description ? <p className="text-xs text-text-muted">{description}</p> : null}
          </div>
          {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
        </div>
      ) : null}
      <div className={cn(padded && "p-4", "min-w-0")}>{children}</div>
    </section>
  );
}

/**
 * One number, with what it is and how to read it.
 *
 * # Why the qualifier is a required prop and not an option
 *
 * 8.7 D4 recorded the rule the dashboard needed: *a number without its qualification is a number
 * that will be misread.* "Stock value 1,200.00" beside "revenue 450.00" is read as this month's
 * purchases unless something says otherwise.
 *
 * So `caption` is not optional. A tile that cannot say what period it covers, or where its figure
 * came from, should not be on a screen.
 */
export function StatTile({
  label,
  value,
  caption,
  tone = "neutral",
  className,
}: {
  label: string;
  value: string;
  caption: string;
  tone?: "neutral" | "positive" | "negative" | "muted";
  className?: string;
}) {
  return (
    <article
      className={cn(
        "flex flex-col gap-1 rounded-xl border border-border bg-surface p-4",
        "shadow-[0_1px_2px_rgba(0,0,0,0.04)]",
        className,
      )}
    >
      <span className="text-xs font-medium uppercase tracking-wide text-text-muted">{label}</span>
      <span
        className={cn(
          "text-2xl font-semibold tabular-nums tracking-tight",
          tone === "positive" && "text-success",
          tone === "negative" && "text-danger",
          tone === "muted" && "text-text-muted",
          tone === "neutral" && "text-text",
        )}
      >
        {value}
      </span>
      <span className="text-xs text-text-muted">{caption}</span>
    </article>
  );
}

/**
 * The strip above a list: filters on one side, actions on the other.
 *
 * Separated from `PageHeader` because they answer different questions. A header says what the
 * screen IS; a toolbar says what you are looking at within it — and on a screen with several
 * lists, there is one header and several toolbars.
 */
export function ToolBar({
  children,
  actions,
  className,
}: {
  children?: ReactNode;
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-wrap items-end justify-between gap-3",
        className,
      )}
    >
      <div className="flex flex-wrap items-end gap-3">{children}</div>
      {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
    </div>
  );
}

/**
 * A numbered stage in a guided task.
 *
 * The audit's third finding: core operations were flat forms. A purchase is four acts over days —
 * order, receive, bill, pay — and a screen that shows them as four unrelated menu entries makes
 * the user hold the sequence in their head.
 */
export function StepList({
  steps,
  current,
  onSelect,
}: {
  steps: Array<{ id: string; label: string; hint?: string; done?: boolean }>;
  current: string;
  onSelect?: (id: string) => void;
}) {
  return (
    <ol className="flex flex-wrap gap-2">
      {steps.map((step, index) => {
        const active = step.id === current;
        return (
          <li key={step.id} className="flex-1 min-w-[10rem]">
            <button
              type="button"
              onClick={() => onSelect?.(step.id)}
              disabled={!onSelect}
              aria-current={active ? "step" : undefined}
              className={cn(
                "flex w-full items-start gap-3 rounded-lg border px-3 py-2 text-start transition-colors",
                active
                  ? "border-primary bg-primary-subtle"
                  : "border-border bg-surface hover:bg-surface-muted",
              )}
            >
              <span
                className={cn(
                  "flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs tabular-nums",
                  step.done
                    ? "bg-success text-success-fg"
                    : active
                      ? "bg-primary text-primary-fg"
                      : "bg-surface-muted text-text-muted",
                )}
              >
                {step.done ? "✓" : index + 1}
              </span>
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="text-sm font-medium text-text">{step.label}</span>
                {step.hint ? (
                  <span className="text-xs text-text-muted">{step.hint}</span>
                ) : null}
              </span>
            </button>
          </li>
        );
      })}
    </ol>
  );
}
