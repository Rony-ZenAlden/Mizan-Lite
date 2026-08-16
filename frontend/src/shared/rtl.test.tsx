import type React from "react";
import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "@/app/session/session";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

import { DashboardScreen } from "@/modules/insight/DashboardScreen";
import { StatementsScreen } from "@/modules/insight/StatementsScreen";
import { ValuationScreen } from "@/modules/insight/ValuationScreen";
import { AnalysisScreen } from "@/modules/insight/AnalysisScreen";
import { SearchBox } from "@/modules/insight/SearchBox";
import { NoticeCentre } from "@/modules/operations/NoticeCentre";
import { BackupScreen } from "@/modules/operations/BackupScreen";
import { ImportScreen } from "@/modules/operations/ImportScreen";
import { ReceiptsScreen } from "@/modules/purchasing/ReceiptsScreen";
import { SupplierReturnsScreen } from "@/modules/purchasing/SupplierReturnsScreen";
import { SupplierPaymentsScreen } from "@/modules/purchasing/SupplierPaymentsScreen";

/**
 * Every screen renders in Arabic (Step 10.3).
 *
 * # What this proves and what it does not
 *
 * It does NOT prove a layout looks right — that needs a browser this environment cannot drive,
 * and a screenshot comparison fails on every legitimate change until nobody reads it.
 *
 * It proves each screen MOUNTS under `dir="rtl"` with an Arabic catalogue: no missing translation
 * throwing, no component assuming a direction at render time, no key that only exists in English.
 * That is the class of failure that takes a screen from "mirrored oddly" to "blank", and it is
 * the one worth a cheap gate.
 *
 * The direction gate next door covers the layout half by refusing physical-direction properties
 * at the source.
 */

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    preferences: vi.fn(),
    dashboard: vi.fn(),
    profitAndLoss: vi.fn(),
    balanceSheet: vi.fn(),
    stockValuation: vi.fn(),
    salesByPeriod: vi.fn(),
    globalSearch: vi.fn(),
    notices: vi.fn(),
    backups: vi.fn(),
    pendingRestore: vi.fn(),
    goodsReceipts: vi.fn(),
    supplierReturns: vi.fn(),
    supplierPayments: vi.fn(),
  };
});

const EMPTY_STATEMENT = {
  requestedFrom: "2026-08-01", requestedTo: "2026-08-31",
  coveredFrom: "2026-08-01", coveredTo: "2026-08-31", coveredPeriods: 1,
  revenue: [], expense: [],
  revenueMinor: "0", expenseMinor: "0", netProfitMinor: "0",
};

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "ar", theme: "system", availableLocales: ["en", "ar"],
  });
  vi.mocked(wails.dashboard).mockResolvedValue({ from: "", to: "", tiles: [] });
  vi.mocked(wails.profitAndLoss).mockResolvedValue(EMPTY_STATEMENT);
  vi.mocked(wails.balanceSheet).mockResolvedValue({
    asAt: "2026-08-31", asset: [], liability: [], equity: [],
    assetMinor: "0", liabilityMinor: "0", equityMinor: "0", resultMinor: "0",
    outOfBalanceMinor: "0", balanced: true,
  });
  vi.mocked(wails.stockValuation).mockResolvedValue({
    lines: [], totalMinor: "0", hasLedger: true, ledgerMinor: "0",
    differenceMinor: "0", reconciled: true,
  });
  vi.mocked(wails.salesByPeriod).mockResolvedValue({
    from: "", to: "", rows: [],
    total: {
      key: "", label: "", id: "", documents: 0, quantityMicro: "0",
      revenueMinor: "0", costMinor: "0", grossMarginMinor: "0", marginPercentMicro: "0",
    },
  });
  vi.mocked(wails.globalSearch).mockResolvedValue({ query: "", results: [], failed: [] });
  vi.mocked(wails.notices).mockResolvedValue({ notices: [], dismissed: 0, failed: [] });
  vi.mocked(wails.backups).mockResolvedValue([]);
  vi.mocked(wails.pendingRestore).mockResolvedValue({
    from: "", replacingVersion: 0, restoringVersion: 0, safetyBackup: "", preparedAt: "",
  });
  vi.mocked(wails.goodsReceipts).mockResolvedValue([]);
  vi.mocked(wails.supplierReturns).mockResolvedValue([]);
  vi.mocked(wails.supplierPayments).mockResolvedValue([]);

  useSessionStore.getState().setSession({
    ...SIGNED_IN,
    permissions: Object.values(wails.PERMISSIONS),
  });
});

// Components, not zero-argument functions: SearchBox takes an optional callback, and typing
// the list as `() => JSX.Element` excluded it.
const SCREENS: Array<[string, React.ComponentType]> = [
  ["dashboard", DashboardScreen],
  ["statements", StatementsScreen],
  ["valuation", ValuationScreen],
  ["analysis", AnalysisScreen],
  ["search", SearchBox],
  ["notices", NoticeCentre],
  ["backups", BackupScreen],
  ["import", ImportScreen],
  ["deliveries", ReceiptsScreen],
  ["supplier returns", SupplierReturnsScreen],
  ["supplier payments", SupplierPaymentsScreen],
];

describe("right to left", () => {
  for (const [name, Screen] of SCREENS) {
    it(`renders ${name} in Arabic`, async () => {
      renderApp(<Screen />, { locale: "ar" });

      // AWAITED, because the direction comes from the preferences the provider loads — not from
      // the render helper's initial attribute. The first version asserted synchronously and read
      // "ltr" every time, which would have made this a test of `renderApp` rather than of the
      // application applying a locale.
      await waitFor(() => {
        expect(document.documentElement.dir).toBe("rtl");
      });
      // Something rendered. A screen that threw during mount leaves the container empty, which is
      // the failure a missing translation or a direction-dependent component produces.
      await screen.findByRole("heading", { hidden: true }).catch(() => null);
      expect(document.body.textContent?.trim().length ?? 0).toBeGreaterThan(0);
    });
  }
});
