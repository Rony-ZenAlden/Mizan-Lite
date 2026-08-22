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

/**
 * The core/advanced split (Step 10.17).
 *
 * Thirty-one routes in one column is a list nobody scans, and the 10.13 grouping did not fix it:
 * six headings over thirty items is still thirty items to read. The sidebar now shows the screens
 * a shop uses every day and folds the rest into one collapsible section.
 *
 * These tests exist because the split degrades silently. Nothing breaks when the core list grows
 * — it just gets longer, one route at a time, until it is the flat list again.
 */
describe("the core navigation", () => {
  const core = ROUTES.filter((route) => !route.advanced);
  const advanced = ROUTES.filter((route) => route.advanced);

  it("stays short enough to take in at a glance", () => {
    // Ten is where a list stops being a set of choices and starts being something to search. A
    // route added without a decision about its tier lands here and fails THIS test, which is the
    // point: the omission is loud rather than quiet.
    expect(
      core.length,
      `the daily list has ${core.length} items: ${core.map((r) => r.path).join(", ")}`,
    ).toBeLessThanOrEqual(10);
  });

  it("holds the screens a shop actually opens every day", () => {
    const paths = core.map((route) => route.path);
    // The till, what was sold, who owes, what is on the shelf. If any of these ever moves into
    // the fold, somebody has to argue for it here.
    for (const daily of ["/pos", "/sales/invoices", "/partners/customers", "/catalog", "/inventory/stock"]) {
      expect(paths, `${daily} belongs in the daily list`).toContain(daily);
    }
  });

  it("keeps the guide reachable without opening anything", () => {
    // The one screen a user who can do nothing else must still find, because it is where they
    // learn what they are looking at. Burying it inside a fold labelled "Advanced" is precisely
    // backwards.
    expect(core.map((route) => route.path)).toContain("/help");
  });

  it("actually folds most of the application away", () => {
    // A split that moved three items would be a rename, not a simplification.
    expect(advanced.length).toBeGreaterThan(core.length);
  });

  it("gives every advanced route a group to sit under", () => {
    // The fold is still grouped inside — twenty-one items do need telling apart. A route with a
    // group the sidebar does not render would simply vanish from the menu.
    for (const route of advanced) {
      expect(NAV_GROUPS, `${route.path} has group "${route.group}"`).toContain(route.group);
    }
  });

  it("leaves no route in both tiers or neither", () => {
    expect(core.length + advanced.length).toBe(ROUTES.length);
  });
});
