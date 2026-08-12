/**
 * Sends a rendered document to the printer.
 *
 * # Why a hidden iframe and not window.open
 *
 * A popup is blocked, appears in the taskbar, steals focus, and leaves the operator on a blank
 * tab when they dismiss the print dialogue. At a till with a queue that is not a papercut — it is
 * the difference between serving the next customer and hunting for a window.
 *
 * An iframe prints from inside the page nobody left. The frame is removed after the dialogue
 * closes, which is what `afterprint` reports.
 *
 * # srcdoc, not a blob URL
 *
 * A blob: URL is a resource that must be revoked, and one that is not is a leak that grows with
 * every receipt on a machine that stays open for a fortnight. srcdoc holds the markup directly
 * and dies with the element.
 */
export function printHTML(html: string): void {
  const frame = document.createElement("iframe");

  // Off-screen rather than display:none. A frame that is not displayed has no layout in some
  // engines, and printing something with no layout prints a blank page.
  frame.setAttribute("aria-hidden", "true");
  frame.style.position = "fixed";
  frame.style.right = "0";
  frame.style.bottom = "0";
  frame.style.width = "0";
  frame.style.height = "0";
  frame.style.border = "0";
  frame.srcdoc = html;

  frame.onload = () => {
    const view = frame.contentWindow;
    if (!view) {
      frame.remove();
      return;
    }

    // Removed when the dialogue closes. A one-shot listener, because a second print of the same
    // frame would otherwise remove it mid-flight.
    view.addEventListener("afterprint", () => frame.remove(), { once: true });

    view.focus();
    view.print();

    // Some engines never fire afterprint. A timer is the backstop: leaving frames attached is
    // how a long-running till accumulates hundreds of them.
    window.setTimeout(() => frame.remove(), 60_000);
  };

  document.body.appendChild(frame);
}
