// TEST-ONLY. A fake client and a render helper.
//
// Nothing outside a test may import this file. Two gates hold that: a source gate (no non-test file
// imports it) and G5, which fails if the sentinel appears anywhere in the built bundle.
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter } from "react-router-dom";
import { ClientProvider } from "./ClientContext";
import type { USDOnlyPlan, LossReport, Amount, BackupInfo, BackupStatus, CartQuote, CashEntry, Client, Customer, DayReport, Drawer, Entry, Movement, PrinterSettings, Product, Profit, Purchase, PurchaseLine, RateState, RepriceProposal, AlertsState, Returnable, Sale, SaleReturn, Statement, StockReport, SettingsState, ShopState, Supplier, SupplierEntry, SupplierList, SupplierStatement } from "./client";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { Locale } from "@/i18n/messages";
import { OwnerProvider } from "@/owner/OwnerProvider";
import { RateProvider } from "@/rates/RateProvider";
import { FAKE_CLIENT_SENTINEL } from "./sentinel";

type Overrides = { [G in keyof Client]?: Partial<Client[G]> };

/** The shop's settings as Go sends them, with any field replaceable. */
export function aSettings(overrides: Partial<SettingsState> = {}): SettingsState {
  return {
    locale: "ar",
    shopName: "بقالية المونة",
    direction: "rtl",
    debtCurrency: "USD",
    localCurrency: "SYP",
    cashNote: "500",
    moneyDisplay: "legacy",
    pinRequired: false,
    rateSource: "standard",
    localRateUrl: "",
    localRateField: "",
    ...overrides,
  };
}

/** A product for tests, with any field replaceable. */
export function aProduct(overrides: Partial<Product> = {}): Product {
  return {
    id: "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
    nameAr: "زيت زيتون",
    nameEn: "Olive oil",
    barcode: "",
    unitCode: "l",
    priceCurrency: "USD",
    price: "3.25",
    quickSlot: 0,
    active: true,
    rowVersion: 1,
    packageContentId: "",
    packageContentQuantity: "",
    convertedPrice: "48750",
    convertedCurrency: "SYP",
    costPrice: "",
    marginAmount: "",
    marginPercent: "",
    reorderLevel: "",
    openPrice: false,
    unitsPerCarton: "",
    ...overrides,
  };
}

/** The store information as Go sends it: a name, a phone and a city, and no logo — with any field replaceable. */
export function aShop(overrides: Partial<ShopState> = {}): ShopState {
  return { name: "بقالية المونة", phone: "0933 123 456", city: "عفرين", address: "", logo: "", logoWidth: 0, logoHeight: 0, ...overrides };
}

/** The last internet check, as Go sends it, with any field replaceable. */
export function aFetch(overrides: Partial<RateState["lastFetch"]> = {}): RateState["lastFetch"] {
  return {
    id: "0190a1b2-0000-7000-8000-00000000f001",
    attemptedAt: "2026-09-14T06:00:00.000Z",
    ageSeconds: 600,
    outcome: "held_today",
    provider: "currency-api-jsdelivr",
    rate: "13007.5355",
    errorCode: "",
    change: "-13.3",
    effectiveRate: "13007.5355",
    acceptable: false,
    ...overrides,
  };
}

/** The rate in force, as Go sends it: set by the owner today, automatic mode, with any field replaceable. */
export function aRate(overrides: Partial<RateState> = {}): RateState {
  return {
    set: true,
    localCurrency: "SYP",
    rate: "15000",
    source: "manual",
    recordedAt: "2026-09-14T06:00:00.000Z",
    ageSeconds: 3 * 3600,
    stale: false,
    mode: "automatic",
    adjustPercent: "0",
    canFetch: true,
    hasFetch: false,
    lastFetch: aFetch(),
    usdOnly: false,
    ...overrides,
  };
}

/** A stock movement for tests, as Go sends it outside owner mode (no costs), with any field replaceable. */
export function aMovement(overrides: Partial<Movement> = {}): Movement {
  return {
    id: "0190a1b2-0000-7000-8000-00000000m001",
    kind: "receipt",
    businessDate: "2026-09-14",
    occurredAt: "2026-09-13T22:00:00.000Z",
    quantity: "25.000",
    onHandAfter: "65.000",
    reason: "",
    note: "",
    reversesId: "",
    pairId: "",
    unitCost: "",
    averageCostBefore: "",
    averageCostAfter: "",
    enteredCurrency: "",
    enteredUnitCost: "",
    rate: "",
    ...overrides,
  };
}

/** A priced cart as Go sends it: 1.5 L of olive oil, 73,000 pounds after rounding, paid with $5, with any field replaceable. */
export function aQuote(overrides: Partial<CartQuote> = {}): CartQuote {
  return {
    lines: [
      {
        productId: aProduct().id,
        nameAr: "زيت زيتون",
        nameEn: "Olive oil",
        unitCode: "l",
        quantity: "1.500",
        priceCurrency: "USD",
        unitPrice: "3.25",
        grossLocal: "73125",
        grossUsd: "4.88",
        discountPercent: "",
        discountLocal: "0",
        discountUsd: "0.00",
        netLocal: "73125",
        netUsd: "4.88",
        onHand: "12.500",
        warnings: [],
      },
    ],
    localCurrency: "SYP",
    linesLocal: "73125",
    linesUsd: "4.88",
    discountLocal: "0",
    discountUsd: "0.00",
    settlement: "SYP",
    total: "73000",
    rounding: "-125",
    cashNote: "500",
    otherCurrency: "USD",
    totalOther: "4.88",
    tenderCurrency: "SYP",
    tendered: "73000",
    tenderGiven: false,
    changeCurrency: "SYP",
    change: "0",
    discounted: false,
    rate: "15000",
    rateRecordedAt: "2026-09-14T06:00:00.000Z",
    rateStale: false,
    token: "token-1",
    payment: "cash",
    customerId: "",
    customerName: "",
    needsCustomer: false,
    debt: "",
    balanceBefore: "",
    balanceAfter: "",
    ...overrides,
  };
}

/** A recorded sale as Go sends it — receipt 7, the quote above paid exactly — with any field replaceable. */
export function aSale(overrides: Partial<Sale> = {}): Sale {
  const q = aQuote();
  return {
    id: "0190a1b2-0000-7000-8000-00000000s007",
    receiptNo: 7,
    businessDate: "2026-09-14",
    soldAt: "2026-09-14T08:30:00.000Z",
    status: "posted",
    payment: "cash",
    shopName: "بقالية المونة",
    localCurrency: "SYP",
    rate: "15000",
    rateRecordedAt: q.rateRecordedAt,
    settlement: "SYP",
    linesLocal: q.linesLocal,
    linesUsd: q.linesUsd,
    discountLocal: "0",
    discountUsd: "0.00",
    cashNote: "500",
    rounding: "-125",
    total: "73000",
    tenderCurrency: "SYP",
    tendered: "73000",
    changeCurrency: "SYP",
    change: "0",
    voidedAt: "",
    voidBusinessDate: "",
    voidReason: "",
    creditCustomerId: "",
    creditCustomerName: "",
    creditCurrency: "",
    creditAmount: "",
    creditBalanceAfter: "",
    creditReversed: false,
    voidReturn: "73000",
    voidReturnCurrency: "SYP",
    cartons: "",
    lines: q.lines.map((l, i) => ({
      id: `line-${i + 1}`,
      lineNo: i + 1,
      productId: l.productId,
      nameAr: l.nameAr,
      nameEn: l.nameEn,
      unitCode: l.unitCode,
      quantity: l.quantity,
      priceCurrency: l.priceCurrency,
      unitPrice: l.unitPrice,
      grossLocal: l.grossLocal,
      grossUsd: l.grossUsd,
      discountPercent: l.discountPercent,
      discountLocal: l.discountLocal,
      discountUsd: l.discountUsd,
      netLocal: l.netLocal,
      netUsd: l.netUsd,
      cartons: "",
    })),
    ...overrides,
  };
}

/** A customer as Go sends it — أبو محمد, owing 9.58 USD since 12 September — with any field replaceable. */
export function aCustomer(overrides: Partial<Customer> = {}): Customer {
  return {
    id: "0190a1b2-0000-7000-8000-00000000c001",
    name: "أبو محمد",
    phone: "0933 123 456",
    note: "الحلاق",
    city: "",
    active: true,
    rowVersion: 1,
    balances: [{ currency: "USD", balance: "9.58", owedSince: "2026-09-12", lastPayment: "2026-09-14", reference: "143700", referenceCurrency: "SYP" }],
    ...overrides,
  };
}

/** A debt book entry as Go sends it: a payment of 6.67 USD in pounds, with any field replaceable. */
export function anEntry(overrides: Partial<Entry> = {}): Entry {
  return {
    id: "0190a1b2-0000-7000-8000-00000000e002",
    seq: 2,
    businessDate: "2026-09-14",
    occurredAt: "2026-09-14T09:00:00.000Z",
    kind: "payment",
    currency: "USD",
    amount: "-6.67",
    balanceAfter: "9.58",
    saleId: "",
    reversesId: "",
    tenderedCurrency: "SYP",
    tendered: "100000",
    changeCurrency: "SYP",
    change: "0",
    rate: "15000",
    note: "",
    customerName: "أبو محمد",
    reversed: false,
    reversible: true,
    ...overrides,
  };
}

/** A statement as Go sends it: the customer's dollar chain, newest first. */
export function aStatement(overrides: Partial<Statement> = {}): Statement {
  return {
    customer: aCustomer(),
    currency: "USD",
    balance: "9.58",
    owedSince: "2026-09-12",
    lastPayment: "2026-09-14",
    entries: [
      anEntry(),
      anEntry({ id: "e1", seq: 1, businessDate: "2026-09-12", kind: "charge", amount: "16.25", balanceAfter: "16.25", saleId: aSale().id,
        tenderedCurrency: "", tendered: "", changeCurrency: "", change: "", rate: "", reversible: false }),
    ],
    rate: "15000",
    localCurrency: "SYP",
    ...overrides,
  };
}

/** A figure in both readings, with any field replaceable. */
export function anAmount(usd = "0.00", local = "0", overrides: Partial<Amount> = {}): Amount {
  return { usd, local, unconverted: 0, ...overrides };
}

/** A gross profit as Go sends it: one sale of 2 L of olive oil, $6.50 at a $4.00 cost, with any field replaceable. */
export function aProfit(overrides: Partial<Profit> = {}): Profit {
  return {
    sales: 1,
    revenueUsd: "6.50",
    costUsd: "4.00",
    profitUsd: "2.50",
    marginUsd: "38.5",
    revenueLocal: "97500",
    costLocal: "60000",
    profitLocal: "37500",
    marginLocal: "38.5",
    discountUsd: "0.00",
    discountLocal: "0",
    roundingLocal: "0",
    unknownLines: 0,
    unknownUsd: "0.00",
    unknownLocal: "0",
    openLines: 0,
    openUsd: "0.00",
    openLocal: "0",
    ...overrides,
  };
}

/** What the notification engine found: nothing, by default — a quiet shop with its backups in order. */
export function anAlerts(overrides: Partial<AlertsState> = {}): AlertsState {
  return {
    notifications: [],
    lowStock: [],
    stale: [],
    lossCount: 0,
    capital: {
      thenDate: "",
      nowDate: "",
      thenUsd: "",
      nowUsd: "",
      change: "",
      thenRate: "",
      nowRate: "",
      rateShift: "",
      depreciation: "",
      rising: false,
    },
    hasCapital: false,
    historyDays: 0,
    historyNeeded: 7,
    backup: "",
    ownerHidden: false,
    localCurrency: "SYP",
    ...overrides,
  };
}

/** A re-price proposal: one jar priced at 15,000 when the dollar was 15,000, proposed at 16,500. */
export function aRepriceProposal(overrides: Partial<RepriceProposal> = {}): RepriceProposal {
  return {
    rate: "16500",
    items: [
      { id: "jar", rowVersion: 3, nameAr: "دبس رمان", nameEn: "Pomegranate molasses", currency: "SYP", price: "15000", proposed: "16500", shift: "+10.0" },
    ],
    ...overrides,
  };
}

/** A sale the counter has found by its receipt number, ready to return part of. */
export function aReturnable(overrides: Partial<Returnable> = {}): Returnable {
  return {
    saleId: "sale-1",
    receiptNo: "1042",
    businessDate: "2026-09-14",
    soldAt: "2026-09-14T08:00:00.000Z",
    payment: "cash",
    settlementCurrency: "SYP",
    total: "73000",
    voided: false,
    customerId: "",
    customerName: "",
    lines: [
      {
        saleLineId: "line-1",
        lineNo: 1,
        productId: "p-1",
        nameAr: "زيت زيتون",
        nameEn: "Olive oil",
        unitCode: "l",
        sold: "2.000",
        returned: "0.000",
        left: "2.000",
        netLocal: "73000",
        netUsd: "4.87",
        returnable: true,
      },
    ],
    returns: [],
    ...overrides,
  };
}

/** A priced or recorded return. */
export function aSaleReturn(overrides: Partial<SaleReturn> = {}): SaleReturn {
  return {
    id: "return-1",
    returnNo: "1",
    saleId: "sale-1",
    saleReceiptNo: "1042",
    businessDate: "2026-09-14",
    returnedAt: "2026-09-14T09:00:00.000Z",
    settlement: "cash",
    currency: "SYP",
    refund: "36500",
    costKnown: true,
    reason: "الزبون رجّع علبة",
    lines: [
      {
        saleLineId: "line-1",
        lineNo: 1,
        productId: "p-1",
        nameAr: "زيت زيتون",
        nameEn: "Olive oil",
        unitCode: "l",
        quantity: "1.000",
        refundLocal: "36500",
        refundUsd: "2.43",
        restocked: true,
      },
    ],
    ...overrides,
  };
}

/** A day's statement as Go sends it — the sale above, a spoiled jar and the electricity bill — with any field replaceable. */
export function aDayReport(overrides: Partial<DayReport> = {}): DayReport {
  return {
    date: "2026-09-14",
    localCurrency: "SYP",
    profit: aProfit(),
    losses: {
      spoiled: anAmount("0.40", "6000"),
      ownUse: anAmount(),
      other: anAmount(),
      shortfall: anAmount(),
      surplus: anAmount(),
      out: anAmount("0.40", "6000"),
    },
    badDebts: anAmount(),
    returns: { count: 0, refund: anAmount(), cost: anAmount(), profit: anAmount(), unknown: 0 },
    expenses: anAmount("1.00", "15000"),
    dailyExpenses: anAmount("1.00", "15000"),
    periodicExpenses: anAmount(),
    categories: [{ category: "electricity", amount: anAmount("1.00", "15000") }],
    netUsd: "1.10",
    netLocal: "16500",
    unconverted: 0,
    takings: [
      { currency: "USD", sales: 0, charged: "0.00", creditSales: 0, credit: "0.00", discounts: "0.00", rounding: "0.00", voids: 0, voided: "0.00", collected: "0.00", refunded: "0.00", writtenOff: "0.00" },
      { currency: "SYP", sales: 1, charged: "97500", creditSales: 0, credit: "0", discounts: "0", rounding: "0", voids: 0, voided: "0", collected: "0", refunded: "0", writtenOff: "0" },
    ],
    rate: "15000",
    ...overrides,
  };
}

/** A stock report as Go sends it: 8 L of olive oil left of 10, with any field replaceable. */
export function aStockReport(overrides: Partial<StockReport> = {}): StockReport {
  const line = { productId: aProduct().id, nameAr: "زيت زيتون", nameEn: "Olive oil", unitCode: "l", onHand: "8.000", averageCost: "2.00", valueUsd: "16.00", valueLocal: "240000" };
  return {
    from: "2026-09-01",
    to: "2026-09-14",
    localCurrency: "SYP",
    lines: [line],
    totalUsd: "16.00",
    totalLocal: "240000",
    valueRate: "15000",
    belowZero: [],
    unknownCost: [],
    reconciliation: {
      from: "2026-09-01",
      to: "2026-09-14",
      opening: "0.00",
      received: "20.00",
      sold: "-4.00",
      losses: "0.00",
      gains: "0.00",
      revaluation: "0.00",
      packages: "0.00",
      negativeStock: "0.00",
      rounding: "0.00",
      closing: "16.00",
    },
    shelf: [{ ...line, price: "3.25", priceCurrency: "USD", profitUsd: "10.00", profitLocal: "150000", belowCost: false }],
    shelfTotalUsd: "10.00",
    shelfTotalLocal: "150000",
    retailTotalUsd: "26.00",
    retailTotalLocal: "390000",
    shelfRate: "15000",
    belowCost: 0,
    leftOut: [],
    ...overrides,
  };
}

/** A cash book entry as Go sends it: the counter's pounds count, 500 short, with any field replaceable. */
export function aCashEntry(overrides: Partial<CashEntry> = {}): CashEntry {
  return {
    id: "0190a1b2-0000-7000-8000-0000000cash1",
    seq: 1,
    businessDate: "2026-09-14",
    occurredAt: "2026-09-14T17:00:00.000Z",
    kind: "count",
    currency: "SYP",
    amount: "97000",
    expected: "97500",
    difference: "-500",
    category: "",
    fromDrawer: false,
    note: "",
    reversesKind: "",
    reversed: false,
    reversible: false,
    ...overrides,
  };
}

/** A day's drawer as Go sends it at the counter: 97,500 pounds expected, not counted yet, with any field replaceable. */
export function aDrawer(overrides: Partial<Drawer> = {}): Drawer {
  const zero = (c: string) => (c === "USD" ? "0.00" : "0");
  const currency = (c: string) => ({
    currency: c,
    opening: zero(c),
    openingCountDate: "",
    cashSalesIn: c === "USD" ? "0.00" : "97500",
    creditPaidIn: zero(c),
    repaymentsIn: zero(c),
    depositsIn: zero(c),
    changeOut: zero(c),
    refundsOut: zero(c),
    voidReturns: zero(c),
    expensesOut: zero(c),
    withdrawalsOut: zero(c),
    returnsOut: zero(c),
    suppliersOut: zero(c),
    suppliersIn: zero(c),
    expected: c === "USD" ? "0.00" : "97500",
    counted: false,
    count: "",
    countExpected: "",
    difference: "",
    countedAt: "",
  });
  return {
    date: "2026-09-14",
    today: "2026-09-14",
    ownerView: false,
    currencies: [currency("USD"), currency("SYP")],
    entries: [],
    categories: ["rent", "electricity", "wages", "transport", "supplies", "other"],
    ...overrides,
  };
}

/** Going over to dollars only in the demo shop: one pound price, a customer, a supplier and the drawer's pounds. */
export function aUSDOnlyPlan(overrides: Partial<USDOnlyPlan> = {}): USDOnlyPlan {
  return {
    localCurrency: "SYP",
    rate: "15000",
    prices: [
      { productId: "p-labneh", nameAr: "لبنة", nameEn: "Labneh", localPrice: "60000", usdPrice: "4.00", localCost: "45000", usdCost: "3.00", raisedToCent: false },
      { productId: "p-gum", nameAr: "علكة", nameEn: "Gum", localPrice: "25", usdPrice: "0.01", localCost: "", usdCost: "", raisedToCent: true },
    ],
    openItems: 1,
    customers: [{ id: "c-1", name: "سمير", local: "150000", dollars: "10.00" }],
    suppliers: [{ id: "s-1", name: "المروى", local: "300000", dollars: "20.00" }],
    drawerLocal: "375000",
    drawerDollars: "25.00",
    token: "plan-token",
    ...overrides,
  };
}

/** The month's losses: 1.5 kg of labneh gone bad, and two litres of oil that arrived broken and were not charged. */
export function aLossReport(overrides: Partial<LossReport> = {}): LossReport {
  return {
    from: "2026-09-01",
    to: "2026-09-24",
    localCurrency: "SYP",
    lines: [
      {
        movementId: "m-1",
        businessDate: "2026-09-20",
        productId: "p-labneh",
        nameAr: "لبنة بلدية",
        nameEn: "Labneh",
        unitCode: "kg",
        reason: "spoiled",
        quantity: "1.500",
        value: { usd: "6.00", local: "90000", unconverted: 0 },
        note: "من الحر",
      },
    ],
    byReason: [{ reason: "spoiled", lines: 1, value: { usd: "6.00", local: "90000", unconverted: 0 } }],
    total: { usd: "6.00", local: "90000", unconverted: 0 },
    arrival: [
      {
        businessDate: "2026-09-05",
        purchaseNo: 7,
        supplierName: "المروى",
        productId: "p-oil",
        nameAr: "زيت زيتون",
        nameEn: "Olive oil",
        unitCode: "l",
        damaged: "2.000",
        currency: "USD",
        value: "5.00",
      },
    ],
    ...overrides,
  };
}

/** A supplier as Go lists one: المروى of Aleppo, owed $22.00, with any field replaceable. */
export function aSupplier(overrides: Partial<Supplier> = {}): Supplier {
  return {
    id: "sup-1",
    name: "المروى",
    phone: "0988 703 785",
    city: "حلب",
    note: "",
    active: true,
    rowVersion: 1,
    balances: [{ currency: "USD", balance: "22.00" }],
    ...overrides,
  };
}

/** The suppliers screen's list: one supplier, the shop owing $22.00 in all, the rate 15,000. */
export function aSupplierList(overrides: Partial<SupplierList> = {}): SupplierList {
  return { suppliers: [aSupplier()], totals: [{ currency: "USD", balance: "22.00" }], localCurrency: "SYP", rate: "15000", ...overrides };
}

/** A line of a supplier's book: purchase 7, $72.00, with any field replaceable. */
export function aSupplierEntry(overrides: Partial<SupplierEntry> = {}): SupplierEntry {
  return {
    id: "sen-1",
    seq: 1,
    businessDate: "2026-09-24",
    occurredAt: "2026-09-24T09:00:00.000Z",
    kind: "purchase",
    currency: "USD",
    amount: "72.00",
    balanceAfter: "72.00",
    source: "",
    purchaseId: "pur-7",
    purchaseNo: 7,
    supplierRef: "2970",
    reversesId: "",
    note: "",
    reversed: false,
    reversible: false,
    ...overrides,
  };
}

/** A supplier's dollar book: purchase 7 and $50.00 paid from the drawer, $22.00 still owed. */
export function aSupplierStatement(overrides: Partial<SupplierStatement> = {}): SupplierStatement {
  return {
    supplier: aSupplier(),
    currency: "USD",
    balance: "22.00",
    entries: [
      aSupplierEntry({ id: "sen-2", seq: 2, kind: "payment", amount: "-50.00", balanceAfter: "22.00", source: "drawer", reversible: true }),
      aSupplierEntry(),
    ],
    ...overrides,
  };
}

/** A purchase line: ten litres of olive oil at $2.00, two broken, 10% off — eight received at $1.75. */
export function aPurchaseLine(overrides: Partial<PurchaseLine> = {}): PurchaseLine {
  return {
    lineNo: 1,
    productId: "p-oil",
    nameAr: "زيت زيتون",
    nameEn: "Olive oil",
    unitCode: "l",
    quantity: "10.000",
    damaged: "2.000",
    good: "8.000",
    unitCost: "2.00",
    discountPercent: "10",
    gross: "16.00",
    lineDiscount: "1.60",
    invoiceShare: "0.40",
    due: "14.00",
    netUnitCost: "1.75",
    ...overrides,
  };
}

/** Purchase 7 from المروى: $14.00 after every discount, $5.00 paid from the drawer. */
export function aPurchase(overrides: Partial<Purchase> = {}): Purchase {
  return {
    id: "pur-7",
    purchaseNo: 7,
    supplierId: "sup-1",
    supplierName: "المروى",
    businessDate: "2026-09-24",
    occurredAt: "2026-09-24T09:00:00.000Z",
    currency: "USD",
    rate: "",
    supplierRef: "2970",
    lines: [aPurchaseLine()],
    gross: "16.00",
    lineDiscount: "1.60",
    invoiceDiscount: "0.40",
    due: "14.00",
    paidNow: "5.00",
    paidFrom: "drawer",
    status: "posted",
    voidedAt: "",
    voidReason: "",
    note: "",
    damagedLines: 1,
    ...overrides,
  };
}

/** A backup as Go lists it: this morning's scheduled one, copied outside, with any field replaceable. */
export function aBackup(overrides: Partial<BackupInfo> = {}): BackupInfo {
  return {
    name: "scheduled-20260914T060000Z.db",
    reason: "scheduled",
    takenAt: "2026-09-14T06:00:00.000Z",
    sizeBytes: "2457600",
    schemaVersion: 8,
    outside: true,
    ageSeconds: 10_800,
    ...overrides,
  };
}

/** The backup status Home shows: a backup this morning and its outside copy, nothing stale, with any field replaceable. */
export function aBackupStatus(overrides: Partial<BackupStatus> = {}): BackupStatus {
  return { last: aBackup(), folder: "/Volumes/USB", lastOutside: aBackup(), outsideStale: false, outsideFailed: "", restored: undefined, every: "daily", ...overrides } as BackupStatus;
}

/** Printer settings as Go sends them: an 80 mm printer through its driver, credit printed automatically. */
export function printerSettings(overrides: Partial<PrinterSettings> = {}): PrinterSettings {
  return { printer: "Xprinter XP-80", paperMm: 80, path: "driver", autoPrint: "credit", drawer: false, footer: "", invoicePrinter: "", ...overrides };
}

/** A client answering as a healthy, set-up application, with any method replaceable. */
export function fakeClient(overrides: Overrides = {}): Client {
  const base: Client = {
    app: {
      bootStatus: async () => ({ state: "ready", phase: "done", current: 0, total: 0 }),
      health: async () => ({ version: "1.0.0-test", schemaVersion: 2, platform: "darwin", dataDir: `/data/${FAKE_CLIENT_SENTINEL}` }),
      firstRunStatus: async () => ({ complete: true }),
      completeFirstRun: async () => ({ recoveryCode: "ABCD-EFGH-JKMN-PQRS" }),
      about: async () => ({
        version: "0.9.0",
        schemaVersion: 8,
        platform: "windows",
        arch: "amd64",
        dataDir: "C:\\Users\\shop\\AppData\\Local\\Mizan Lite",
        logsDir: "C:\\Users\\shop\\AppData\\Local\\Mizan Lite\\logs",
        backupsDir: "C:\\Users\\shop\\AppData\\Local\\Mizan Lite\\backups",
        guides: ["install-ar", "install-en", "quick-card-ar", "shop-guide-ar", "shop-guide-en", "troubleshooting-ar", "troubleshooting-en"],
        notices: "Mizan Lite — third-party notices\n\n== IBM Plex Sans Arabic (font) — SIL Open Font License 1.1",
      }),
      saveGuide: async (name) => ({ path: `/Users/shop/Documents/${name}.pdf`, bytes: 1, cancelled: false }),
      saveSupportFile: async () => ({ path: "/Users/shop/Documents/support.zip", bytes: 1, cancelled: false }),
    },
    settings: {
      get: async () => aSettings(),
      update: async (input) =>
        aSettings({
          locale: input.locale ?? "ar",
          shopName: input.shopName ?? "بقالية المونة",
          direction: input.locale === "en" ? "ltr" : "rtl",
          moneyDisplay: input.moneyDisplay ?? "legacy",
          rateSource: input.rateSource ?? "standard",
          localRateUrl: input.localRateUrl ?? "",
          localRateField: input.localRateField ?? "",
        }),
      shop: async () => aShop(),
      saveShop: async (input) => aShop(input),
      pickLogoFile: async () => ({ path: "/Users/shop/Pictures/logo.png", cancelled: false }),
      setLogo: async () => aShop({ logo: "iVBORw0KGgo=", logoWidth: 300, logoHeight: 120 }),
      removeLogo: async () => aShop(),
      usdOnlyPlan: async () => aUSDOnlyPlan(),
      switchToUsdOnly: async () => aSettings({ moneyDisplay: "usd" }),
    },
    catalog: {
      importTemplate: async () => ({ path: "/Users/shop/Documents/products.xlsx", bytes: 1, cancelled: false }),
      importPreview: async () => ({
        path: "/Users/shop/Documents/products.xlsx",
        digest: "abc",
        rows: [
          { row: 2, nameAr: "رز مصري", nameEn: "Egyptian rice", barcode: "6290000000011", unitCode: "kg", currency: "USD", price: "1.10", quantity: "25.5", cost: "0.80" },
          { row: 3, nameAr: "سكر", nameEn: "", barcode: "", unitCode: "kg", currency: "SYP", price: "12000", quantity: "", cost: "" },
        ],
        problems: [],
        cancelled: false,
      }),
      importApply: async () => ({ created: 2, withStock: 1 }),
      units: async () => [
        { code: "kg", kind: "mass", inputDecimals: 3 },
        { code: "l", kind: "volume", inputDecimals: 3 },
        { code: "jar", kind: "count", inputDecimals: 0 },
      ],
      currencies: async () => [
        { code: "SYP", decimals: 0 },
        { code: "USD", decimals: 2 },
      ],
      products: async () => [aProduct()],
      product: async () => aProduct(),
      createProduct: async (input) => aProduct({ ...input, id: "new", rowVersion: 1 }),
      updateProduct: async (input) => aProduct({ ...input, rowVersion: input.rowVersion + 1 }),
      setPrice: async (input) => aProduct({ id: input.id, priceCurrency: input.priceCurrency, price: input.price, rowVersion: input.rowVersion + 1 }),
      setReorder: async (input) => aProduct({ id: input.id, reorderLevel: input.level, rowVersion: input.rowVersion + 1 }),
      repriceProposal: async () => aRepriceProposal(),
      bulkReprice: async (items) => items.map((i) => aProduct({ id: i.id, price: i.price, rowVersion: i.rowVersion + 1 })),
      setActive: async (input) => aProduct({ id: input.id, active: input.active, rowVersion: input.rowVersion + 1 }),
      setQuickSlot: async (input) => aProduct({ id: input.id, quickSlot: input.slot }),
      setPackage: async (input) =>
        aProduct({ id: input.packageProductId, packageContentId: input.contentProductId, packageContentQuantity: input.contentQuantity }),
      clearPackage: async (productId) => aProduct({ id: productId }),
    },
    stock: {
      levels: async () => [{ productId: aProduct().id, onHand: "12.500" }],
      valuation: async () => ({
        lines: [{ productId: aProduct().id, onHand: "12.500", averageCost: "3.20", value: "40.00", valueLocal: "600000" }],
        total: "40.00",
        totalLocal: "600000",
        localCurrency: "SYP",
        rate: "15000",
      }),
      movements: async () => ({ movements: [aMovement()], costsVisible: false, reversibleId: "" }),
      receive: async (input) => ({ productId: input.productId, onHand: "1" }),
      opening: async (input) => ({ productId: input.productId, onHand: "1" }),
      count: async (input) => ({ productId: input.productId, onHand: input.counted }),
      adjust: async (input) => ({ productId: input.productId, onHand: "1" }),
      openPackage: async (input) => [
        { productId: input.packageProductId, onHand: "2" },
        { productId: "content", onHand: "16.000" },
      ],
      reverseReceipt: async () => ({ productId: aProduct().id, onHand: "40.000" }),
      correctCost: async (input) => ({ productId: input.productId, onHand: "12.500" }),
      verify: async () => [],
    },
    fx: {
      current: async () => aRate(),
      history: async () => [
        { id: "r2", rate: "15000", source: "manual", provider: "", recordedAt: "2026-09-14T07:00:00.000Z", change: "-1.3", note: "تصحيح" },
        { id: "r1", rate: "15200", source: "manual", provider: "", recordedAt: "2026-09-14T06:00:00.000Z", change: "", note: "" },
      ],
      setRate: async (input) => aRate({ rate: input.rate }),
      refresh: async () => aRate({ hasFetch: true }),
      acceptProposal: async () => aRate({ source: "fetched", rate: "13007.5355", hasFetch: true }),
      setMode: async (mode) => aRate({ mode }),
      setAdjustPercent: async (adjustPercent) => aRate({ adjustPercent }),
      fetchQuote: async () => ({ provider: "currency-api-jsdelivr", rate: "13007.5355" }),
    },
    till: {
      scan: async () => ({ found: false, productId: "", nameAr: "", nameEn: "", unitCode: "", unitDecimals: 0, active: false, onHand: "", openPrice: false, priceCurrency: "" }),
      quote: async () => aQuote(),
      checkout: async () => aSale(),
      cashNote: async () => ({ currency: "SYP", note: "500" }),
      setCashNote: async (note) => ({ currency: "SYP", note }),
      // Go makes the shop's open item in its own currency (catalog.OpenItem).
      openItem: async () =>
        aProduct({ id: "misc", nameAr: "متفرقات", nameEn: "Miscellaneous", unitCode: "piece", priceCurrency: "SYP", price: "0", openPrice: true }),
    },
    sales: {
      list: async (businessDate) => ({
        businessDate: businessDate || "2026-09-14",
        sales: [aSale()],
        totals: [
          { currency: "SYP", sales: 1, charged: "73000", cashIn: "73000", changeOut: "0", voids: 0, voided: "0", onCredit: "0" },
          { currency: "USD", sales: 0, charged: "0.00", cashIn: "0.00", changeOut: "0.00", voids: 0, voided: "0.00", onCredit: "0.00" },
        ],
      }),
      receipt: async () => aSale(),
      void: async (input) => aSale({ status: "voided", voidReason: input.reason, voidedAt: "2026-09-14T09:00:00.000Z", voidBusinessDate: "2026-09-14" }),
      returnable: async (receiptNo) => aReturnable({ receiptNo }),
      quoteReturn: async (input) => aSaleReturn({ id: "", saleId: input.saleId, reason: input.reason, settlement: input.settlement }),
      recordReturn: async (input) => aSaleReturn({ saleId: input.saleId, reason: input.reason, settlement: input.settlement }),
      verify: async () => [],
    },
    customers: {
      search: async () => [aCustomer()],
      create: async (input) => aCustomer({ id: "new", name: input.name, phone: input.phone, note: input.note, balances: [] }),
      update: async (input) => aCustomer({ id: input.id, name: input.name, phone: input.phone, note: input.note, rowVersion: input.rowVersion + 1 }),
      setActive: async (input) => aCustomer({ id: input.id, active: input.active, rowVersion: input.rowVersion + 1 }),
      statement: async (_customerId, currency) => aStatement({ currency }),
      outstanding: async () => ({
        businessDate: "2026-09-14",
        customers: [aCustomer()],
        today: [{ currency: "USD", payments: 1, settled: "6.67", cashIn: "0.00", changeOut: "0.00", refunds: 0, refundOut: "0.00", charged: "16.25", writtenOff: "0.00" }],
        rate: "15000",
        localCurrency: "SYP",
      }),
      quotePayment: async (input) => ({
        currency: input.currency,
        tenderCurrency: input.tenderCurrency || input.currency,
        tendered: "100000",
        changeCurrency: "SYP",
        change: "0",
        settled: "6.67",
        balanceBefore: "16.25",
        balanceAfter: "9.58",
        all: input.all,
        rate: "15000",
        token: "pay-token",
      }),
      recordPayment: async () => anEntry(),
      opening: async (input) => anEntry({ kind: "opening", amount: input.amount, currency: input.currency, note: input.note }),
      writeOff: async (input) => anEntry({ kind: "write_off", amount: `-${input.amount}`, note: input.note }),
      refund: async (input) => anEntry({ kind: "refund", note: input.reason }),
      reverse: async (input) => anEntry({ kind: "reversal", reversesId: input.entryId, note: input.reason }),
    },
    suppliers: {
      list: async () => aSupplierList(),
      create: async (input) => aSupplier({ id: "sup-new", name: input.name, phone: input.phone, city: input.city, note: input.note, balances: [] }),
      update: async (input) => aSupplier({ id: input.id, name: input.name, phone: input.phone, city: input.city, note: input.note, rowVersion: input.rowVersion + 1 }),
      setActive: async (id, rowVersion, active) => aSupplier({ id, active, rowVersion: rowVersion + 1 }),
      statement: async (_supplierId, currency) => aSupplierStatement({ currency }),
      quotePurchase: async () => ({ purchase: aPurchase({ id: "", purchaseNo: 0, status: "" }), balanceBefore: "22.00", balanceAfter: "31.00" }),
      recordPurchase: async () => aPurchase(),
      voidPurchase: async (purchaseId, reason) => aPurchase({ id: purchaseId, status: "voided", voidReason: reason, voidedAt: "2026-09-24T10:00:00.000Z" }),
      purchase: async (purchaseId) => aPurchase({ id: purchaseId }),
      purchases: async () => [aPurchase()],
      pay: async (input) => aSupplierEntry({ id: "sen-pay", kind: "payment", amount: `-${input.amount}`, currency: input.currency, source: input.source, reversible: true }),
      refund: async (input) => aSupplierEntry({ id: "sen-refund", kind: "refund", amount: input.amount, currency: input.currency, source: input.source, reversible: true }),
      opening: async (input) => aSupplierEntry({ id: "sen-open", kind: "opening", amount: input.inShopsFavour ? `-${input.amount}` : input.amount, currency: input.currency }),
      reverse: async (entryId, reason) => aSupplierEntry({ id: "sen-rev", kind: "reversal", reversesId: entryId, note: reason }),
    },
    reports: {
      day: async (date) => aDayReport({ date: date || "2026-09-14" }),
      month: async (month) => ({
        month: month || "2026-09",
        from: "2026-09-01",
        to: "2026-09-30",
        localCurrency: "SYP",
        days: [aDayReport()],
        total: aDayReport({ rate: "" }),
      }),
      period: async (from, to) => ({
        month: "",
        from: from || "2026-09-01",
        to: to || "2026-09-14",
        localCurrency: "SYP",
        days: [aDayReport()],
        total: aDayReport({ rate: "" }),
      }),
      products: async (from, to) => ({
        from: from || "2026-09-01",
        to: to || "2026-09-14",
        localCurrency: "SYP",
        rows: [
          {
            productId: aProduct().id,
            nameAr: "زيت زيتون",
            nameEn: "Olive oil",
            unitCode: "l",
            quantity: "2.000",
            revenueUsd: "6.50",
            costUsd: "4.00",
            profitUsd: "2.50",
            marginUsd: "38.5",
            revenueLocal: "97500",
            costLocal: "60000",
            profitLocal: "37500",
            marginLocal: "38.5",
            unknownLines: 0,
            unknownQuantity: "0.000",
            unknownUsd: "0.00",
            unknownLocal: "0",
            openLines: 0,
            openUsd: "0.00",
            openLocal: "0",
          },
        ],
        discountUsd: "0.00",
        discountLocal: "0",
        roundingLocal: "0",
        total: aProfit(),
      }),
      stock: async () => aStockReport(),
      losses: async (from, to) => aLossReport({ from: from || "2026-09-01", to: to || "2026-09-24" }),
    },
    cash: {
      drawer: async (date) => aDrawer({ date: date || "2026-09-14" }),
      record: async (input) => aCashEntry({ kind: input.kind, currency: input.currency, amount: input.amount, category: input.category, fromDrawer: input.fromDrawer, note: input.note, expected: "", difference: "" }),
      count: async (input) => aCashEntry({ currency: input.currency, amount: input.counted }),
      reverse: async (entryId, reason) => aCashEntry({ id: `${entryId}-r`, kind: "reversal", note: reason, expected: "", difference: "" }),
    },
    exports: {
      report: async () => ({ path: "/Users/shop/Documents/report.xlsx", bytes: 4096, cancelled: false }),
      statement: async () => ({ path: "/Users/shop/Documents/statement.pdf", bytes: 4096, cancelled: false }),
      debtLedger: async () => ({ path: "/Users/shop/Documents/ledger.xlsx", bytes: 4096, cancelled: false }),
      salesHistory: async () => ({ path: "/Users/shop/Documents/sales.xlsx", bytes: 4096, cancelled: false }),
      invoice: async () => ({ path: "/Users/shop/Documents/invoice.pdf", bytes: 4096, cancelled: false }),
      showInFolder: async () => true,
    },
    print: {
      sale: async () => ({ printer: "Xprinter XP-80", copyNo: 1, path: "driver" }),
      entry: async () => ({ printer: "Xprinter XP-80", copyNo: 1, path: "driver" }),
      preview: async () => ({ png: "iVBORw0KGgo=", width: 576, height: 600, copyNo: 1 }),
      invoice: async () => ({ printer: "Office Laser", copyNo: 1, path: "driver" }),
      label: async () => ({ printer: "Xprinter XP-80", copyNo: 1, path: "driver" }),
      zReport: async () => ({ printer: "Xprinter XP-80", copyNo: 1, path: "driver" }),
    },
    printers: {
      list: async () => [{ name: "Xprinter XP-80", default: true }, { name: "Office Laser", default: false }],
      settings: async () => printerSettings(),
      save: async (input) => printerSettings({ ...input, paperMm: Number(input.paperMm) }),
      test: async () => ({ printer: "Xprinter XP-80", copyNo: 1, path: "driver" }),
    },
    alerts: {
      current: async () => anAlerts(),
    },
    backups: {
      list: async () => [aBackup(), aBackup({ name: "on_close-20260913T190000Z.db", reason: "on_close", takenAt: "2026-09-13T19:00:00.000Z", outside: false })],
      takeNow: async () => aBackup({ name: "on_demand-20260914T090000Z.db", reason: "on_demand", takenAt: "2026-09-14T09:00:00.000Z" }),
      status: async () => aBackupStatus(),
      lossPreview: async (name) => ({ backup: aBackup({ name }), sales: 214, voids: 2, debtEntries: 9, cashEntries: 3, stockMovements: 31 }),
      restore: async () => ({ staged: true, restarting: true }),
      restoreFromFile: async () => aBackup({ name: "imported-20260914T090000Z.db", reason: "imported" }),
      saveCopy: async () => ({ path: "/Volumes/USB/copy.db", bytes: 0, cancelled: false }),
      setBackupEvery: async (every) => aBackupStatus({ every }),
      setOutsideFolder: async (clear) => aBackupStatus({ folder: clear ? "" : "/Volumes/USB" }),
    },
    owner: {
      status: async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: 0 }),
      elevate: async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: 120 }),
      endElevation: async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: 0 }),
      changePin: async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: 0 }),
      recover: async () => ({ recoveryCode: "WXYZ-2345-6789-ABCD" }),
      events: async () => [],
    },
  };
  return {
    app: { ...base.app, ...overrides.app },
    settings: { ...base.settings, ...overrides.settings },
    catalog: { ...base.catalog, ...overrides.catalog },
    owner: { ...base.owner, ...overrides.owner },
    stock: { ...base.stock, ...overrides.stock },
    fx: { ...base.fx, ...overrides.fx },
    till: { ...base.till, ...overrides.till },
    sales: { ...base.sales, ...overrides.sales },
    customers: { ...base.customers, ...overrides.customers },
    suppliers: { ...base.suppliers, ...overrides.suppliers },
    reports: { ...base.reports, ...overrides.reports },
    cash: { ...base.cash, ...overrides.cash },
    exports: { ...base.exports, ...overrides.exports },
    print: { ...base.print, ...overrides.print },
    printers: { ...base.printers, ...overrides.printers },
    backups: { ...base.backups, ...overrides.backups },
    alerts: { ...base.alerts, ...overrides.alerts },
  };
}

/** Renders inside the providers the application has, in a chosen locale and route. */
export function renderWithProviders(
  ui: ReactElement,
  { client = fakeClient(), locale = "ar", route = "/" }: { client?: Client; locale?: Locale; route?: string } = {},
) {
  return render(
    <ClientProvider client={client}>
      <LocaleProvider initial={locale}>
        <OwnerProvider>
          <RateProvider>
            <MemoryRouter initialEntries={[route]} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
              {ui}
            </MemoryRouter>
          </RateProvider>
        </OwnerProvider>
      </LocaleProvider>
    </ClientProvider>,
  );
}
