import { describe, expect, it } from "vitest";
import { formatInteger } from "./numbers";

// Arabic-Indic (U+0660–0669) and Extended Arabic-Indic (U+06F0–06F9) digits.
const NON_LATIN_DIGITS = /[٠-٩۰-۹]/;

describe("formatInteger", () => {
  it("uses Latin digits in Arabic, per the owner's decision (Q6)", () => {
    const formatted = formatInteger(1234567, "ar");
    expect(formatted).not.toMatch(NON_LATIN_DIGITS);
    expect(formatted.replace(/\D/g, "")).toBe("1234567");
  });

  it("uses Latin digits in English", () => {
    expect(formatInteger(1234567, "en")).toBe("1,234,567");
  });

  it("formats a string beyond Number's exact range without losing a digit", () => {
    const beyond = "9007199254740993"; // 2^53 + 1 — a Number would round this to ...992
    expect(formatInteger(beyond, "en").replace(/\D/g, "")).toBe(beyond);
    expect(formatInteger(beyond, "ar").replace(/\D/g, "")).toBe(beyond);
  });

  it("formats zero and negatives", () => {
    expect(formatInteger(0, "ar")).toBe("0");
    expect(formatInteger(-42, "en").replace(/[^\d-]/g, "")).toBe("-42");
  });
});
