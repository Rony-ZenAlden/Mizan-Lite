import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "@/app/session/session";
import { StockScreen } from "@/modules/inventory/StockScreen";
import { formatQuantity, isNegativeQuantity, toMicro } from "@/modules/inventory/quantity";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    preferences: vi.fn(),
    stockOnHand: vi.fn(),
    stockMovements: vi.fn(),
    checkLedger: vi.fn(),
  };
});

const CEMENT = {
  variantId: "v1", productCode: "CEMENT", productName: "Bag of cement", sku: "CEMENT",
  unit: "PCS", onHandMicro: "10000000", reservedMicro: "0", availableMicro: "10000000",
  averageCostMicro: "100000000", valueMinor: "1000",
};

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en", theme: "system", availableLocales: ["en", "ar"],
  });
  vi.mocked(wails.stockOnHand).mockResolvedValue([CEMENT]);
  vi.mocked(wails.checkLedger).mockResolvedValue({
    checked: 1, discrepancies: [], healthy: true,
  });
  useSessionStore.getState().setSession({
    ...SIGNED_IN,
    permissions: [wails.PERMISSIONS.stockView, wails.PERMISSIONS.costView],
  });
});

afterEach(() => vi.clearAllMocks());

// ── the formatter ───────────────────────────────────────────────────────────────

describe("formatQuantity", () => {
  it("formats micro units without ever parsing them into a number", () => {
    expect(formatQuantity("10000000")).toBe("10");
    expect(formatQuantity("2500000")).toBe("2.5");
    expect(formatQuantity("1437000")).toBe("1.437");
    expect(formatQuantity("0")).toBe("0");
    expect(formatQuantity("500000")).toBe("0.5");
  });

  it("groups thousands", () => {
    expect(formatQuantity("1234567000000")).toBe("1,234,567");
  });

  it("keeps a negative quantity negative, because stock genuinely can be", () => {
    expect(formatQuantity("-3000000")).toBe("-3");
    expect(isNegativeQuantity("-3000000")).toBe(true);
    expect(isNegativeQuantity("0")).toBe(false);
  });

  // The reason these cross as strings at all: 2^53 is 9007199254740992, and a quantity in micro
  // units passes it at nine billion. A float64 cannot tell the next two integers apart.
  it("survives a quantity beyond float64's integer precision", () => {
    const huge = "9007199254740993000000";
    expect(formatQuantity(huge)).toBe("9,007,199,254,740,993");
    // The proof that parsing would have lost it.
    expect(String(Number(huge) / 1e6)).not.toBe("9007199254740993");
  });

  it("trims to the precision asked for", () => {
    expect(formatQuantity("100666667", 2)).toBe("100.66");
  });
});

describe("toMicro", () => {
  it("turns what somebody typed into micro units without going through a float", () => {
    expect(toMicro("2.5")).toBe("2500000");
    expect(toMicro("10")).toBe("10000000");
    expect(toMicro("0.001")).toBe("1000");
    expect(toMicro("1.437238")).toBe("1437238");
  });

  it("refuses anything that is not a plain decimal, rather than sending NaN", () => {
    for (const bad of ["", "abc", "1.2.3", "1e5", "-", "1,5"]) {
      expect(toMicro(bad)).toBeNull();
    }
  });

  // Six decimals is the scale; a seventh is a number this system cannot hold, and silently
  // rounding it would be inventing precision the user did not ask for.
  it("refuses more precision than the scale holds", () => {
    expect(toMicro("1.1234567")).toBeNull();
  });
});

// ── the screen ──────────────────────────────────────────────────────────────────

describe("StockScreen", () => {
  it("lists what is on hand with its unit", async () => {
    renderApp(<StockScreen />);

    const table = await screen.findByRole("table", { name: /stock on hand/i });
    expect(within(table).getByText("CEMENT")).toBeInTheDocument();
    expect(within(table).getByText("10 PCS")).toBeInTheDocument();
  });

  // Costs are absent, not zero, for a caller without the permission — so the COLUMNS go, rather
  // than filling with figures that would read as free stock.
  it("drops the cost columns entirely when the caller may not see them", async () => {
    // Absent, exactly as redact.Visible leaves them for a caller without the permission —
    // built by omission rather than by deleting, so the type still says these are optional.
    vi.mocked(wails.stockOnHand).mockResolvedValue([{
      variantId: CEMENT.variantId, productCode: CEMENT.productCode,
      productName: CEMENT.productName, sku: CEMENT.sku, unit: CEMENT.unit,
      onHandMicro: CEMENT.onHandMicro, reservedMicro: CEMENT.reservedMicro,
      availableMicro: CEMENT.availableMicro,
    }]);
    useSessionStore.getState().setSession({
      ...SIGNED_IN, permissions: [wails.PERMISSIONS.stockView],
    });

    renderApp(<StockScreen />);

    const table = await screen.findByRole("table", { name: /stock on hand/i });
    expect(within(table).getByText("CEMENT")).toBeInTheDocument();
    expect(within(table).queryByText(/average cost/i)).not.toBeInTheDocument();
    // And no zero anywhere pretending to be a cost.
    expect(within(table).queryByText("0.00")).not.toBeInTheDocument();
  });

  it("shows the cost columns when the caller may see them", async () => {
    renderApp(<StockScreen />);

    const table = await screen.findByRole("table", { name: /stock on hand/i });
    expect(within(table).getByText(/average cost/i)).toBeInTheDocument();
    expect(within(table).getByText("10.00")).toBeInTheDocument();
  });

  // Negative stock is a real state some warehouses permit, and it must LOOK like the exception it
  // is rather than blending into the column.
  it("marks stock that has gone below zero", async () => {
    vi.mocked(wails.stockOnHand).mockResolvedValue([
      { ...CEMENT, onHandMicro: "-3000000", availableMicro: "-3000000" },
    ]);

    renderApp(<StockScreen />);

    const table = await screen.findByRole("table", { name: /stock on hand/i });
    expect(within(table).getByText("Below zero")).toBeInTheDocument();
    expect(within(table).getByText("-3 PCS")).toBeInTheDocument();
  });

  // A projection that has drifted from its ledger is invisible until somebody counts — which may
  // be a year. This is what makes it visible before then.
  it("warns when the stock levels disagree with the movement ledger", async () => {
    vi.mocked(wails.checkLedger).mockResolvedValue({
      checked: 4,
      healthy: false,
      discrepancies: [
        {
          variantId: "v1", sku: "CEMENT",
          projectedOnHandMicro: "99000000", ledgerOnHandMicro: "7000000",
          firstSuspectMovementId: "m3",
        },
      ],
    });

    renderApp(<StockScreen />);

    expect(await screen.findByText(/do not match the movement ledger/i)).toBeInTheDocument();
    // And it says nothing was repaired, because nothing was.
    expect(screen.getByText(/hides the fault that broke it/i)).toBeInTheDocument();
  });

  it("says nothing when the ledger is healthy", async () => {
    renderApp(<StockScreen />);

    await screen.findByRole("table", { name: /stock on hand/i });
    expect(screen.queryByText(/do not match the movement ledger/i)).not.toBeInTheDocument();
  });

  it("filters by product, code, or SKU", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.stockOnHand).mockResolvedValue([
      CEMENT,
      { ...CEMENT, variantId: "v2", productCode: "SAND", productName: "Sand", sku: "SAND" },
    ]);

    renderApp(<StockScreen />);
    await screen.findByRole("table", { name: /stock on hand/i });
    await user.type(screen.getByLabelText(/search stock/i), "sand");

    const table = screen.getByRole("table", { name: /stock on hand/i });
    expect(within(table).queryByText("CEMENT")).not.toBeInTheDocument();
    expect(within(table).getByText("SAND")).toBeInTheDocument();
  });
});

describe("MovementHistory", () => {
  it("shows the ledger that produced the figure, with each movement's direction", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.stockMovements).mockResolvedValue([
      {
        id: "m1", movementType: "receipt", isInward: true, quantityMicro: "10000000",
        unitCostMicro: "100000000", valueMinor: "1000", balanceAfterMicro: "10000000",
        documentType: "", reason: "", occurredAt: "2026-06-15T09:00:00Z",
      },
      {
        id: "m2", movementType: "adjustment_out", isInward: false, quantityMicro: "2000000",
        unitCostMicro: "100000000", valueMinor: "-200", balanceAfterMicro: "8000000",
        documentType: "", reason: "damaged", occurredAt: "2026-06-16T09:00:00Z",
      },
    ]);

    renderApp(<StockScreen />);
    const stock = await screen.findByRole("table", { name: /stock on hand/i });
    await user.click(within(stock).getByRole("button", { name: "CEMENT" }));

    const table = await screen.findByRole("table", { name: /movements/i });
    expect(within(table).getByText("Received")).toBeInTheDocument();
    expect(within(table).getByText("Adjusted down")).toBeInTheDocument();

    // The direction badge comes from the backend's reading of the movement TYPE; the quantity
    // itself is always positive.
    expect(within(table).getByText("In")).toBeInTheDocument();
    expect(within(table).getByText("Out")).toBeInTheDocument();
    expect(within(table).getByText("2 PCS")).toBeInTheDocument();

    // The balance each movement left behind — recorded at write time, not recomputed here.
    expect(within(table).getByText("8")).toBeInTheDocument();
    expect(within(table).getByText("damaged")).toBeInTheDocument();
  });

  it("shows the date the stock moved, not the date the row was written", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.stockMovements).mockResolvedValue([
      {
        id: "m1", movementType: "receipt", isInward: true, quantityMicro: "10000000",
        balanceAfterMicro: "10000000", documentType: "", reason: "",
        occurredAt: "2026-06-15T09:00:00Z",
      },
    ]);

    renderApp(<StockScreen />);
    const stock = await screen.findByRole("table", { name: /stock on hand/i });
    await user.click(within(stock).getByRole("button", { name: "CEMENT" }));

    const table = await screen.findByRole("table", { name: /movements/i });
    expect(within(table).getByText("2026-06-15")).toBeInTheDocument();
  });
});
