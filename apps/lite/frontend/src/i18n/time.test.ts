import { describe, expect, it } from "vitest";
import { formatAge, formatDate, formatDateTime } from "./time";

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
  it("is day/month/year and a 24-hour time in the machine's zone, digits only, the same in both languages (Q-L8.6)", () => {
    const at = new Date(2026, 8, 15, 15, 5);
    expect(formatDateTime(at.toISOString())).toBe("15/09/2026 15:05");
    expect(formatDateTime(at.toISOString())).toBe("15/09/2026 15:05");
    expect(formatDateTime(at.toISOString())).toMatch(/^[0-9/: ]+$/);
    expect(formatDateTime("not a time")).toBe("not a time");
  });
});

describe("formatDate", () => {
  it("turns Go's business dates and months into what the shop reads", () => {
    expect(formatDate("2026-09-15")).toBe("15/09/2026");
    expect(formatDate("2026-09")).toBe("09/2026");
    expect(formatDate("")).toBe("");
    expect(formatDate("yesterday")).toBe("yesterday");
  });
});
