import { describe, expect, it } from "vitest";
import { addToCart, plusOne, type Pick } from "./cart";

const jar: Pick = { productId: "jar", nameAr: "مربى", nameEn: "Jam", unitCode: "jar", unitDecimals: 0 };
const bulgur: Pick = { productId: "bulgur", nameAr: "برغل", nameEn: "Bulgur", unitCode: "kg", unitDecimals: 3 };

describe("the cart", () => {
  it("counts a whole count up by one, in any digits, beyond a Number's exact range", () => {
    expect(plusOne("2")).toBe("3");
    expect(plusOne("٩")).toBe("10");
    expect(plusOne("9007199254740993")).toBe("9007199254740994");
  });

  it("refuses to count up anything that is not a whole count", () => {
    expect(plusOne("1.5")).toBeNull();
    expect(plusOne("")).toBeNull();
    expect(plusOne("abc")).toBeNull();
  });

  it("a counted product added again goes up by one on its line", () => {
    const once = addToCart([], jar, "1");
    const twice = addToCart(once, jar, "1");
    expect(twice).toHaveLength(1);
    expect(twice[0]!.quantity).toBe("2");
    expect(twice[0]!.key).toBe(once[0]!.key);
  });

  it("a line with a discount typed, or a quantity that is not a count, is left alone and a new line added", () => {
    const discounted = addToCart([], jar, "1").map((l) => ({ ...l, discountPercent: "10" }));
    expect(addToCart(discounted, jar, "1")).toHaveLength(2);
    const odd = addToCart([], jar, "1").map((l) => ({ ...l, quantity: "x" }));
    expect(addToCart(odd, jar, "1")).toHaveLength(2);
  });

  it("each weighing is its own line", () => {
    const cart = addToCart(addToCart([], bulgur, "1.750"), bulgur, "0.500");
    expect(cart.map((l) => l.quantity)).toEqual(["1.750", "0.500"]);
    expect(new Set(cart.map((l) => l.key)).size).toBe(2);
  });
});
