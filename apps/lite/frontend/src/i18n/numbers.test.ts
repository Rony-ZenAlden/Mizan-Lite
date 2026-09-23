import { describe, expect, it } from "vitest";
import { formatInteger, typeable } from "./numbers";

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

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { formatCountdown, formatDecimal, latinDigits, normaliseNumber, quantityProblem } from "./numbers";

// The TypeScript half of the contract. Go reads the same file (internal/lite/numinput); a case added there is
// enforced here, and an implementation that disagrees fails its own suite (L1 §6, D-L1.4).
const FIXTURE = resolve(process.cwd(), "../../../internal/lite/numinput/testdata/cases.json");

interface Fixture {
  normalise: { input: string; output?: string; error?: string }[];
  quantities: { input: string; decimals: number; error?: string }[];
  latinDigits: { input: string; output: string }[];
}

describe("the shared number fixture", () => {
  const fixture = JSON.parse(readFileSync(FIXTURE, "utf8")) as Fixture;

  it("loaded (a gate with no input checks nothing)", () => {
    expect(fixture.normalise.length).toBeGreaterThan(20);
    expect(fixture.latinDigits.length).toBeGreaterThan(2);
  });

  it.each(fixture.quantities.map((c) => [JSON.stringify(c.input), c.decimals, c] as const))(
    "quantityProblem(%s, %d decimals)",
    (_, __, c) => {
      expect(quantityProblem(c.input, c.decimals)).toBe(c.error ?? null);
    },
  );

  it.each(fixture.normalise.map((c) => [JSON.stringify(c.input), c] as const))("normaliseNumber(%s)", (_, c) => {
    const got = normaliseNumber(c.input);
    if (c.error) {
      expect(got).toEqual({ ok: false, code: c.error });
    } else {
      expect(got).toEqual({ ok: true, value: c.output });
    }
  });

  it.each(fixture.latinDigits.map((c) => [JSON.stringify(c.input), c] as const))("latinDigits(%s)", (_, c) => {
    expect(latinDigits(c.input)).toBe(c.output);
  });
});

describe("formatDecimal", () => {
  it("groups the integer part and copies the fraction without rounding", () => {
    expect(formatDecimal("45000", "en")).toBe("45,000");
    expect(formatDecimal("3.25", "en")).toBe("3.25");
    expect(formatDecimal("1234567.255", "en")).toBe("1,234,567.255");
  });

  it("uses Latin digits in Arabic", () => {
    expect(formatDecimal("45000.50", "ar")).not.toMatch(NON_LATIN_DIGITS);
    expect(formatDecimal("45000.50", "ar").replace(/[^\d.]/g, "")).toBe("45000.50");
  });

  it("keeps a value past Number's exact range", () => {
    expect(formatDecimal("9007199254740993.01", "en").replace(/,/g, "")).toBe("9007199254740993.01");
  });
});

describe("formatCountdown", () => {
  it("shows minutes and zero-padded seconds, never negative", () => {
    expect(formatCountdown(120)).toBe("2:00");
    expect(formatCountdown(61)).toBe("1:01");
    expect(formatCountdown(5)).toBe("0:05");
    expect(formatCountdown(-3)).toBe("0:00");
  });
});

describe("typeable — the figure a form starts from (0.9.9)", () => {
  it("is the new pound of a dual reading, which is what a person types in dual mode", () => {
    expect(typeable("150 (15000)")).toBe("150");
    expect(typeable("136.75 (13675)")).toBe("136.75");
  });

  it("is a single figure as it is", () => {
    expect(typeable("15000")).toBe("15000");
    expect(typeable("3.25")).toBe("3.25");
    expect(typeable("")).toBe("");
  });
});
