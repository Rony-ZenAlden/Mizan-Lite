import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "@/app/session/session";
import { ReceiptsScreen } from "@/modules/purchasing/ReceiptsScreen";
import { SupplierReturnsScreen } from "@/modules/purchasing/SupplierReturnsScreen";
import { SupplierPaymentsScreen } from "@/modules/purchasing/SupplierPaymentsScreen";
import { LandedCostsPanel } from "@/modules/purchasing/LandedCostsPanel";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    preferences: vi.fn(),
    goodsReceipts: vi.fn(),
    supplierReturns: vi.fn(),
    supplierPayments: vi.fn(),
    landedCosts: vi.fn(),
    applyLandedCost: vi.fn(),
  };
});

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en", theme: "system", availableLocales: ["en", "ar"], landing: "/",
  });
  vi.mocked(wails.goodsReceipts).mockResolvedValue([]);
  vi.mocked(wails.supplierReturns).mockResolvedValue([]);
  vi.mocked(wails.supplierPayments).mockResolvedValue([]);
  vi.mocked(wails.landedCosts).mockResolvedValue([]);
  useSessionStore.getState().setSession({
    ...SIGNED_IN,
    permissions: [
      wails.PERMISSIONS.orderView, wails.PERMISSIONS.billView,
      wails.PERMISSIONS.supplierPay,
    ],
  });
});

describe("deliveries", () => {
  it("totals what is awaiting an invoice, rather than leaving it to be added up", async () => {
    vi.mocked(wails.goodsReceipts).mockResolvedValue([
      {
        id: "r1", status: "confirmed", number: "GRN-000001", orderId: "o1",
        supplierName: "Acme", receiptDate: "2026-08-01", deliveryNoteReference: "DN-9",
        receivedByName: "Sam", valueMinor: "40000", billed: false,
      },
      {
        id: "r2", status: "confirmed", number: "GRN-000002", orderId: "o1",
        supplierName: "Acme", receiptDate: "2026-08-02", deliveryNoteReference: "",
        receivedByName: "Sam", valueMinor: "20000", billed: false,
      },
      {
        id: "r3", status: "confirmed", number: "GRN-000003", orderId: "o2",
        supplierName: "Beta", receiptDate: "2026-08-03", deliveryNoteReference: "",
        receivedByName: "Sam", valueMinor: "90000", billed: true,
      },
    ]);

    renderApp(<ReceiptsScreen />);

    // 400.00 + 200.00, excluding the invoiced one. This is the GRNI figure an accountant
    // reconciles at month end, and making them add a column to find it is how a screen fails
    // somebody who is already under time pressure.
    expect(await screen.findByText(/2 deliveries worth 600\.00/)).toBeInTheDocument();
  });
});

describe("returns", () => {
  it("shows the credit and the cost side by side, because they differ", async () => {
    vi.mocked(wails.supplierReturns).mockResolvedValue([
      {
        id: "ret1", number: "DN-000001", status: "posted",
        supplierId: "s1", supplierName: "Acme", returnDate: "2026-08-10",
        reason: "damaged in transit", currency: "SAR",
        netMinor: "20000", taxMinor: "3000", totalMinor: "23000",
        // What the ORIGINAL delivery cost us, which is not what the supplier credits.
        costMinor: "18000",
        lines: [],
      },
    ]);

    renderApp(<SupplierReturnsScreen />);

    expect(await screen.findByText("230.00")).toBeInTheDocument();
    // §D.3: a return is costed at the ORIGINAL delivery, not today's average. A screen showing
    // only the credit leaves somebody unable to explain why inventory moved by a different
    // figure.
    expect(screen.getByText("180.00")).toBeInTheDocument();
  });
});

describe("supplier payments", () => {
  it("names the method, so a till reconciliation can see money left the bank", async () => {
    vi.mocked(wails.supplierPayments).mockResolvedValue([
      {
        id: "p1", number: "PAY-000001", supplierName: "Acme",
        paymentDate: "2026-08-12", method: "bank_transfer", reference: "TRF-88",
        amountMinor: "115000", status: "posted",
      },
    ]);

    renderApp(<SupplierPaymentsScreen />);

    // 6.7's defect: one rule credited cash whatever the method, so a transfer would have reduced
    // the till and the till would have been short with nothing to explain it.
    expect(await screen.findByText("Bank transfer")).toBeInTheDocument();
  });
});

describe("landed costs", () => {
  it("keeps recording a charge separate from putting it in the cost", async () => {
    vi.mocked(wails.landedCosts).mockResolvedValue([
      {
        id: "c1", receiptId: "r1", chargeType: "freight", description: "Sea freight",
        basis: "value", currency: "SAR", amountMinor: "50000", status: "pending",
      },
      {
        id: "c2", receiptId: "r1", chargeType: "customs", description: "Customs duty",
        basis: "quantity", currency: "SAR", amountMinor: "20000", status: "applied",
      },
    ]);

    renderApp(<LandedCostsPanel receiptId="r1" />);

    // Pending offers the button; applied does not. Recording that freight was charged is
    // bookkeeping; deciding it belongs in the cost of these goods is a judgement somebody makes.
    expect(await screen.findByText("Add to cost")).toBeInTheDocument();
    expect(screen.getByText("In the cost")).toBeInTheDocument();

    // The BASIS is shown: two charges of the same amount on one delivery can cost two products
    // very differently depending on how they are spread.
    expect(screen.getByText("Value")).toBeInTheDocument();
    expect(screen.getByText("Quantity")).toBeInTheDocument();
  });
});
