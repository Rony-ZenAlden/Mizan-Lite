import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "@/app/session/session";
import { DashboardScreen } from "@/modules/insight/DashboardScreen";
import { StatementsScreen } from "@/modules/insight/StatementsScreen";
import { ValuationScreen } from "@/modules/insight/ValuationScreen";
import { SearchBox } from "@/modules/insight/SearchBox";
import { KpiStrip } from "@/modules/insight/KpiStrip";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    preferences: vi.fn(),
    dashboard: vi.fn(),
    profitAndLoss: vi.fn(),
    balanceSheet: vi.fn(),
    stockValuation: vi.fn(),
    globalSearch: vi.fn(),
  };
});

const BALANCED_SHEET = {
  asAt: "2026-08-31",
  asset: [
    {
      accountId: "a1", code: "1000", name: "Assets", type: "asset", depth: 0,
      isPostable: false, amountMinor: "21000", children: [],
    },
  ],
  liability: [],
  equity: [],
  assetMinor: "21000", liabilityMinor: "0", equityMinor: "21000",
  resultMinor: "21000", outOfBalanceMinor: "0", balanced: true,
};

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en", theme: "system", availableLocales: ["en", "ar"],
  });
  useSessionStore.getState().setSession({
    ...SIGNED_IN,
    permissions: [
      wails.PERMISSIONS.profitAndLossView, wails.PERMISSIONS.balanceSheetView,
      wails.PERMISSIONS.salesAnalysisView, wails.PERMISSIONS.purchaseAnalysisView,
      wails.PERMISSIONS.valuationView, wails.PERMISSIONS.catalogView,
    ],
  });
});

describe("dashboard", () => {
  it("says which module answered and whether a tile is a period or a position", async () => {
    vi.mocked(wails.dashboard).mockResolvedValue({
      from: "2026-08-01", to: "2026-08-16",
      tiles: [
        {
          key: "sales.revenue", source: "sales", kind: "money",
          amountMinor: "45000", count: 0, periodic: true, failed: false,
        },
        {
          key: "inventory.stock_value", source: "inventory", kind: "money",
          amountMinor: "120000", count: 0, periodic: false, failed: false,
        },
      ],
    });

    renderApp(<DashboardScreen />);

    expect(await screen.findByText("Revenue")).toBeInTheDocument();
    expect(screen.getByText("From sales")).toBeInTheDocument();
    expect(screen.getByText("From inventory")).toBeInTheDocument();

    // The two ranges are labelled differently, which is the whole reason `periodic` crosses:
    // "stock value 1,200.00" read as this month's purchases is the failure being prevented.
    expect(screen.getByText("for the period")).toBeInTheDocument();
    expect(screen.getByText("as at now")).toBeInTheDocument();
  });

  it("says a tile could not be calculated rather than showing zero", async () => {
    vi.mocked(wails.dashboard).mockResolvedValue({
      from: "2026-08-01", to: "2026-08-16",
      tiles: [
        {
          key: "sales.revenue", source: "sales", kind: "money",
          amountMinor: "0", count: 0, periodic: true, failed: true,
        },
      ],
    });

    renderApp(<DashboardScreen />);

    // Zero is a real figure — a shop that sold nothing has revenue of zero — so a failure
    // rendered as one would be a lie the reader cannot detect.
    expect(await screen.findByText("Could not be calculated")).toBeInTheDocument();
    expect(screen.queryByText("0.00")).not.toBeInTheDocument();
  });
});

describe("statements", () => {
  it("says so when the figures cover more than the range asked for", async () => {
    vi.mocked(wails.profitAndLoss).mockResolvedValue({
      requestedFrom: "2026-03-01", requestedTo: "2026-03-15",
      coveredFrom: "2026-03-01", coveredTo: "2026-03-31", coveredPeriods: 1,
      revenue: [], expense: [],
      revenueMinor: "250000", expenseMinor: "40000", netProfitMinor: "210000",
    });
    vi.mocked(wails.balanceSheet).mockResolvedValue(BALANCED_SHEET);

    renderApp(<StatementsScreen />);

    expect(
      await screen.findByText("These figures cover whole accounting periods"),
    ).toBeInTheDocument();
  });

  it("reports an unbalanced sheet rather than hiding it", async () => {
    vi.mocked(wails.profitAndLoss).mockResolvedValue({
      requestedFrom: "2026-08-01", requestedTo: "2026-08-31",
      coveredFrom: "2026-08-01", coveredTo: "2026-08-31", coveredPeriods: 1,
      revenue: [], expense: [],
      revenueMinor: "0", expenseMinor: "0", netProfitMinor: "0",
    });
    vi.mocked(wails.balanceSheet).mockResolvedValue({
      ...BALANCED_SHEET, outOfBalanceMinor: "9000", balanced: false,
    });

    renderApp(<StatementsScreen />);

    // The books being wrong is the reason to draw the sheet, not a reason to refuse.
    expect(await screen.findByText("The books do not balance")).toBeInTheDocument();
    // The sections still RENDER. "Assets" appears twice — as the section heading and as the
    // account the fixture names — which is itself the evidence the sheet was drawn.
    expect(screen.getAllByText("Assets").length).toBeGreaterThan(1);
  });

  it("never labels either figure 'profit' on its own", async () => {
    vi.mocked(wails.profitAndLoss).mockResolvedValue({
      requestedFrom: "2026-08-01", requestedTo: "2026-08-31",
      coveredFrom: "2026-08-01", coveredTo: "2026-08-31", coveredPeriods: 1,
      revenue: [], expense: [],
      revenueMinor: "250000", expenseMinor: "40000", netProfitMinor: "210000",
    });
    vi.mocked(wails.balanceSheet).mockResolvedValue(BALANCED_SHEET);

    renderApp(<StatementsScreen />);

    // Gross margin and net profit are different numbers. A screen labelling either one "profit"
    // is how a shopkeeper concludes they are doing well while losing money.
    expect(await screen.findByText("Net profit")).toBeInTheDocument();
    expect(screen.queryByText("Profit")).not.toBeInTheDocument();
  });
});

describe("valuation", () => {
  it("distinguishes agreement from never having asked the books", async () => {
    vi.mocked(wails.stockValuation).mockResolvedValue({
      lines: [], totalMinor: "60000",
      hasLedger: false, ledgerMinor: "0", differenceMinor: "0", reconciled: false,
    });

    renderApp(<ValuationScreen />);

    // A difference of zero shown in both cases would claim a check that never happened.
    expect(await screen.findByText("The books were not checked")).toBeInTheDocument();
    expect(screen.queryByText("Difference")).not.toBeInTheDocument();
  });

  it("says so when the shelf and the books disagree", async () => {
    vi.mocked(wails.stockValuation).mockResolvedValue({
      lines: [], totalMinor: "60000",
      hasLedger: true, ledgerMinor: "54000", differenceMinor: "6000", reconciled: false,
    });

    renderApp(<ValuationScreen />);

    expect(await screen.findByText("The stock and the books disagree")).toBeInTheDocument();
  });
});

describe("search", () => {
  it("distinguishes nothing found from a source that could not answer", async () => {
    vi.mocked(wails.globalSearch).mockResolvedValue({
      query: "ahma", results: [], failed: ["sales"],
    });

    renderApp(<SearchBox />);

    const box = await screen.findByLabelText("Search");
    box.focus();
    // Typed rather than set, so the two-character minimum is actually crossed.
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(box, { target: { value: "ahma" } });

    expect(await screen.findByText("Some sources did not answer")).toBeInTheDocument();
    expect(screen.getByText("Nothing found.")).toBeInTheDocument();
  });

  it("does not call the backend below the minimum query length", async () => {
    vi.mocked(wails.globalSearch).mockResolvedValue({
      query: "a", results: [], failed: [],
    });

    renderApp(<SearchBox />);

    const box = await screen.findByLabelText("Search");
    const { fireEvent } = await import("@testing-library/react");
    fireEvent.change(box, { target: { value: "a" } });

    expect(wails.globalSearch).not.toHaveBeenCalled();
  });
});

// ── the KPI strip ───────────────────────────────────────────────────────────────

describe("KpiStrip", () => {
  const TILES = [
    { key: "sales.revenue", source: "sales", kind: "money", amountMinor: "450000",
      count: 0, periodic: true, failed: false },
    { key: "sales.receivables", source: "sales", kind: "money", amountMinor: "120000",
      count: 0, periodic: false, failed: false },
    { key: "inventory.out_of_stock", source: "inventory", kind: "count", amountMinor: "0",
      count: 3, periodic: false, failed: false },
  ];

  it("shows only the tiles belonging to its domain", async () => {
    vi.mocked(wails.dashboard).mockResolvedValue({
      from: "2026-08-01", to: "2026-08-22", tiles: TILES,
    });

    renderApp(<KpiStrip source="sales" />);

    expect(await screen.findByText("4,500.00")).toBeInTheDocument();
    expect(screen.getByText("1,200.00")).toBeInTheDocument();
    // Inventory's tile belongs on the stock screen, not here.
    expect(screen.queryByText("3")).not.toBeInTheDocument();
  });

  it("narrows further when asked for specific tiles", async () => {
    // The money screen wants what customers owe and nothing else: revenue there would be a
    // figure with no bearing on the decision the screen is open for.
    vi.mocked(wails.dashboard).mockResolvedValue({
      from: "2026-08-01", to: "2026-08-22", tiles: TILES,
    });

    renderApp(<KpiStrip source="sales" only={["sales.receivables"]} />);

    expect(await screen.findByText("1,200.00")).toBeInTheDocument();
    expect(screen.queryByText("4,500.00")).not.toBeInTheDocument();
  });

  it("says a figure could not be calculated rather than showing zero", async () => {
    /*
     * Zero is a REAL answer — a shop that sold nothing today has revenue of zero — so a failure
     * rendered as zero is a lie the reader has no way to detect. The same choice the dashboard
     * makes, for the same reason.
     */
    vi.mocked(wails.dashboard).mockResolvedValue({
      from: "2026-08-01", to: "2026-08-22",
      tiles: [{ key: "sales.revenue", source: "sales", kind: "money", amountMinor: "0",
                count: 0, periodic: true, failed: true }],
    });

    renderApp(<KpiStrip source="sales" />);

    expect(await screen.findByText(/could not be calculated/i)).toBeInTheDocument();
    expect(screen.queryByText("0.00")).not.toBeInTheDocument();
  });

  it("labels each figure with the range it covers", async () => {
    // A periodic figure and a position look identical otherwise, which is how "stock value
    // 40,000" gets read as this month's purchases.
    vi.mocked(wails.dashboard).mockResolvedValue({
      from: "2026-08-01", to: "2026-08-22", tiles: TILES,
    });

    renderApp(<KpiStrip source="sales" />);

    await screen.findByText("4,500.00");
    expect(screen.getAllByText(/this period|as at now/i).length).toBeGreaterThan(0);
  });

  it("renders nothing at all while the figures are still loading", () => {
    // It is a HEADER on somebody else's screen. A spinner or an error banner here pushes the
    // actual work down the page and reads as the screen below having failed.
    vi.mocked(wails.dashboard).mockReturnValue(new Promise(() => {}));

    renderApp(<KpiStrip source="sales" />);

    // Not `container` to be empty — the shared providers render their own live regions into it.
    // The claim is narrower and truer: this component contributed nothing.
    expect(screen.queryByRole("group")).not.toBeInTheDocument();
    expect(screen.queryByText(/could not be calculated/i)).not.toBeInTheDocument();
  });
});
