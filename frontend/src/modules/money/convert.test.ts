import { describe, expect, it } from "vitest";
import { convertMinor, divideMinor } from "./convert";

/**
 * The conversion arithmetic.
 *
 * # Why this is tested this hard for a figure nothing is posted from
 *
 * The second line on a till is read by a person deciding what to charge. It being quietly wrong
 * is worse than it being absent, because nobody checks a number the computer produced.
 *
 * And the failure mode is specific: every value here is a decimal string of an exact integer, and
 * the moment one passes through `Number` it is silently approximated. `Number` holds integers
 * exactly only to 2^53; a Syrian pound amount times a rate in the thousands passes that at a few
 * hundred thousand pounds.
 */
describe("convertMinor", () => {
  it("multiplies exactly at a rate a shop actually uses", () => {
    // 100.00 USD at 15,000 → 1,500,000.00 SYP
    expect(convertMinor("10000", "15000000000000")).toBe("150000000");
  });

  it("stays exact far past what a float could hold", () => {
    /*
     * 2^53 is about 9×10¹⁵. This product is around 10²², so a float implementation does not
     * merely round the last digit — it loses thousands.
     *
     * The expected value is the exact integer, written out. If this ever comes back with a
     * trailing run of zeros where digits should be, something has become a Number.
     */
    const amount = "999999999999";        // ~10 billion major units
    const rate = "15000000000000";        // 15,000
    expect(convertMinor(amount, rate)).toBe("14999999999985000");
  });

  it("rounds half away from zero, symmetrically", () => {
    // A refund must land on the figure that cancels the sale it reverses. Rounding toward zero
    // would make -0.5 and +0.5 round to different magnitudes and the pair would stop netting.
    expect(convertMinor("1", "1500000000")).toBe("2");
    expect(convertMinor("-1", "1500000000")).toBe("-2");
    expect(convertMinor("1", "500000000")).toBe("1");
    expect(convertMinor("-1", "500000000")).toBe("-1");
  });

  it("returns nothing rather than guessing", () => {
    // A caller showing nothing is correct. A caller showing NaN, or zero, is lying.
    for (const bad of ["", " ", "abc", "1.5", "1e9", "--1"]) {
      expect(convertMinor(bad, "15000000000000"), `amount ${bad}`).toBeNull();
    }
    for (const bad of ["0", "-1", "", "x"]) {
      expect(convertMinor("10000", bad), `rate ${bad}`).toBeNull();
    }
  });
});

describe("divideMinor", () => {
  it("divides exactly at a rate a shop actually uses", () => {
    // 1,500,000.00 SYP at 15,000 → 100.00 USD
    expect(divideMinor("150000000", "15000000000000")).toBe("10000");
  });

  /*
   * # The bug this test exists because of
   *
   * The first version inverted the rate — 10¹⁸ / rateNano — and then multiplied. That inversion
   * TRUNCATES, and the truncation is then multiplied by the amount, so the error grows with the
   * figure.
   *
   * At 15,000 the inverted rate is 66,666,666,666,666 and change; the discarded remainder is
   * two thirds of a unit in the ninth decimal place. Invisible on one line, and it accumulates
   * down a page of them.
   */
  it("does not compound a truncated inverse", () => {
    const rate = "15000000000000";
    // Sixty lines of the same figure, converted one at a time and then together.
    const line = "150000000";
    let summed = 0n;
    for (let i = 0; i < 60; i += 1) {
      summed += BigInt(divideMinor(line, rate) as string);
    }
    const total = divideMinor((BigInt(line) * 60n).toString(), rate) as string;

    // Per-line and whole-total must agree. An inverted-rate implementation drifts here.
    expect(summed.toString()).toBe(total);
  });

  it("rounds half away from zero, symmetrically", () => {
    expect(divideMinor("3", "2000000000")).toBe("2");
    expect(divideMinor("-3", "2000000000")).toBe("-2");
  });

  it("returns nothing rather than dividing by zero", () => {
    expect(divideMinor("10000", "0")).toBeNull();
    expect(divideMinor("10000", "-5")).toBeNull();
    expect(divideMinor("oops", "15000000000000")).toBeNull();
  });

  it("round-trips a figure back to itself at a clean rate", () => {
    // Not a general property — rounding makes it false for arbitrary inputs — but it must hold
    // where the division is exact, or the two directions disagree about the same rate.
    const rate = "2000000000"; // 2
    const there = convertMinor("12345", rate) as string;
    expect(divideMinor(there, rate)).toBe("12345");
  });
});
