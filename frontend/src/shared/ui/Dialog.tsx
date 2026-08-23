import * as RadixDialog from "@radix-ui/react-dialog";
import { useEffect, useRef, type ReactNode } from "react";
import { cn } from "./cn";

// Dialog and Sheet are the same Radix primitive with different geometry: a Dialog is centred,
// a Sheet is anchored to an edge. Sharing the implementation means focus trapping, scroll
// locking, Escape handling, and focus restoration are written once — the parts that are hard
// to get right and impossible to notice when they are wrong.

interface BaseProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  /** Screen-reader description. Radix warns when a dialog has none. */
  description?: string;
  children?: ReactNode;
  footer?: ReactNode;
}

// A className constant rather than an <Overlay /> wrapper component: Radix's Presence clones
// the Portal's child to attach a ref, and a plain function component silently drops it
// ("Function components cannot be given refs"), which breaks the mount/unmount animation
// tracking the overlay depends on.
const OVERLAY = "fixed inset-0 z-40 bg-surface-sunken/70 backdrop-blur-sm";

/**
 * Restores focus to whatever was focused before the dialog opened.
 *
 * Radix returns focus to `Dialog.Trigger` on close. These dialogs are CONTROLLED and often
 * opened programmatically — from a table row action, a menu item, a keyboard shortcut — so
 * there is no Trigger, `triggerRef` is null, and Radix drops focus to <body>. A keyboard user
 * who pressed Escape would be silently returned to the top of the document, which is the exact
 * class of defect Radix is here to prevent and which is invisible to a mouse user.
 *
 * Found by the focus test, not by review.
 */
function useFocusRestore(open: boolean) {
  const previous = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (open) {
      previous.current = document.activeElement as HTMLElement | null;
    }
  }, [open]);

  return (event: Event) => {
    const target = previous.current;
    // Only intervene when the element is still around and focusable; otherwise let Radix do
    // whatever it would have done, rather than forcing focus onto a detached node.
    if (target && document.body.contains(target)) {
      event.preventDefault();
      target.focus();
    }
  };
}

function Header({ title, description }: { title: string; description?: string }) {
  return (
    <div className="flex flex-col gap-1">
      <RadixDialog.Title className="text-lg font-semibold text-text">{title}</RadixDialog.Title>
      {description ? (
        <RadixDialog.Description className="text-sm text-text-muted">
          {description}
        </RadixDialog.Description>
      ) : null}
    </div>
  );
}

export function Dialog({ open, onOpenChange, title, description, children, footer }: BaseProps) {
  const restoreFocus = useFocusRestore(open);
  return (
    <RadixDialog.Root open={open} onOpenChange={onOpenChange}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className={OVERLAY} />
        <RadixDialog.Content
          onCloseAutoFocus={restoreFocus}
          className={cn(
            "fixed z-50 flex w-[min(32rem,calc(100vw-2rem))] flex-col gap-4",
            "top-1/2 start-1/2 -translate-y-1/2 -translate-x-1/2 rtl:translate-x-1/2",
            "card p-6 shadow-lg",
          )}
        >
          <Header title={title} description={description} />
          {children}
          {footer ? <div className="flex justify-end gap-2">{footer}</div> : null}
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}

/** A panel anchored to the inline-end edge — the inspector/detail pattern. */
export function Sheet({ open, onOpenChange, title, description, children, footer }: BaseProps) {
  const restoreFocus = useFocusRestore(open);
  return (
    <RadixDialog.Root open={open} onOpenChange={onOpenChange}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className={OVERLAY} />
        <RadixDialog.Content
          onCloseAutoFocus={restoreFocus}
          className={cn(
            "fixed inset-block-0 end-0 z-50 flex w-[min(28rem,100vw)] flex-col gap-4",
            "border-s border-border bg-surface p-6 shadow-lg",
          )}
          style={{ insetBlock: 0 }}
        >
          <Header title={title} description={description} />
          <div className="flex-1 overflow-y-auto">{children}</div>
          {footer ? <div className="flex justify-end gap-2">{footer}</div> : null}
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}

export const DialogClose = RadixDialog.Close;
