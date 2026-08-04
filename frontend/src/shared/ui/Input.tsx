import { forwardRef, useId, type InputHTMLAttributes } from "react";
import { cn } from "./cn";

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  /** A field-level error. Rendered text; the caller translates the code first. */
  error?: string;
  hint?: string;
}

/**
 * A labelled text input.
 *
 * The label is bound with a generated id rather than left to the caller, because an unlabelled
 * input is invisible to a screen reader and that is the single most common accessibility
 * defect in form-heavy software — which an ERP entirely is.
 */
export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { label, error, hint, className, id, ...rest },
  ref,
) {
  const generated = useId();
  const inputId = id ?? generated;
  const describedBy = error ? `${inputId}-error` : hint ? `${inputId}-hint` : undefined;

  return (
    <div className="flex flex-col gap-1">
      {label ? (
        <label htmlFor={inputId} className="text-sm font-medium text-text">
          {label}
        </label>
      ) : null}
      <input
        ref={ref}
        id={inputId}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy}
        className={cn(
          "h-10 w-full rounded border bg-surface px-3 text-sm text-text",
          "placeholder:text-text-muted disabled:opacity-50",
          error ? "border-danger" : "border-border",
          className,
        )}
        {...rest}
      />
      {error ? (
        <p id={`${inputId}-error`} className="text-xs text-danger">
          {error}
        </p>
      ) : hint ? (
        <p id={`${inputId}-hint`} className="text-xs text-text-muted">
          {hint}
        </p>
      ) : null}
    </div>
  );
});
