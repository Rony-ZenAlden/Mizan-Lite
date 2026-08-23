import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "@/app/session/session";
import { POSTerminal } from "@/modules/sales/POSTerminal";
import { InvoicesScreen } from "@/modules/sales/InvoicesScreen";
import { InvoiceDetail } from "@/modules/sales/InvoiceDetail";
import { printHTML } from "@/modules/sales/print";
import { compareMinor, parseMinor, subtractMinor } from "@/modules/accounting/money";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    preferences: vi.fn(),
    currentShift: vi.fn(),
    openShift: vi.fn(),
    closeShift: vi.fn(),
    heldSales: vi.fn(),
    scanBarcode: vi.fn(),
    draftSale: vi.fn(),
    salesDocument: vi.fn(),
    addSaleLine: vi.fn(),
    removeSaleLine: vi.fn(),
    postSale: vi.fn(),
    takePayment: vi.fn(),
    salesDocuments: vi.fn(),
    printSalesDocument: vi.fn(),
  };
});

const OPEN_SHIFT = {
  id: "shift-1", terminal: "main", status: "open",
  openedAt: "2026-08-13T08:00:00Z", openingFloatMinor: "10000", closedAt: "",
};

const LINE = {
  id: "l1", lineNumber: 1, variantId: "v1", productName: "Bag of cement", sku: "CEMENT",
  uomCode: "PCS", quantityMicro: "2000000", unitPriceMinor: "1500", discountMinor: "0",
  taxAmountMinor: "450", netMinor: "3000", totalMinor: "3450", priceListCode: "RETAIL",
};

const SALE = {
  document: {
    id: "d1", documentType: "invoice", status: "draft", number: "", partnerName: "",
    date: "2026-08-13", currency: "SAR", netMinor: "3000", taxMinor: "450",
    discountMinor: "0", totalMinor: "3450", outstandingMinor: "", isHeld: false, holdLabel: "",
  },
  lines: [LINE],
  editable: true,
};

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en", theme: "system", availableLocales: ["en", "ar"], landing: "/",
  });
  vi.mocked(wails.currentShift).mockResolvedValue(OPEN_SHIFT);
  vi.mocked(wails.heldSales).mockResolvedValue([]);
  vi.mocked(wails.salesDocuments).mockResolvedValue([]);
  useSessionStore.getState().setSession({
    ...SIGNED_IN,
    permissions: [
      wails.PERMISSIONS.saleView, wails.PERMISSIONS.saleDraft,
      wails.PERMISSIONS.salePost, wails.PERMISSIONS.shiftOpen,
      wails.PERMISSIONS.shiftClose,
    ],
  });
});

afterEach(() => {
  vi.clearAllMocks();
  vi.restoreAllMocks();
  // printHTML appends to document.body, which testing-library's cleanup does not own. Left
  // alone they accumulate across tests and make DOM queries answer for the wrong test.
  document.querySelectorAll("iframe").forEach((frame) => frame.remove());
});

// ── the money arithmetic ────────────────────────────────────────────────────────
//
// Change due is the one figure this application computes in JavaScript, so it gets the same
// scrutiny the Go money code does.

describe("exact money arithmetic", () => {
  it("subtracts beyond the float64 integer ceiling", () => {
    // 2^53 is 9007199254740992. A currency with no minor unit reaches this in ordinary trading
    // under hyperinflation, which is a situation this product is explicitly built for (§18).
    // Number() would round both operands before subtracting and answer 0.
    expect(subtractMinor("9007199254740993", "9007199254740992")).toBe("1");
    expect(Number("9007199254740993") - Number("9007199254740992")).toBe(0);
  });

  it("gives change and reports a short tender", () => {
    expect(subtractMinor("5000", "3450")).toBe("1550");
    expect(compareMinor("3000", "3450")).toBe(-1);
    expect(compareMinor("3450", "3450")).toBe(0);
  });

  it("parses a typed amount without ever multiplying a float", () => {
    // 8.07 is the classic case: parseFloat("8.07") * 100 is 806.9999999999999, and a till that
    // rounds that is a penny out roughly once a day with no way to explain it.
    // 0.29 is the classic case: parseFloat("0.29") * 100 is 28.999999999999996. Rounding hides
    // it; truncating — which is what a `| 0` or a `Math.trunc` does — charges 28 pence for a
    // 29-pence item, once a day, with no way to explain it.
    expect(parseMinor("0.29")).toBe("29");
    expect(parseFloat("0.29") * 100).not.toBe(29);
    expect(Math.trunc(parseFloat("0.29") * 100)).toBe(28);

    expect(parseMinor("8.07")).toBe("807");
    expect(parseMinor("100")).toBe("10000");
    expect(parseMinor("0.5")).toBe("50");
    expect(parseMinor("0")).toBe("0");
  });

  it("refuses more decimals than the currency has, rather than truncating", () => {
    // Truncating would take a different amount than the one on screen.
    expect(parseMinor("8.075")).toBeNull();
    expect(parseMinor("abc")).toBeNull();
    expect(parseMinor("")).toBeNull();
  });
});

// ── the till ────────────────────────────────────────────────────────────────────

describe("the point of sale", () => {
  it("refuses to trade with no open shift, and replaces the screen rather than warning", async () => {
    // Cash taken outside a shift belongs to no reconciliation: at close the drawer is short and
    // there is no record of why. So this is a gate, not a banner.
    vi.mocked(wails.currentShift).mockResolvedValue({ ...OPEN_SHIFT, status: "closed" });

    renderApp(<POSTerminal />);

    expect(await screen.findByText("Open the till")).toBeInTheDocument();
    expect(screen.queryByLabelText("Scan or type a barcode")).not.toBeInTheDocument();
  });

  it("opens the sale on the FIRST scan, not when the screen loads", async () => {
    // A draft per visit would litter the day with empty documents nobody cancels.
    vi.mocked(wails.draftSale).mockResolvedValue({ ...SALE.document, id: "d1" });
    vi.mocked(wails.salesDocument).mockResolvedValue({ ...SALE, lines: [] });
    vi.mocked(wails.scanBarcode).mockResolvedValue({
      variantId: "v1", productCode: "CEMENT", productName: "Bag of cement", sku: "CEMENT",
      quantityMicro: "1000000", packagingCode: "",
    });
    vi.mocked(wails.addSaleLine).mockResolvedValue(SALE);

    const user = userEvent.setup();
    renderApp(<POSTerminal />);

    const field = await screen.findByLabelText("Scan or type a barcode");
    expect(wails.draftSale).not.toHaveBeenCalled();

    await user.type(field, "5901234123457{Enter}");

    await waitFor(() => expect(wails.draftSale).toHaveBeenCalledTimes(1));
    expect(await screen.findByText("Bag of cement")).toBeInTheDocument();
  });

  it("adds a case barcode's whole case, not one unit", async () => {
    // §A.5: one scan of a case barcode is twelve units. Charging for one is the defect that
    // makes a shop stop trusting the scanner.
    vi.mocked(wails.draftSale).mockResolvedValue(SALE.document);
    vi.mocked(wails.salesDocument).mockResolvedValue({ ...SALE, lines: [] });
    vi.mocked(wails.scanBarcode).mockResolvedValue({
      variantId: "v1", productCode: "CEMENT", productName: "Bag of cement", sku: "CEMENT",
      quantityMicro: "12000000", packagingCode: "CASE",
    });
    vi.mocked(wails.addSaleLine).mockResolvedValue(SALE);

    const user = userEvent.setup();
    renderApp(<POSTerminal />);
    await user.type(
      await screen.findByLabelText("Scan or type a barcode"), "10012345678902{Enter}");

    await waitFor(() =>
      expect(wails.addSaleLine).toHaveBeenCalledWith(
        expect.objectContaining({ quantityMicro: "12000000" }),
      ),
    );
  });

  it("takes focus back from a control that stole it, not merely leaves it where it was", async () => {
    // The first version of this test typed into the scan field and asserted the field still had
    // focus — which it did, because nothing had taken it. The drill that removed the focus
    // effect PASSED.
    //
    // A scanner types into whatever holds focus. So the test has to take focus away first, the
    // way a real operator does: void a line by clicking the button. If focus stays on that
    // button, the next scan is typed into it and the item is never charged for.
    vi.mocked(wails.draftSale).mockResolvedValue(SALE.document);
    vi.mocked(wails.salesDocument).mockResolvedValue({ ...SALE, lines: [] });
    vi.mocked(wails.scanBarcode).mockResolvedValue({
      variantId: "v1", productCode: "C", productName: "Bag of cement", sku: "CEMENT",
      quantityMicro: "1000000", packagingCode: "",
    });
    vi.mocked(wails.addSaleLine).mockResolvedValue(SALE);
    vi.mocked(wails.removeSaleLine).mockResolvedValue({ ...SALE, lines: [] });

    const user = userEvent.setup();
    renderApp(<POSTerminal />);
    const field = await screen.findByLabelText("Scan or type a barcode");

    await user.type(field, "590{Enter}");
    await screen.findByText("Bag of cement");
    expect(field).toHaveValue("");

    const voidButton = screen.getByRole("button", { name: "Void" });
    await user.click(voidButton);

    await waitFor(() => expect(field).toHaveFocus());
    expect(voidButton).not.toHaveFocus();
  });

  it("records the SALE's amount, not what the customer handed over", async () => {
    // The extra note in the drawer is change, not revenue. Recording the tendered amount would
    // overstate the day by exactly the change given.
    vi.mocked(wails.salesDocument).mockResolvedValue(SALE);
    vi.mocked(wails.draftSale).mockResolvedValue(SALE.document);
    vi.mocked(wails.scanBarcode).mockResolvedValue({
      variantId: "v1", productCode: "C", productName: "Bag of cement", sku: "CEMENT",
      quantityMicro: "2000000", packagingCode: "",
    });
    vi.mocked(wails.addSaleLine).mockResolvedValue(SALE);
    vi.mocked(wails.postSale).mockResolvedValue({ ...SALE.document, status: "posted" });
    vi.mocked(wails.takePayment).mockResolvedValue({ ...SALE.document, status: "posted" });

    const user = userEvent.setup();
    renderApp(<POSTerminal />);
    await user.type(await screen.findByLabelText("Scan or type a barcode"), "590{Enter}");
    await screen.findByText("Bag of cement");

    await user.click(screen.getByRole("button", { name: "Take payment" }));
    await user.type(await screen.findByLabelText("Amount received"), "50");

    // 50.00 tendered against a 34.50 total leaves 15.50.
    expect(await screen.findByText("15.50")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Complete sale" }));

    await waitFor(() =>
      expect(wails.takePayment).toHaveBeenCalledWith(
        expect.objectContaining({ amountMinor: "3450" }),
      ),
    );
  });

  it("posts the sale BEFORE recording the payment", async () => {
    // A payment allocates against an obligation. Recording money against a document that is not
    // yet posted gives an unallocated receipt and a customer charged but not invoiced.
    const order: string[] = [];
    vi.mocked(wails.salesDocument).mockResolvedValue(SALE);
    vi.mocked(wails.draftSale).mockResolvedValue(SALE.document);
    vi.mocked(wails.scanBarcode).mockResolvedValue({
      variantId: "v1", productCode: "C", productName: "Bag of cement", sku: "CEMENT",
      quantityMicro: "2000000", packagingCode: "",
    });
    vi.mocked(wails.addSaleLine).mockResolvedValue(SALE);
    vi.mocked(wails.postSale).mockImplementation(async () => {
      order.push("post");
      return { ...SALE.document, status: "posted" };
    });
    vi.mocked(wails.takePayment).mockImplementation(async () => {
      order.push("pay");
      return { ...SALE.document, status: "posted" };
    });

    const user = userEvent.setup();
    renderApp(<POSTerminal />);
    await user.type(await screen.findByLabelText("Scan or type a barcode"), "590{Enter}");
    await screen.findByText("Bag of cement");
    await user.click(screen.getByRole("button", { name: "Take payment" }));
    await user.click(await screen.findByRole("button", { name: "Complete sale" }));

    await waitFor(() => expect(order).toEqual(["post", "pay"]));
  });

  it("refuses to complete a sale on a short tender", async () => {
    // A till that accepts less than the total and calls it settled has invented a debt nobody
    // agreed to.
    vi.mocked(wails.salesDocument).mockResolvedValue(SALE);
    vi.mocked(wails.draftSale).mockResolvedValue(SALE.document);
    vi.mocked(wails.scanBarcode).mockResolvedValue({
      variantId: "v1", productCode: "C", productName: "Bag of cement", sku: "CEMENT",
      quantityMicro: "2000000", packagingCode: "",
    });
    vi.mocked(wails.addSaleLine).mockResolvedValue(SALE);

    const user = userEvent.setup();
    renderApp(<POSTerminal />);
    await user.type(await screen.findByLabelText("Scan or type a barcode"), "590{Enter}");
    await screen.findByText("Bag of cement");
    await user.click(screen.getByRole("button", { name: "Take payment" }));
    await user.type(await screen.findByLabelText("Amount received"), "20");

    expect(await screen.findByText("Not enough")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Complete sale" })).toBeDisabled();
  });
});

// ── the ledger ──────────────────────────────────────────────────────────────────

describe("the invoice list", () => {
  it("shows outstanding as absent on a draft, not as zero", async () => {
    // "0.00" reads as settled. A draft owes nothing because it has not been agreed, which is a
    // different fact and must not look the same.
    vi.mocked(wails.salesDocuments).mockResolvedValue([
      { ...SALE.document, id: "d1", status: "draft", outstandingMinor: "" },
      {
        ...SALE.document, id: "d2", status: "posted", number: "INV-0001",
        totalMinor: "9900", outstandingMinor: "4900",
      },
      {
        ...SALE.document, id: "d3", status: "posted", number: "INV-0002",
        totalMinor: "1200", outstandingMinor: "0",
      },
    ]);

    renderApp(<InvoicesScreen />);

    // Part-paid: the balance, and only the balance.
    expect(await screen.findByText("INV-0001")).toBeInTheDocument();
    expect(screen.getByText("49.00")).toBeInTheDocument();
    // Fully paid: a word, not a zero.
    expect(screen.getByText("Settled")).toBeInTheDocument();

    // The draft owes nothing because it has not been agreed. Its row shows a dash — and there
    // is exactly one "Settled" on the screen, so the draft did not quietly become one.
    expect(screen.getByText("Not yet numbered")).toBeInTheDocument();
    expect(screen.getAllByText("Settled")).toHaveLength(1);
  });
});

// ── printing ────────────────────────────────────────────────────────────────────

describe("printing", () => {
  const POSTED = {
    ...SALE,
    document: { ...SALE.document, status: "posted", number: "INV-0042" },
    editable: false,
  };

  beforeEach(() => {
    vi.mocked(wails.salesDocument).mockResolvedValue(POSTED);
    useSessionStore.getState().setSession({
      ...SIGNED_IN,
      permissions: [wails.PERMISSIONS.saleView, wails.PERMISSIONS.salePrint],
    });
  });

  it("offers no print button on a draft", async () => {
    // A draft has no number, no tax point, and no agreement behind it. Offering a control that
    // always refuses teaches people the software is broken.
    vi.mocked(wails.salesDocument).mockResolvedValue(SALE);

    renderApp(<InvoiceDetail documentId="d1" onBack={() => {}} />);

    expect(await screen.findByText("Bag of cement")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Print" })).not.toBeInTheDocument();
  });

  it("hides printing from someone who may only view", async () => {
    // A printed invoice LEAVES THE BUILDING. Looking up what a customer owes and producing a
    // document on company letterhead are different acts.
    useSessionStore.getState().setSession({
      ...SIGNED_IN,
      permissions: [wails.PERMISSIONS.saleView],
    });

    renderApp(<InvoiceDetail documentId="d1" onBack={() => {}} />);

    expect(await screen.findByText("INV-0042")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Print" })).not.toBeInTheDocument();
  });

  it("asks for the paper each template was designed for", async () => {
    // An 80mm receipt laid out on A4 wastes most of a page; an A4 invoice squeezed onto a roll
    // is unreadable. The template and the paper are one decision, made here rather than left to
    // whoever clicks.
    vi.mocked(wails.printSalesDocument).mockResolvedValue({
      html: "<!doctype html><html><body>INV-0042</body></html>",
      number: "INV-0042",
    });

    const user = userEvent.setup();
    renderApp(<InvoiceDetail documentId="d1" onBack={() => {}} />);

    await user.click(await screen.findByRole("button", { name: "Print" }));
    await waitFor(() =>
      expect(wails.printSalesDocument).toHaveBeenCalledWith("d1", "invoice", "A4"),
    );

    await user.click(screen.getByRole("button", { name: "Print receipt" }));
    await waitFor(() =>
      expect(wails.printSalesDocument).toHaveBeenCalledWith("d1", "receipt", "80mm"),
    );
  });

  it("prints from inside the page, and cleans the frame up afterwards", async () => {
    // A popup is blocked, steals focus, and leaves the operator on a blank tab. At a till with
    // a queue that is the difference between serving the next customer and hunting for a
    // window.
    const printed = vi.fn();
    vi.spyOn(HTMLIFrameElement.prototype, "contentWindow", "get").mockReturnValue({
      focus: vi.fn(),
      print: printed,
      addEventListener: (_: string, handler: () => void) => handler(),
    } as unknown as Window);

    const before = new Set(document.querySelectorAll("iframe"));
    printHTML("<!doctype html><html><body>INV-0042</body></html>");

    // THIS call's frame, not "any iframe on the page". An earlier test in this file prints too,
    // and jsdom never fires load on a srcdoc frame — so a broad query finds a leftover and the
    // assertion passes or fails for reasons that have nothing to do with the code.
    const frame = [...document.querySelectorAll("iframe")].find((each) => !before.has(each));
    expect(frame).toBeDefined();
    frame?.dispatchEvent(new Event("load"));

    expect(printed).toHaveBeenCalledTimes(1);
    // afterprint fired synchronously in the fake, so the frame is already detached. A till left
    // open for a fortnight must not accumulate one frame per receipt.
    expect(frame?.isConnected).toBe(false);
  });
});
