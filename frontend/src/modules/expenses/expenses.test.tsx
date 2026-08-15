import { screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "@/app/session/session";
import { ExpensesScreen } from "@/modules/expenses/ExpensesScreen";
import { DebtsScreen } from "@/modules/expenses/DebtsScreen";
import { PartnerStatement } from "@/modules/partners/PartnerStatement";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    preferences: vi.fn(),
    expenses: vi.fn(),
    unsettledExpenses: vi.fn(),
    debts: vi.fn(),
    debtPositions: vi.fn(),
    partnerStatement: vi.fn(),
  };
});

const PAID = {
  id: "e1", status: "posted", number: "EXP-000001", payeeName: "A taxi",
  expenseDate: "2026-09-01", reference: "", description: "",
  settlement: "immediate", paidMethod: "cash", dueDate: "",
  currency: "SAR", netMinor: "5000", taxMinor: "750", totalMinor: "5750",
  outstandingMinor: "", isTemplate: false,
};

const OWED = {
  ...PAID, id: "e2", number: "EXP-000002", payeeName: "The landlord",
  settlement: "on_account", paidMethod: "", dueDate: "2026-09-30",
  netMinor: "100000", taxMinor: "15000", totalMinor: "115000",
  outstandingMinor: "115000",
};

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en", theme: "system", availableLocales: ["en", "ar"],
  });
  vi.mocked(wails.expenses).mockResolvedValue([PAID, OWED]);
  vi.mocked(wails.unsettledExpenses).mockResolvedValue([OWED]);
  vi.mocked(wails.debts).mockResolvedValue([]);
  vi.mocked(wails.debtPositions).mockResolvedValue([]);
  useSessionStore.getState().setSession({
    ...SIGNED_IN,
    permissions: [
      wails.PERMISSIONS.expenseView, wails.PERMISSIONS.debtView,
      wails.PERMISSIONS.partnerBalanceView,
    ],
  });
});

afterEach(() => vi.clearAllMocks());

describe("the expenses screen", () => {
  it("shows outstanding as absent on an expense paid when it was recorded", async () => {
    // Absent, not zero. An expense paid on the spot owes nothing and never did — showing "0.00"
    // would read as a debt that WAS settled, which is a different fact.
    renderApp(<ExpensesScreen />);

    expect(await screen.findByText("EXP-000001")).toBeInTheDocument();
    // The owed one shows its figure; the paid one shows a dash.
    expect(screen.getAllByText("1,150.00").length).toBeGreaterThan(0);
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });

  it("distinguishes paid from owed, because the whole document turns on it", async () => {
    renderApp(<ExpensesScreen />);

    expect(await screen.findByText("Paid")).toBeInTheDocument();
    expect(screen.getAllByText("On account").length).toBeGreaterThan(0);
  });

  it("lists what is still to pay separately from what was spent", async () => {
    // "What did we spend" is a record; "what do we still owe" is the list somebody acts on.
    // Separated onto another screen, the second is never opened.
    renderApp(<ExpensesScreen />);

    // The heading, specifically — the table caption carries the same words, and a bare text
    // query would pass on either.
    expect(
      await screen.findByRole("heading", { name: "Still to pay" }),
    ).toBeInTheDocument();
    expect(wails.unsettledExpenses).toHaveBeenCalled();
  });
});

describe("the debts screen", () => {
  it("shows every position, including the ones with no movement", async () => {
    // A screen showing only the kinds with activity would silently change shape as a business
    // used it — and "the owner has taken nothing out" is an answer somebody wants stated.
    vi.mocked(wails.debtPositions).mockResolvedValue([
      { kind: "loan_payable", netMinor: "250000" },
      { kind: "loan_receivable", netMinor: "0" },
      { kind: "owner_equity", netMinor: "0" },
    ]);

    renderApp(<DebtsScreen />);

    expect(await screen.findByText("Borrowed")).toBeInTheDocument();
    expect(screen.getByText("Lent")).toBeInTheDocument();
    expect(screen.getByText("Owner's capital")).toBeInTheDocument();
  });

  it("reads a negative position as the other direction rather than hiding the sign", async () => {
    // A negative loan-payable means the business repaid more than it borrowed, which is unusual
    // enough to show plainly rather than as an absolute value with a word beside it.
    vi.mocked(wails.debtPositions).mockResolvedValue([
      { kind: "loan_payable", netMinor: "-5000" },
      { kind: "loan_receivable", netMinor: "0" },
      { kind: "owner_equity", netMinor: "0" },
    ]);

    renderApp(<DebtsScreen />);

    expect(await screen.findByText("-50.00")).toBeInTheDocument();
    expect(screen.getByText("repaid beyond what was borrowed")).toBeInTheDocument();
  });
});

describe("the partner statement", () => {
  const STATEMENT = {
    balance: {
      partnerId: "p1", partnerName: "Corner Shop",
      receivableMinor: "500000", payableMinor: "490000", netMinor: "10000",
    },
    receivable: [
      {
        documentType: "sales.invoice", documentId: "d1", documentNumber: "INV-0001",
        date: "2026-06-01", dueDate: "2026-07-01",
        totalMinor: "500000", settledMinor: "0", outstandingMinor: "500000",
      },
    ],
    payable: [
      {
        documentType: "purchasing.bill", documentId: "b1", documentNumber: "BILL-0001",
        date: "2026-08-01", dueDate: "2026-09-01",
        totalMinor: "490000", settledMinor: "0", outstandingMinor: "490000",
      },
    ],
    receivableAgeing: {
      currentMinor: "0", days30Minor: "0", days60Minor: "0",
      days90Minor: "500000", olderMinor: "0",
    },
  };

  it("keeps both halves visible rather than netting them away", async () => {
    // Owing 5,000 and being owed 4,900 is NOT the same risk as owing 100, and a screen that
    // showed only the net would say it was.
    vi.mocked(wails.partnerStatement).mockResolvedValue(STATEMENT);

    renderApp(<PartnerStatement partnerId="p1" onBack={() => {}} />);

    expect(await screen.findByText("Corner Shop")).toBeInTheDocument();

    // Each figure appears twice — once in the summary and once on the row it came from — so the
    // assertion is that BOTH halves are present, which is the thing netting would destroy.
    expect(screen.getAllByText("5,000.00").length).toBeGreaterThan(0); // they owe
    expect(screen.getAllByText("4,900.00").length).toBeGreaterThan(0); // we owe
    expect(screen.getByText("100.00")).toBeInTheDocument(); // the net, shown once
  });

  it("says what kind of document each row is", async () => {
    // A statement row a customer checks against their own records needs to say what it WAS, not
    // only what it came to.
    vi.mocked(wails.partnerStatement).mockResolvedValue(STATEMENT);

    renderApp(<PartnerStatement partnerId="p1" onBack={() => {}} />);

    expect(await screen.findByText("Sales invoice")).toBeInTheDocument();
    expect(screen.getByText("Supplier bill")).toBeInTheDocument();
  });

  it("asks for the ageing date the operator chose, not the clock's", async () => {
    // A statement printed for a month end must say what it said at that month end.
    vi.mocked(wails.partnerStatement).mockResolvedValue(STATEMENT);

    renderApp(<PartnerStatement partnerId="p1" onBack={() => {}} />);
    await screen.findByText("Corner Shop");

    // Empty means "today" — the BACKEND decides, so the frontend never bakes a date in.
    expect(wails.partnerStatement).toHaveBeenCalledWith("p1", "");
  });

  it("shows the ageing band a debt has actually reached", async () => {
    vi.mocked(wails.partnerStatement).mockResolvedValue(STATEMENT);

    renderApp(<PartnerStatement partnerId="p1" onBack={() => {}} />);

    expect(await screen.findByText(/Up to 90 days/)).toBeInTheDocument();
    // Bands with nothing in them are not shown, so the ones that are carry meaning.
    expect(screen.queryByText(/Not yet due/)).not.toBeInTheDocument();
  });
});
