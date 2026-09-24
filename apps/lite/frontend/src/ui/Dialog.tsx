import { useEffect, useId, useLayoutEffect, useRef, type ReactNode } from "react";

/**
 * A modal dialog: labelled, focus moved inside on open and returned on close, Escape to dismiss.
 *
 * Small on purpose. Lite's dialogs are forms of a few fields; a library for focus-trapping every edge case
 * would be the eleven-primitive copy D7 was amended away from.
 */
export function Dialog({ title, onClose, wide = false, children }: { title: string; onClose: () => void; wide?: boolean; children: ReactNode }) {
  const titleId = useId();
  const panel = useRef<HTMLDivElement>(null);
  // The latest onClose, read when Escape is pressed. Focus is moved in once, when the dialog opens: a screen that
  // re-renders every second — the owner's countdown does — hands a new onClose each time, and an effect keyed on it
  // pulled focus back to the first field every second while a person typed (found in 0.10.0).
  const close = useRef(onClose);
  useLayoutEffect(() => {
    close.current = onClose;
  });

  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const first = panel.current?.querySelector<HTMLElement>("input, select, button, textarea");
    first?.focus();
    const onKey = (event: { key: string }) => {
      if (event.key === "Escape") close.current();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      previous?.focus?.();
    };
  }, []);

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-text/40 p-4">
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={`max-h-full w-full ${wide ? "max-w-4xl" : "max-w-md"} space-y-4 overflow-auto rounded-lg border border-border bg-surface-raised p-6 shadow-lg`}
      >
        <h2 id={titleId} className="text-lg font-semibold">
          {title}
        </h2>
        {children}
      </div>
    </div>
  );
}
