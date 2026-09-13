import { useEffect, useId, useRef, type ReactNode } from "react";

/**
 * A modal dialog: labelled, focus moved inside on open and returned on close, Escape to dismiss.
 *
 * Small on purpose. Lite's dialogs are forms of a few fields; a library for focus-trapping every edge case
 * would be the eleven-primitive copy D7 was amended away from.
 */
export function Dialog({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const titleId = useId();
  const panel = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const first = panel.current?.querySelector<HTMLElement>("input, select, button, textarea");
    first?.focus();
    const onKey = (event: { key: string }) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      previous?.focus?.();
    };
  }, [onClose]);

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-text/40 p-4">
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="w-full max-w-md space-y-4 rounded-lg border border-border bg-surface-raised p-6 shadow-lg"
      >
        <h2 id={titleId} className="text-lg font-semibold">
          {title}
        </h2>
        {children}
      </div>
    </div>
  );
}
