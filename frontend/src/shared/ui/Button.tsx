import { Slot } from "@radix-ui/react-slot";
import { forwardRef, type ButtonHTMLAttributes } from "react";
import { cn } from "./cn";
import { Spinner } from "./Feedback";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";
export type ButtonSize = "sm" | "md";

const VARIANTS: Record<ButtonVariant, string> = {
  primary: "bg-primary text-primary-fg hover:bg-primary/90",
  secondary: "bg-surface text-text border border-border hover:bg-surface-raised",
  ghost: "bg-transparent text-text hover:bg-surface-raised",
  danger: "bg-danger text-danger-fg hover:bg-danger/90",
};

const SIZES: Record<ButtonSize, string> = {
  sm: "h-8 px-3 text-sm gap-1.5",
  md: "h-10 px-4 text-sm gap-2",
};

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  /** Renders a spinner and blocks interaction. */
  loading?: boolean;
  /** Renders the child element instead of a <button>, keeping the styling. */
  asChild?: boolean;
}

/**
 * The primary action primitive.
 *
 * Focus styling is inherited from the global :focus-visible rule rather than declared here —
 * one focus treatment for the whole application (index.css), so a primitive cannot
 * accidentally ship without one.
 */
export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = "secondary", size = "md", loading = false, asChild = false, className, children, disabled, ...rest },
  ref,
) {
  const Comp = asChild ? Slot : "button";
  return (
    <Comp
      ref={ref}
      // `disabled` alone would drop the button out of the tab order while loading, moving
      // focus somewhere unpredictable mid-action; aria-busy states it without that.
      disabled={disabled ?? loading}
      aria-busy={loading || undefined}
      className={cn(
        "inline-flex items-center justify-center rounded font-medium transition-colors",
        "disabled:pointer-events-none disabled:opacity-50",
        VARIANTS[variant],
        SIZES[size],
        className,
      )}
      {...rest}
    >
      {loading ? <Spinner size="sm" /> : null}
      {children}
    </Comp>
  );
});
