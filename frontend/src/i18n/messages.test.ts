import { describe, expect, it } from "vitest";
import { DIRECTION, MESSAGES, translate } from "./messages";

// The frontend half of the i18n completeness gates. The Go half
// (internal/platform/i18n/coverage_test.go) checks the same catalogs from the other side,
// including that every error code has a translation.
describe("i18n key coverage", () => {
  const locales = Object.keys(MESSAGES) as (keyof typeof MESSAGES)[];
  const reference = Object.keys(MESSAGES.en).sort();

  it("loads the shared catalogs", () => {
    // If the JSON import path breaks, this is where it shows up rather than as blank UI.
    expect(reference.length).toBeGreaterThan(50);
    expect(MESSAGES.en["app.title"]).toBe("Mizan");
  });

  for (const locale of locales) {
    it(`locale "${locale}" matches the reference key set`, () => {
      expect(Object.keys(MESSAGES[locale]).sort()).toEqual(reference);
    });
  }

  it("includes error codes, not just UI vocabulary", () => {
    // The backend returns codes and the frontend renders them (§22.2), so the codes must be
    // present on this side.
    expect(MESSAGES.en["migrate.checksum_mismatch"]).toBeTruthy();
    expect(MESSAGES.ar["migrate.checksum_mismatch"]).toBeTruthy();
  });

  it("has no untranslated Arabic entries", () => {
    // A copy-paste that left English text in the Arabic catalog is invisible to a
    // non-Arabic-speaking reviewer, so it is checked mechanically.
    //
    // Language names are the one legitimate exception: they are ENDONYMS, written in their own
    // language whatever the surrounding UI is. A user hunting for Arabic is looking for
    // "العربية", not for "Arabic" transliterated into a script they may not read. Listed
    // explicitly rather than pattern-matched, so a new exception has to be argued for.
    const ENDONYMS = ["locale.name.en", "locale.name.ar"];

    const identical = reference.filter(
      (key) =>
        !ENDONYMS.includes(key) &&
        MESSAGES.ar[key] === MESSAGES.en[key] &&
        MESSAGES.en[key]!.length > 3,
    );
    // Anything else identical in both languages is almost certainly a missed translation.
    expect(identical).toEqual([]);
  });
});

describe("direction", () => {
  it("marks Arabic RTL and English LTR", () => {
    expect(DIRECTION.ar).toBe("rtl");
    expect(DIRECTION.en).toBe("ltr");
  });
});

describe("translate", () => {
  it("resolves a key in the requested locale", () => {
    expect(translate("ar", "app.title")).toBe("ميزان");
    expect(translate("en", "app.title")).toBe("Mizan");
  });

  it("returns the key when nothing defines it", () => {
    expect(translate("en", "does.not.exist")).toBe("does.not.exist");
  });

  it("interpolates parameters and leaves unknown ones visible", () => {
    expect(
      translate("en", "migrate.database_too_new", { current: "5", target: "3" }),
    ).toContain("5");
    // Mirrors the Go resolver: a stray placeholder is a bug report, a dropped one a mystery.
    expect(translate("en", "migrate.database_too_new", { current: "5" })).toContain("{target}");
  });
});
