import { screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "@/app/session/session";
import { ChartScreen } from "@/modules/accounting/ChartScreen";
import { TrialBalanceScreen } from "@/modules/accounting/TrialBalanceScreen";
import { formatMinor, isZeroMinor } from "@/modules/accounting/money";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    preferences: vi.fn(),
    chartOfAccounts: vi.fn(),
    fiscalPeriods: vi.fn(),
    trialBalance: vi.fn(),
  };
});

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en", theme: "system", availableLocales: ["en", "ar"], landing: "/",
  });
  useSessionStore.getState().setSession({
    ...SIGNED_IN, permissions: [wails.PERMISSIONS.accountView],
  });
});

afterEach(() => vi.clearAllMocks());

// ── the formatter ───────────────────────────────────────────────────────────────

describe("formatMinor", () => {
  it("formats minor units without ever parsing them into a number", () => {
    expect(formatMinor("0")).toBe("0.00");
    expect(formatMinor("1")).toBe("0.01");
    expect(formatMinor("150")).toBe("1.50");
    expect(formatMinor("123456")).toBe("1,234.56");
    expect(formatMinor("-250000")).toBe("-2,500.00");
  });

  it("handles a currency with no decimal places", () => {
    // Syrian Pounds have none, and this is the case that reaches JavaScript's precision
    // ceiling first — which is why the amount is a string in the first place.
    expect(formatMinor("1234567", 0)).toBe("1,234,567");
  });

  it("survives an amount beyond float64's integer precision", () => {
    // 2^53 is 9007199254740992. A float64 cannot tell the next two integers apart; a string
    // can, and a hyperinflated currency reaches this in ordinary trading (§18).
    const huge = "9007199254740993";
    expect(formatMinor(huge, 0)).toBe("9,007,199,254,740,993");
    expect(formatMinor(huge, 0)).not.toBe(formatMinor("9007199254740992", 0));
  });

  it("recognises zero without parsing", () => {
    expect(isZeroMinor("0")).toBe(true);
    expect(isZeroMinor("-0")).toBe(true);
    expect(isZeroMinor("000")).toBe(true);
    expect(isZeroMinor("1")).toBe(false);
  });
});

// ── the chart ───────────────────────────────────────────────────────────────────

describe("ChartScreen", () => {
  it("renders the hierarchy and distinguishes headings from accounts", async () => {
    vi.mocked(wails.chartOfAccounts).mockResolvedValue([
      {
        id: "a1", code: "1000", name: "Assets", nameKey: "account.assets",
        type: "asset", normal: "debit", depth: 0,
        isPostable: false, isSystem: false, isActive: true,
      },
      {
        id: "a2", code: "1110", name: "Cash on hand", nameKey: "account.cash",
        type: "asset", normal: "debit", depth: 1,
        isPostable: true, isSystem: true, isActive: true,
      },
    ]);

    renderApp(<ChartScreen />);

    expect(await screen.findByText("1000")).toBeInTheDocument();

    // A heading is the shape of a chart, not a defect, so it reads as a description. Scoped to
    // the table BODY: "Account" is also a column header, and asserting on the whole document
    // would match either and prove neither.
    const body = screen.getAllByRole("rowgroup")[1] ?? screen.getByRole("table");
    expect(within(body).getByText("Heading")).toBeInTheDocument();
    expect(within(body).getByText("Account")).toBeInTheDocument();
    // Names come from the translation key, not the stored English.
    expect(screen.getByText("Assets")).toBeInTheDocument();
    expect(screen.getByText("Cash on hand")).toBeInTheDocument();
  });
});

// ── the trial balance ───────────────────────────────────────────────────────────

describe("TrialBalanceScreen", () => {
  const period = { id: "p1", sequence: 3, start: "2026-03-01", end: "2026-03-31", status: "open" };

  it("shows the amounts and the totals computed in Go", async () => {
    vi.mocked(wails.fiscalPeriods).mockResolvedValue([period]);
    vi.mocked(wails.trialBalance).mockResolvedValue({
      periodId: "p1",
      rows: [
        {
          accountId: "a1", code: "1200", name: "Receivables", type: "asset", normal: "debit",
          openingMinor: "0", debitMinor: "25000", creditMinor: "0", closingMinor: "25000",
        },
        {
          accountId: "a2", code: "4100", name: "Sales", type: "revenue", normal: "credit",
          openingMinor: "0", debitMinor: "0", creditMinor: "25000", closingMinor: "-25000",
        },
      ],
      totalDebitMinor: "25000", totalCreditMinor: "25000", balanced: true,
    });

    renderApp(<TrialBalanceScreen />);

    expect(await screen.findByText("1200")).toBeInTheDocument();
    expect(screen.getAllByText("250.00").length).toBeGreaterThan(0);
    // A credit-normal account reads negative under the debit-positive convention (2.3).
    expect(screen.getByText("-250.00")).toBeInTheDocument();
    // Balanced: no alarm.
    expect(screen.queryByText(/do not balance/i)).not.toBeInTheDocument();
  });

  it("shouts when the books do not balance", async () => {
    // The one thing on this screen that must never be quiet: an unbalanced trial balance means
    // the books are wrong, and a plain number would let somebody read past it.
    vi.mocked(wails.fiscalPeriods).mockResolvedValue([period]);
    vi.mocked(wails.trialBalance).mockResolvedValue({
      periodId: "p1",
      rows: [
        {
          accountId: "a1", code: "1200", name: "Receivables", type: "asset", normal: "debit",
          openingMinor: "0", debitMinor: "25000", creditMinor: "0", closingMinor: "25000",
        },
      ],
      totalDebitMinor: "25000", totalCreditMinor: "24999", balanced: false,
    });

    renderApp(<TrialBalanceScreen />);

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/do not balance/i);
  });
});
