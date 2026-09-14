import { forwardRef, useId, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes } from "react";

interface FieldShell {
  label: string;
  hint?: string;
  error?: string;
}

function Shell({ id, label, hint, error, children }: FieldShell & { id: string; children: ReactNode }) {
  return (
    <div className="space-y-1">
      <label htmlFor={id} className="block text-sm font-medium">
        {label}
      </label>
      {children}
      {hint && !error ? (
        <p id={`${id}-hint`} className="text-xs text-text-muted">
          {hint}
        </p>
      ) : null}
      {error ? (
        <p id={`${id}-error`} role="alert" className="text-xs text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

const inputClass =
  "block w-full rounded-md border border-border bg-surface-raised px-3 py-2 text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60 aria-[invalid=true]:border-danger";

/**
 * A labelled input. The label, hint and error are passed in already translated. The ref reaches the input, so a screen
 * can hold focus on it (the till's scan field).
 */
export const TextField = forwardRef<HTMLInputElement, FieldShell & InputHTMLAttributes<HTMLInputElement>>(function TextField(
  { label, hint, error, ...input },
  ref,
) {
  const id = useId();
  return (
    <Shell id={id} label={label} hint={hint} error={error}>
      <input
        ref={ref}
        id={id}
        className={inputClass}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${id}-error` : hint ? `${id}-hint` : undefined}
        {...input}
      />
    </Shell>
  );
});

/** An input inside a table cell: named for assistive technology by its aria-label, its error shown beneath it. */
export function CellInput({ error, ...input }: { error?: string } & InputHTMLAttributes<HTMLInputElement>) {
  const id = useId();
  return (
    <div className="space-y-1">
      <input
        id={id}
        className={inputClass}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${id}-error` : undefined}
        {...input}
      />
      {error ? (
        <p id={`${id}-error`} role="alert" className="text-xs text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

/** A labelled select. */
export function SelectField({ label, hint, error, children, ...select }: FieldShell & SelectHTMLAttributes<HTMLSelectElement>) {
  const id = useId();
  return (
    <Shell id={id} label={label} hint={hint} error={error}>
      <select
        id={id}
        className={inputClass}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${id}-error` : hint ? `${id}-hint` : undefined}
        {...select}
      >
        {children}
      </select>
    </Shell>
  );
}

/**
 * A PIN input: masked, a numeric keypad on touch screens, never autofilled or remembered by the webview.
 * Arabic-Indic digits are accepted as typed; Go normalises them.
 */
export function PinField(props: FieldShell & Omit<InputHTMLAttributes<HTMLInputElement>, "type">) {
  return <TextField type="password" inputMode="numeric" autoComplete="off" spellCheck={false} maxLength={12} dir="ltr" {...props} />;
}
