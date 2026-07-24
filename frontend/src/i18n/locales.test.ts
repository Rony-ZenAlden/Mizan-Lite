import { describe, expect, it } from "vitest";
import { MESSAGES } from "./locales";

// Every locale must define exactly the same keys — no missing translations, no
// orphans. This is the i18n completeness gate (PHASE_0_FOUNDATION.md §TEST).
describe("i18n key coverage", () => {
  const locales = Object.keys(MESSAGES) as (keyof typeof MESSAGES)[];
  const reference = Object.keys(MESSAGES.en).sort();

  it("has at least one key", () => {
    expect(reference.length).toBeGreaterThan(0);
  });

  for (const locale of locales) {
    it(`locale "${locale}" matches the reference key set`, () => {
      expect(Object.keys(MESSAGES[locale]).sort()).toEqual(reference);
    });
  }
});
