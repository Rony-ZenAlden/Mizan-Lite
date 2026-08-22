import { describe, expect, it } from "vitest";
import { NAV_GROUPS, ROUTES } from "@/app/shell/routes";

/**
 * The sidebar's grouping (Step 10.13).
 *
 * The 10.12 audit counted thirty items in one flat column — a list nobody scans, where the
 * entries near the bottom effectively do not exist.
 *
 * The group lives on the ROUTE, so there is no second list to keep in step. These tests are what
 * stop that one list drifting.
 */
describe("navigation", () => {
  it("puts every route in a known group", () => {
    for (const route of ROUTES) {
      expect(
        NAV_GROUPS,
        `${route.path} has group "${route.group}", which the sidebar does not render`,
      ).toContain(route.group);
    }
  });

  it("keeps every group small enough to scan", () => {
    for (const group of NAV_GROUPS) {
      const items = ROUTES.filter((route) => route.group === group);
      // Eight is where a grouped list starts being a flat one again. If a group outgrows it, the
      // answer is a new group, not a longer column.
      expect(items.length, `the "${group}" group has ${items.length} items`).toBeLessThanOrEqual(8);
    }
  });

  it("fills every group it declares", () => {
    // An empty group renders a heading with nothing under it, which reads as a broken menu.
    for (const group of NAV_GROUPS) {
      expect(
        ROUTES.some((route) => route.group === group),
        `the "${group}" group has no routes`,
      ).toBe(true);
    }
  });

  it("has no duplicate paths", () => {
    const paths = ROUTES.map((route) => route.path);
    expect(new Set(paths).size).toBe(paths.length);
  });

  it("routes the screens a shop cannot operate without", () => {
    // Each of these was unreachable at some point in this project's history, and each was found
    // by somebody going looking rather than by a check. This is the check.
    const paths = new Set(ROUTES.map((route) => route.path));
    for (const essential of [
      "/pos",                    // selling
      "/catalog",                // registering what you sell
      "/inventory/stock",        // what is on the shelf
      "/purchasing/orders",      // ordering
      "/purchasing/receive",     // TAKING GOODS IN — had no screen until 10.13
      "/purchasing/bills",       // being invoiced
      "/partners/customers",     // who you sell to
      "/reports/statements",     // whether you made money
      "/operations/backups",     // not losing it all
      "/help",                   // learning any of the above
    ]) {
      expect(paths, `${essential} is not routed`).toContain(essential);
    }
  });
});
