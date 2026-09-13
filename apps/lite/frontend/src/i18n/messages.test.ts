import { describe, expect, it } from "vitest";
import { DEFAULT_LOCALE, directionOf, documentLocale, isLocale, translate, translateDynamic } from "./messages";

describe("messages", () => {
  it("opens in Arabic by default", () => {
    expect(DEFAULT_LOCALE).toBe("ar");
  });

  it("maps each locale to its direction", () => {
    expect(directionOf("ar")).toBe("rtl");
    expect(directionOf("en")).toBe("ltr");
  });

  it("translates in both languages", () => {
    expect(translate("ar", "app.name")).toBe("ميزان لايت");
    expect(translate("en", "app.name")).toBe("Mizan Lite");
  });

  it("interpolates {name} placeholders and leaves unknown ones visible", () => {
    expect(translate("en", "boot.progress", { current: "2", total: "5" })).toBe("Step 2 of 5");
    expect(translate("ar", "boot.progress", { current: "2", total: "5" })).toBe("الخطوة 2 من 5");
    expect(translate("en", "boot.progress", { current: "2" })).toBe("Step 2 of {total}");
  });

  it("returns the key itself for an unknown runtime key", () => {
    expect(translateDynamic("ar", "lite.no_such_key")).toBe("lite.no_such_key");
  });

  it("reads the language the document was served in, defaulting to Arabic", () => {
    document.documentElement.lang = "en";
    expect(documentLocale()).toBe("en");
    document.documentElement.lang = "fr";
    expect(documentLocale()).toBe("ar");
    document.documentElement.removeAttribute("lang");
    expect(documentLocale()).toBe("ar");
  });

  it("recognises only shipped locales", () => {
    expect(isLocale("ar")).toBe(true);
    expect(isLocale("en")).toBe(true);
    expect(isLocale("ar-SY")).toBe(false);
    expect(isLocale(undefined)).toBe(false);
  });
});
