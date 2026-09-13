import { describe, expect, it } from "vitest";
import { ROUTES } from "@/app/routes";
import { CATALOGS } from "@/i18n/messages";

// Every module under src/screens, loaded eagerly so a screen file nobody imports is still seen.
const screenModules = import.meta.glob("../screens/**/*.tsx", { eager: true }) as Record<string, Record<string, unknown>>;

describe("G4 — screens, routes and navigation are one set", () => {
  it("finds the screens (a gate with no input checks nothing)", () => {
    const screens = Object.entries(screenModules).filter(([file]) => !/\.test\.tsx$/.test(file));
    expect(screens.length).toBeGreaterThanOrEqual(1);
  });

  it("every exported *Screen component is routed", () => {
    const routed = new Set(ROUTES.map((r) => r.Screen));
    const unrouted: string[] = [];
    for (const [file, exports] of Object.entries(screenModules)) {
      if (/\.test\.tsx$/.test(file)) continue;
      for (const [name, value] of Object.entries(exports)) {
        if (/Screen$/.test(name) && typeof value === "function" && !routed.has(value as never)) {
          unrouted.push(`${file}: ${name}`);
        }
      }
    }
    expect(unrouted, "a screen that exists and cannot be reached — Mizan 10.9's eleven unrouted screens").toEqual([]);
  });

  it("route paths are unique", () => {
    const paths = ROUTES.map((r) => r.path);
    expect(new Set(paths).size).toBe(paths.length);
  });

  it("every navigation label exists in both catalogs", () => {
    for (const route of ROUTES) {
      expect(CATALOGS.ar.common, route.path).toHaveProperty([route.labelKey]);
      expect(CATALOGS.en.common, route.path).toHaveProperty([route.labelKey]);
    }
  });
});
