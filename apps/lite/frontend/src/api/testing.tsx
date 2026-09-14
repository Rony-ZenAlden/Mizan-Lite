// TEST-ONLY. A fake client and a render helper.
//
// Nothing outside a test may import this file. Two gates hold that: a source gate (no non-test file
// imports it) and G5, which fails if the sentinel appears anywhere in the built bundle.
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter } from "react-router-dom";
import { ClientProvider } from "./ClientContext";
import type { CartQuote, Client, Customer, Entry, Movement, Product, RateState, Sale, Statement } from "./client";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { Locale } from "@/i18n/messages";
import { OwnerProvider } from "@/owner/OwnerProvider";
import { RateProvider } from "@/rates/RateProvider";
import { FAKE_CLIENT_SENTINEL } from "./sentinel";

type Overrides = { [G in keyof Client]?: Partial<Client[G]> };

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
    ...overrides,
  };
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
    canFetch: true,
    hasFetch: false,
    lastFetch: aFetch(),
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

/** A client answering as a healthy, set-up application, with any method replaceable. */
export function fakeClient(overrides: Overrides = {}): Client {
  const base: Client = {
    app: {
      bootStatus: async () => ({ state: "ready", phase: "done", current: 0, total: 0 }),
      health: async () => ({ version: "1.0.0-test", schemaVersion: 2, platform: "darwin", dataDir: `/data/${FAKE_CLIENT_SENTINEL}` }),
      firstRunStatus: async () => ({ complete: true }),
      completeFirstRun: async () => ({ recoveryCode: "ABCD-EFGH-JKMN-PQRS" }),
    },
    settings: {
      get: async () => ({ locale: "ar", shopName: "بقالية المونة", direction: "rtl", debtCurrency: "USD" }),
      update: async (input) => ({
        locale: input.locale ?? "ar",
        shopName: input.shopName ?? "بقالية المونة",
        direction: input.locale === "en" ? "ltr" : "rtl",
        debtCurrency: "USD",
      }),
    },
    catalog: {
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
      fetchQuote: async () => ({ provider: "currency-api-jsdelivr", rate: "13007.5355" }),
    },
    till: {
      scan: async () => ({ found: false, productId: "", nameAr: "", nameEn: "", unitCode: "", unitDecimals: 0, active: false, onHand: "" }),
      quote: async () => aQuote(),
      checkout: async () => aSale(),
      cashNote: async () => ({ currency: "SYP", note: "500" }),
      setCashNote: async (note) => ({ currency: "SYP", note }),
    },
    sales: {
      list: async (businessDate) => ({
        businessDate: businessDate || "2026-09-14",
        sales: [aSale()],
        totals: [
          { currency: "SYP", sales: 1, charged: "73000", cashIn: "73000", changeOut: "0", voids: 0, refunded: "0", onCredit: "0" },
          { currency: "USD", sales: 0, charged: "0.00", cashIn: "0.00", changeOut: "0.00", voids: 0, refunded: "0.00", onCredit: "0.00" },
        ],
      }),
      receipt: async () => aSale(),
      void: async (input) => aSale({ status: "voided", voidReason: input.reason, voidedAt: "2026-09-14T09:00:00.000Z", voidBusinessDate: "2026-09-14" }),
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
