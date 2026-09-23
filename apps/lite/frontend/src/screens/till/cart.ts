import { normaliseNumber } from "@/i18n/numbers";

/** What the till knows of a product it is adding. */
export interface Pick {
  productId: string;
  nameAr: string;
  nameEn: string;
  unitCode: string;
  unitDecimals: number;
  /** An item whose price the cashier types, never counted in stock (2026-09-23). */
  openPrice?: boolean;
  /** The currency that typed price is in — the product's own, which is the one Go reads it in. */
  priceCurrency?: string;
}

/** One cart line as the cashier has it: the quantity and discount exactly as typed. Go prices it. */
export interface CartEntry extends Pick {
  key: string;
  quantity: string;
  discountPercent: string;
  /** The price typed for an open-priced item, as the shop reads its money; "" for every catalogue-priced line. */
  price: string;
}

/**
 * One more of a counted product: "2" → "3". The only number the till changes itself, and only a whole count, in BigInt —
 * never a price, a weight or a total (DESIGN D9). Null when the typed quantity is not a whole count, so the caller adds a
 * new line instead of guessing.
 */
export function plusOne(quantity: string): string | null {
  const typed = normaliseNumber(quantity);
  if (!typed.ok || !/^\d+$/.test(typed.value)) return null;
  return (BigInt(typed.value) + 1n).toString();
}

/** One fewer of a counted product: "3" → "2". Null below 2 or when the quantity is not a whole count — the line is removed on purpose, not by a key. */
export function minusOne(quantity: string): string | null {
  const typed = normaliseNumber(quantity);
  if (!typed.ok || !/^\d+$/.test(typed.value) || BigInt(typed.value) < 2n) return null;
  return (BigInt(typed.value) - 1n).toString();
}

let counter = 0;

/**
 * Adds a product to the cart. A counted product already in the cart, with no discount typed on its line, goes up by one;
 * a weighed product always takes a new line with its quantity — two weighings are two lines, as on a scale's ticket.
 */
export function addToCart(cart: CartEntry[], pick: Pick, quantity: string, price = ""): CartEntry[] {
  // An open-priced item is never merged: two carrier bags at different prices are two things sold, and merging them would
  // charge the second at the first one's price.
  if (pick.unitDecimals === 0 && !pick.openPrice) {
    const index = cart.findIndex((l) => l.productId === pick.productId && l.discountPercent === "");
    const next = index >= 0 ? plusOne(cart[index]!.quantity) : null;
    if (index >= 0 && next !== null) {
      return cart.map((l, i) => (i === index ? { ...l, quantity: next } : l));
    }
  }
  counter += 1;
  return [...cart, { ...pick, key: `line-${counter}`, quantity, discountPercent: "", price }];
}
