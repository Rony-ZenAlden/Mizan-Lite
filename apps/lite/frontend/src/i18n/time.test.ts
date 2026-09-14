import { describe, expect, it } from "vitest";
import { formatAge, formatDateTime } from "./time";

describe("formatAge", () => {
  it("words an age in both languages with the platform's plurals and Latin digits", () => {
    expect(formatAge(30, "en", "just now")).toBe("just now");
    expect(formatAge(-5, "en", "just now")).toBe("just now");
    expect(formatAge(5 * 60, "en", "just now")).toBe("5 minutes ago");
    expect(formatAge(3 * 3600, "en", "just now")).toBe("3 hours ago");
    expect(formatAge(3600, "en", "just now")).toBe("1 hour ago");
    expect(formatAge(26 * 3600, "en", "just now")).toBe("yesterday");
    expect(formatAge(3 * 86_400, "en", "just now")).toBe("3 days ago");
    const arabic = formatAge(3 * 3600, "ar", "الآن");
    expect(arabic).toMatch(/3/);
    expect(arabic).not.toMatch(/[٠-٩]/);
    expect(arabic).toMatch(/ساع/);
  });
});

describe("formatDateTime", () => {
  it("formats a timestamp with Latin digits, and leaves an unreadable one as it is", () => {
    expect(formatDateTime("2026-09-14T06:00:00.000Z", "ar")).not.toMatch(/[٠-٩]/);
    expect(formatDateTime("2026-09-14T06:00:00.000Z", "en")).toMatch(/2026/);
    expect(formatDateTime("not a time", "en")).toBe("not a time");
  });
});
