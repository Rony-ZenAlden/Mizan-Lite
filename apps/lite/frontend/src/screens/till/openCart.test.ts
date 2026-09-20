import { beforeEach, describe, expect, it } from "vitest";
import type { CartEntry } from "./cart";
import { forgetCart, keepCart, recoverCart } from "./openCart";

const line = (over: Partial<CartEntry> = {}): CartEntry => ({
  key: "line-1",
  productId: "p-1",
  nameAr: "زيت زيتون",
  nameEn: "Olive oil",
  unitCode: "l",
  unitDecimals: 3,
  quantity: "2",
  discountPercent: "",
  ...over,
});

describe("the open cart survives a restart (owner's request, 2026-09-20)", () => {
  beforeEach(() => window.localStorage.clear());

  it("comes back with what was picked and how much of it", () => {
    keepCart([line(), line({ key: "line-2", productId: "p-2", quantity: "0.5" })]);
    const back = recoverCart();
    expect(back).toHaveLength(2);
    expect(back?.[0]?.productId).toBe("p-1");
    expect(back?.[1]?.quantity).toBe("0.5");
  });

  it("keeps no prices, so a recovered cart is priced again at today's rate", () => {
    keepCart([line()]);
    const raw = window.localStorage.getItem("mizan.lite.till.cart") ?? "";
    // The rate moves daily in this shop: a restored total would be a quiet lie about what the customer owes.
    expect(raw).not.toMatch(/price|total|refund|rate/i);
  });

  it("an empty cart is forgotten rather than stored", () => {
    keepCart([line()]);
    keepCart([]);
    expect(recoverCart()).toBeNull();
    keepCart([line()]);
    forgetCart();
    expect(recoverCart()).toBeNull();
  });

  it("a cart from yesterday is not offered — it is somebody else's, and the shop has moved on", () => {
    window.localStorage.setItem(
      "mizan.lite.till.cart",
      JSON.stringify({ at: Date.now() - 13 * 60 * 60 * 1000, lines: [line()] }),
    );
    expect(recoverCart()).toBeNull();
    // And it is cleared, so it cannot come back later.
    expect(window.localStorage.getItem("mizan.lite.till.cart")).toBeNull();
  });

  it("nonsense in storage is discarded rather than crashing the till", () => {
    for (const junk of ["", "{", "null", "[]", '{"at":"soon","lines":[]}', '{"at":1,"lines":"two"}']) {
      window.localStorage.setItem("mizan.lite.till.cart", junk);
      expect(recoverCart()).toBeNull();
    }
    // A line missing what a line needs is dropped, not half-restored.
    window.localStorage.setItem("mizan.lite.till.cart", JSON.stringify({ at: Date.now(), lines: [{ productId: 1 }] }));
    expect(recoverCart()).toBeNull();
  });
});
