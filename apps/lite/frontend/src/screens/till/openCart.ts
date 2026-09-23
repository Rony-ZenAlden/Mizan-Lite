import type { Customer } from "@/api/client";
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
    const lines = validLines(saved.lines);
    return lines.length > 0 ? lines : null;
  } catch {
    return null;
  }
}

/** Forgets the open cart — after a sale, or when the cashier clears it. */
export function forgetCart(): void {
  keepCart([]);
}

/** A cart put aside for a moment — the customer went back for something — while the next one is served (2026-09-23). */
export interface HeldCart {
  id: string;
  at: number;
  lines: CartEntry[];
  saleDiscount: string;
  payment: "cash" | "credit";
  /** The credit customer the cart was for, if any — kept whole, so putting the cart back puts back who it was for. */
  customer: Customer | null;
}

const HELD_KEY = "mizan.lite.till.held";

/** How many carts may wait at once. A counter with more than this waiting has stopped serving and started storing. */
export const MAX_HELD = 9;

/** How long a held cart is kept. The same day as a recovered cart: a cart held overnight belongs to nobody. */
const HELD_MAX_AGE_MS = MAX_AGE_MS;

/** The carts waiting, oldest first. Unreadable or stale ones are dropped. Never throws. */
export function heldCarts(): HeldCart[] {
  try {
    const raw = window.localStorage.getItem(HELD_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    const now = Date.now();
    return parsed.filter(
      (h): h is HeldCart =>
        !!h &&
        typeof h.id === "string" &&
        typeof h.at === "number" &&
        now - h.at <= HELD_MAX_AGE_MS &&
        Array.isArray(h.lines) &&
        validLines(h.lines).length === h.lines.length &&
        h.lines.length > 0,
    );
  } catch {
    return [];
  }
}

function saveHeld(list: HeldCart[]): void {
  try {
    if (list.length === 0) window.localStorage.removeItem(HELD_KEY);
    else window.localStorage.setItem(HELD_KEY, JSON.stringify(list));
  } catch {
    // Held carts are a convenience at the counter, not a record: a till that cannot keep one still sells.
  }
}

let heldCounter = 0;

/** Puts a cart aside. Refused (returns null) when it is empty or MAX_HELD are already waiting. */
export function holdCart(cart: Omit<HeldCart, "id" | "at">): HeldCart | null {
  if (cart.lines.length === 0) return null;
  const list = heldCarts();
  if (list.length >= MAX_HELD) return null;
  heldCounter += 1;
  const held: HeldCart = { ...cart, id: `held-${Date.now()}-${heldCounter}`, at: Date.now() };
  saveHeld([...list, held]);
  return held;
}

/** Takes a held cart back out, removing it from the waiting list; null when it is no longer there. */
export function takeHeld(id: string): HeldCart | null {
  const list = heldCarts();
  const found = list.find((h) => h.id === id) ?? null;
  if (found) saveHeld(list.filter((h) => h.id !== id));
  return found;
}

/** Throws a held cart away — the customer left and is not coming back for it. */
export function dropHeld(id: string): void {
  saveHeld(heldCarts().filter((h) => h.id !== id));
}

function validLines(lines: unknown[]): CartEntry[] {
  return lines
    .filter(
      (l): l is CartEntry =>
        !!l &&
        typeof (l as CartEntry).productId === "string" &&
        typeof (l as CartEntry).quantity === "string" &&
        typeof (l as CartEntry).key === "string",
    )
    // A cart saved before open-priced items existed has no price on its lines; they were all catalogue-priced.
    .map((l) => ({ ...l, price: typeof l.price === "string" ? l.price : "" }));
}
