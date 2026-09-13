/** A progress indicator with an accessible name. The name is passed in already translated. */
export function Spinner({ label }: { label: string }) {
  return (
    <span role="status" aria-live="polite" className="inline-flex items-center gap-2 text-text-muted">
      <span aria-hidden="true" className="h-4 w-4 animate-spin rounded-full border-2 border-border border-t-primary" />
      <span>{label}</span>
    </span>
  );
}
