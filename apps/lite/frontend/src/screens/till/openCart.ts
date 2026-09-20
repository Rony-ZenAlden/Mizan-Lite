import type { CartEntry } from "./cart";

/**
 * The cart the till had open, kept so a restart does not lose it (the owner's request, 2026-09-20).
 *
 * # Why the browser's storage and not the database
 *
 * An open cart is not a fact about the shop. Nothing has been sold, no stock has moved, no money has changed hands —
 * it is what one person had half-typed when the power went. Writing it to the books would put a row in the database
 * that no report should ever count, and every reader of those tables would have to learn to skip it.
 *
 * So it lives where the half-typed thing belongs: in this computer's webview, beside the till. It survives a restart
 * and a power cut, which is what was asked for, and it cannot reach a report.
 *
 * # What is NOT kept
 *
 * Prices. The cart holds what was picked and how much of it, exactly as the cashier typed it; Go prices it again on
 * the next quote. A restored cart at yesterday's price would be a quiet lie about what the customer owes today, and
 * the exchange rate moves daily in this shop.
 */
const KEY = "mizan.lite.till.cart";

/** How long a recovered cart is worth offering. A cart from last week is somebody else's, and the shop has moved on. */
const MAX_AGE_MS = 12 * 60 * 60 * 1000;

interface Saved {
  at: number;
  lines: CartEntry[];
}

/** Keeps the open cart, or clears it when the cart is empty. Never throws: storage can be unavailable or full. */
export function keepCart(lines: CartEntry[]): void {
  try {
    if (lines.length === 0) {
      window.localStorage.removeItem(KEY);
      return;
    }
    window.localStorage.setItem(KEY, JSON.stringify({ at: Date.now(), lines } satisfies Saved));
  } catch {
    // A till that cannot save its cart still sells. This is a convenience, not a record.
  }
}

/** The cart that was open, or null. Anything unreadable, stale or not shaped like a cart is discarded. */
export function recoverCart(): CartEntry[] | null {
  try {
    const raw = window.localStorage.getItem(KEY);
    if (!raw) return null;
    const saved = JSON.parse(raw) as Partial<Saved>;
    if (typeof saved?.at !== "number" || Date.now() - saved.at > MAX_AGE_MS || !Array.isArray(saved.lines)) {
      window.localStorage.removeItem(KEY);
      return null;
    }
    const lines = saved.lines.filter(
      (l): l is CartEntry =>
        !!l && typeof l.productId === "string" && typeof l.quantity === "string" && typeof l.key === "string",
    );
    return lines.length > 0 ? lines : null;
  } catch {
    return null;
  }
}

/** Forgets the open cart — after a sale, or when the cashier clears it. */
export function forgetCart(): void {
  keepCart([]);
}
