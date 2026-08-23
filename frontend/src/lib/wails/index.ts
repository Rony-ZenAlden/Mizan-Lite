// The typed binding surface. Every Go call in the application goes through here.
//
// Each function is a thin, typed wrapper over `call`, which performs the single envelope
// unwrap (§3.1). Components never see a Result<T> and never touch window.go.

import { onEvent } from "./bridge";
import { call } from "./call";

export { BindingError, isBindingError } from "./call";
export type { APIError, FieldError, Result } from "./types";

// ── Boot ────────────────────────────────────────────────────────────────────────
//
// The one binding callable before the object graph exists — it is what reports whether the
// graph exists. Everything else returns a typed not-ready error until boot succeeds.

export type BootState = "starting" | "ready" | "failed";

export interface BootStatus {
  state: BootState;
  /** migrate.Phase: checking | backup | migrating | restoring | done */
  phase: string;
  current: number;
  total: number;
  name: string;
  /** Present only when state is "failed". Rendered from code + params, never as prose. */
  error: { code: string; messageKey: string; params?: Record<string, string> } | null;
  /** Where the pre-migration snapshot was written, when a migration failed. */
  backupPath: string;
}

export function bootStatus(): Promise<BootStatus> {
  return call<BootStatus>("Boot", "Status");
}

/**
 * Subscribes to boot progress.
 *
 * Events are an optimisation, not the source of truth: a window that finishes loading after
 * boot completed would never receive one. The boot screen therefore polls `bootStatus()` on
 * mount and treats events as a way to update sooner.
 */
export function onBootEvent(callback: () => void): () => void {
  const offProgress = onEvent("boot:progress", callback);
  const offReady = onEvent("boot:ready", callback);
  const offFailed = onEvent("boot:failed", callback);
  return () => {
    offProgress();
    offReady();
    offFailed();
  };
}

// ── Auth ────────────────────────────────────────────────────────────────────────

export interface SessionInfo {
  signedIn: boolean;
  userId: string;
  username: string;
  displayName: string;
  mustChange: boolean;
  /** Cosmetic only — the backend re-checks every call (§14.3). */
  permissions: string[];
}

/** Signs in. The session token never crosses this boundary; it lives in the Go process. */
export function login(username: string, password: string, remember: boolean): Promise<SessionInfo> {
  return call<SessionInfo>("Auth", "Login", username, password, remember);
}

export function logout(): Promise<boolean> {
  return call<boolean>("Auth", "Logout");
}

/** Asks "am I signed in, and as whom?" — resolves with signedIn:false rather than throwing. */
export function me(): Promise<SessionInfo> {
  return call<SessionInfo>("Auth", "Me");
}

// ── Setup ───────────────────────────────────────────────────────────────────────
//
// The wizard. Both methods are Public on the Go side (§WIZ.1's bootstrap paradox): no user
// exists on a fresh install, so nothing about first-run setup can be permissioned. `Apply`
// carries a second gate there — it refuses once a company exists, forever.

export interface SetupCountry {
  code: string;
  /** A translation key, not a name: the country list is translated like everything else. */
  nameKey: string;
  defaultLocale: string;
  supportedLocales: string[];
  functionalCurrency: string;
  pricingCurrency: string;
  fiscalYearStart: number;
  dateFormat: string;
  firstDayOfWeek: number;
  phoneCode: string;
}

export interface SetupBusinessProfile {
  code: string;
  nameKey: string;
  name: string;
  description: string;
}

export interface SetupCurrency {
  code: string;
  name: string;
  symbol: string;
}

export interface SetupOptions {
  countries: SetupCountry[];
  businessProfiles: SetupBusinessProfile[];
  currencies: SetupCurrency[];
  locales: string[];
}

export interface SetupStatus {
  required: boolean;
  /** Present ONLY while setup is required — the backend stops volunteering it afterwards. */
  options?: SetupOptions;
}

export interface SetupInput {
  locale: string;
  countryCode: string;
  companyCode: string;
  companyName: string;
  legalName: string;
  taxNumber: string;
  functionalCurrency: string;
  pricingCurrency: string;
  businessProfile: string;
  fiscalYearStartMonth: number;
  fiscalYearStartYear: number;
  branchCode: string;
  branchName: string;
  warehouseCode: string;
  warehouseName: string;
  adminUsername: string;
  adminDisplayName: string;
  adminPassword: string;
  /** What prints at the top of a receipt. Blank means the shop's name. */
  receiptHeader: string;
}

export interface SetupResult {
  companyId: string;
  branchId: string;
  warehouseId: string;
  adminUserId: string;
}

export function setupStatus(): Promise<SetupStatus> {
  return call<SetupStatus>("Setup", "Status");
}

export function applySetup(input: SetupInput): Promise<SetupResult> {
  return call<SetupResult>("Setup", "Apply", input);
}

// ── Identity (administration) ───────────────────────────────────────────────────

export interface User {
  id: string;
  username: string;
  displayName: string;
  email: string;
  isActive: boolean;
  /** The setup administrator: protected from deletion, not from deactivation. */
  isSystem: boolean;
}

export interface Role {
  id: string;
  code: string;
  name: string;
  description: string;
  isSystem: boolean;
  isActive: boolean;
}

export interface PermissionEntry {
  code: string;
  module: string;
  /** Declared by an older build and no longer by this one. Shown, not hidden. */
  obsolete: boolean;
}

export interface AdminSession {
  id: string;
  userId: string;
  username: string;
  displayName: string;
  startedAt: string;
  lastSeen: string;
  expiresAt: string;
  deviceInfo: string;
  /** The session making the call. Ending it signs you out. */
  current: boolean;
}

export interface NewUser {
  username: string;
  displayName: string;
  email: string;
  password: string;
  roleCode: string;
}

export function users(): Promise<User[]> {
  return call<User[]>("Identity", "Users");
}

export function createUser(input: NewUser): Promise<User> {
  return call<User>("Identity", "CreateUser", input);
}

export function setUserActive(userId: string, active: boolean): Promise<boolean> {
  return call<boolean>("Identity", "SetActive", userId, active);
}

export function resetPasswordFor(userId: string, password: string): Promise<boolean> {
  return call<boolean>("Identity", "ResetPasswordFor", userId, password);
}

/** Changes the CALLER'S OWN password. No user id: the backend reads it from the session. */
export function changeMyPassword(current: string, next: string): Promise<boolean> {
  return call<boolean>("Identity", "ChangeMyPassword", current, next);
}

export function roles(): Promise<Role[]> {
  return call<Role[]>("Identity", "Roles");
}

export function userRoles(userId: string): Promise<Role[]> {
  return call<Role[]>("Identity", "UserRoles", userId);
}

export function permissionCatalogue(): Promise<PermissionEntry[]> {
  return call<PermissionEntry[]>("Identity", "Permissions");
}

export function roleGrants(roleId: string): Promise<string[]> {
  return call<string[]>("Identity", "RoleGrants", roleId);
}

export function grantToRole(roleId: string, permission: string): Promise<boolean> {
  return call<boolean>("Identity", "GrantToRole", roleId, permission);
}

export function revokeFromRole(roleId: string, permission: string): Promise<boolean> {
  return call<boolean>("Identity", "RevokeFromRole", roleId, permission);
}

export function assignRole(userId: string, roleId: string): Promise<boolean> {
  return call<boolean>("Identity", "AssignRole", userId, roleId);
}

export function unassignRole(userId: string, roleId: string): Promise<boolean> {
  return call<boolean>("Identity", "UnassignRole", userId, roleId);
}

export function adminSessions(): Promise<AdminSession[]> {
  return call<AdminSession[]>("Identity", "Sessions");
}

export function revokeSession(sessionId: string): Promise<boolean> {
  return call<boolean>("Identity", "RevokeSession", sessionId);
}

/** The permission codes the administration screens gate on. */
export const PERMISSIONS = {
  userView: "identity.user.view",
  userManage: "identity.user.manage",
  roleView: "identity.role.view",
  roleManage: "identity.role.manage",
  sessionView: "identity.session.view",
  sessionRevoke: "identity.session.revoke",
  auditView: "audit.entry.view",
  auditPayload: "audit.entry.view_payload",
  accountView: "accounting.account.view",
  accountManage: "accounting.account.manage",
  catalogView: "catalog.view",
  catalogManage: "catalog.manage",
  customerView: "partner.customer.view",
  customerManage: "partner.customer.manage",
  supplierView: "partner.supplier.view",
  supplierManage: "partner.supplier.manage",
  priceView: "pricing.view",
  stockView: "inventory.stock.view",
  stockAdjust: "inventory.stock.adjust",
  costView: "inventory.cost.view",
  saleView: "sales.document.view",
  saleDraft: "sales.document.draft",
  salePost: "sales.document.post",
  saleCancel: "sales.document.cancel",
  shiftOpen: "pos.shift.open",
  shiftClose: "pos.shift.close",
  salePrint: "sales.document.print",
  orderView: "purchasing.order.view",
  orderDraft: "purchasing.order.draft",
  orderPlace: "purchasing.order.place",
  receiptRecord: "purchasing.receipt.record",
  billView: "purchasing.bill.view",
  billDraft: "purchasing.bill.draft",
  billPost: "purchasing.bill.post",
  supplierPay: "purchasing.payment.post",
  expenseView: "expenses.expense.view",
  expenseDraft: "expenses.expense.draft",
  expensePost: "expenses.expense.post",
  expenseSettle: "expenses.settlement.post",
  debtView: "expenses.debt.view",
  debtRecord: "expenses.debt.record",
  profitAndLossView: "accounting.profit_and_loss.view",
  balanceSheetView: "accounting.balance_sheet.view",
  salesAnalysisView: "sales.analysis.view",
  purchaseAnalysisView: "purchasing.analysis.view",
  valuationView: "inventory.valuation.view",
  partnerBalanceView: "partner.balance.view",
} as const;

// ── Accounting (read-only, §20.6 tier v1.1) ─────────────────────────────────────
//
// The chart and the trial balance are readable two releases before anything may be edited, so
// this surface deliberately offers no way to post, edit, or close.

export interface Account {
  id: string;
  code: string;
  name: string;
  nameKey: string;
  type: string;
  normal: string;
  depth: number;
  isPostable: boolean;
  isSystem: boolean;
  isActive: boolean;
}

export interface TrialBalanceRow {
  accountId: string;
  code: string;
  name: string;
  type: string;
  normal: string;
  /**
   * Minor units as STRINGS.
   *
   * JavaScript numbers are float64 and lose integer precision above 2^53. Money crosses as
   * text and is formatted, never arithmetic'd, on this side — the totals that matter are
   * computed in Go, where they are exact.
   */
  openingMinor: string;
  debitMinor: string;
  creditMinor: string;
  closingMinor: string;
}

export interface TrialBalance {
  periodId: string;
  rows: TrialBalanceRow[];
  totalDebitMinor: string;
  totalCreditMinor: string;
  /** The assertion made visible: a correct trial balance sums to zero (2.3). */
  balanced: boolean;
}

export interface FiscalPeriod {
  id: string;
  sequence: number;
  start: string;
  end: string;
  status: string;
}

export function chartOfAccounts(): Promise<Account[]> {
  return call<Account[]>("Accounting", "Chart");
}

export function fiscalPeriods(): Promise<FiscalPeriod[]> {
  return call<FiscalPeriod[]>("Accounting", "Periods");
}

export function trialBalance(periodId: string): Promise<TrialBalance> {
  return call<TrialBalance>("Accounting", "TrialBalance", periodId);
}

// ── System ──────────────────────────────────────────────────────────────────────

export interface HealthInfo {
  version: string;
  commit: string;
  buildTime: string;
  goVersion: string;
  platform: string;
}

export function health(): Promise<HealthInfo> {
  return call<HealthInfo>("System", "Health");
}

// ── Config ──────────────────────────────────────────────────────────────────────

export type ThemePreference = "light" | "dark" | "system";

export interface Preferences {
  locale: string;
  theme: ThemePreference;
  availableLocales: string[];
  /** The route this workspace opens on, chosen by the business profile at setup. */
  landing: string;
}

export function preferences(): Promise<Preferences> {
  return call<Preferences>("Config", "Preferences");
}

export function setLocale(locale: string): Promise<Preferences> {
  return call<Preferences>("Config", "SetLocale", locale);
}

export function setTheme(theme: ThemePreference): Promise<Preferences> {
  return call<Preferences>("Config", "SetTheme", theme);
}

// ── Ops ─────────────────────────────────────────────────────────────────────────

export interface JobState {
  key: string;
  enabled: boolean;
  schedule: string;
  lastRunAt: string;
  nextRunAt: string;
  lastStatus: string;
}

export interface RunRecord {
  runId: string;
  jobKey: string;
  startedAt: string;
  finishedAt: string;
  status: string;
  attempt: number;
  error: string;
  output: string;
  triggeredBy: string;
}

export function jobs(): Promise<JobState[]> {
  return call<JobState[]>("Ops", "Jobs");
}

export function runs(jobKey: string, limit: number): Promise<RunRecord[]> {
  return call<RunRecord[]>("Ops", "Runs", jobKey, limit);
}

// ── Audit ───────────────────────────────────────────────────────────────────────

export interface AuditEntry {
  id: string;
  occurredAt: string;
  actorUserId: string;
  /** A snapshot taken when the entry was written, so old records stay readable. */
  actorName: string;
  correlationId: string;
  action: string;
  entityType: string;
  entityId: string;
  entityLabel: string;
  source: string;

  /**
   * The before/after payload, gated by `audit.entry.view_payload`.
   *
   * OPTIONAL, and that is the contract: the key is ABSENT — not blank — when the signed-in user
   * may not see it. `undefined` therefore means "you may not see this", which is a different
   * fact from an entry that genuinely had no before-state, and the UI must render them
   * differently.
   */
  beforeJson?: string;
  afterJson?: string;
  changedFields?: string;
}

export interface AuditFilter {
  entityType?: string;
  entityId?: string;
  actorId?: string;
  limit?: number;
}

export function auditEntries(filter: AuditFilter = {}): Promise<AuditEntry[]> {
  return call<AuditEntry[]>("Audit", "Entries", {
    entityType: filter.entityType ?? "",
    entityId: filter.entityId ?? "",
    actorId: filter.actorId ?? "",
    limit: filter.limit ?? 0,
  });
}

/**
 * Whether the payload was withheld rather than empty.
 *
 * A helper rather than an inline `=== undefined` at each call site, so every screen renders the
 * distinction the same way — and so the reasoning lives in one place.
 */
export function payloadHidden(entry: AuditEntry): boolean {
  return entry.beforeJson === undefined && entry.afterJson === undefined;
}

// ── Money ───────────────────────────────────────────────────────────────────────

export interface Currency {
  code: string;
  name: string;
  symbol: string;
  decimalPlaces: number;
  symbolPosition: string;
}

export function currencies(): Promise<Currency[]> {
  return call<Currency[]>("Money", "Currencies");
}

// ── Catalog (read-only) ─────────────────────────────────────────────────────────
//
// Phase 3 builds the master data and the screens that READ it. There is no create form here
// deliberately: shipping one would put a control behind a workflow nobody has designed yet.

export interface Category {
  id: string;
  code: string;
  name: string;
  nameKey: string;
  path: string;
  depth: number;
  isActive: boolean;
}

export interface ProductRow {
  id: string;
  code: string;
  name: string;
  nameKey: string;
  /** Free text the shop writes for itself; seeds the edit form. */
  description: string;
  categoryCode: string;
  type: string;
  stockUnit: string;
  tracking: string;
  variantCount: number;
  isActive: boolean;
}

export interface Variant {
  id: string;
  sku: string;
  name: string;
  combination: string;
  isDefault: boolean;
  hasHistory: boolean;
  isActive: boolean;
}

export interface AttributeValue {
  code: string;
  name: string;
  nameKey: string;
  displayHint: string;
}

export interface ProductAttribute {
  code: string;
  name: string;
  nameKey: string;
  isVariantDefining: boolean;
  values: AttributeValue[];
}

export interface ProductDetail {
  product: ProductRow;
  salesUnit: string;
  purchaseUnit: string;
  stockUnitLocked: boolean;
  variants: Variant[];
  attributes: ProductAttribute[];
  /**
   * One variant, and it is the default (§A.1). The detail screen hides the variants section
   * entirely when this is true, so a bag of cement never shows the word "variant".
   */
  isSimple: boolean;
}

export function productCategories(): Promise<Category[]> {
  return call<Category[]>("Catalog", "Categories");
}

export function products(): Promise<ProductRow[]> {
  return call<ProductRow[]>("Catalog", "Products");
}

export function product(code: string): Promise<ProductDetail> {
  return call<ProductDetail>("Catalog", "Product", code);
}

/** What a barcode resolves to (§A.5): one variant, and how many the scan is worth. */
export interface Scan {
  variantId: string;
  productCode: string;
  productName: string;
  sku: string;
  /** A case barcode is worth its whole case, so one scan can be twelve units. */
  quantityMicro: string;
  packagingCode: string;
}

export function scanBarcode(code: string): Promise<Scan> {
  return call<Scan>("Catalog", "Scan", code);
}

// ── Partners (read-only) ────────────────────────────────────────────────────────

export interface PartnerRow {
  id: string;
  code: string;
  name: string;
  legalName: string;
  partnerType: string;
  isCustomer: boolean;
  isSupplier: boolean;
  taxNumber: string;
  isTaxExempt: boolean;
  currency: string;
  paymentTermsDays: number;
  /** Minor units as a string (§17). "0" means NO LIMIT, not "no credit". */
  creditLimitMinor: string;
  phone: string;
  email: string;
  isActive: boolean;
  hasHistory: boolean;
}

export interface PartnerAddress {
  id: string;
  label: string;
  addressType: string;
  line1: string;
  line2: string;
  city: string;
  region: string;
  postalCode: string;
  countryCode: string;
  isDefault: boolean;
}

export interface PartnerContact {
  id: string;
  name: string;
  role: string;
  phone: string;
  email: string;
  isPrimary: boolean;
}

export interface PartnerDetail {
  partner: PartnerRow;
  addresses: PartnerAddress[];
  contacts: PartnerContact[];
  customerRoleLocked: boolean;
  supplierRoleLocked: boolean;
}

export function customers(search: string): Promise<PartnerRow[]> {
  return call<PartnerRow[]>("Partners", "Customers", search);
}

export function suppliers(search: string): Promise<PartnerRow[]> {
  return call<PartnerRow[]>("Partners", "Suppliers", search);
}

export function customer(code: string): Promise<PartnerDetail> {
  return call<PartnerDetail>("Partners", "Customer", code);
}

export function supplier(code: string): Promise<PartnerDetail> {
  return call<PartnerDetail>("Partners", "Supplier", code);
}

// ── Inventory ───────────────────────────────────────────────────────────────────
//
// Quantities cross as STRINGS of micro units, like money (§17): integers scaled by 10⁶, which
// JavaScript's number type cannot be trusted with once a wholesaler's figures are involved.
// Nothing here does arithmetic with them.
//
// Cost fields are OPTIONAL — absent, not zero — for callers without `inventory.cost.view`. A zero
// on a screen reads as free stock; an absent field reads as "you may not see this".

export interface StockRow {
  variantId: string;
  productCode: string;
  productName: string;
  sku: string;
  unit: string;
  onHandMicro: string;
  reservedMicro: string;
  availableMicro: string;
  averageCostMicro?: string;
  valueMinor?: string;
}

export interface MovementRow {
  id: string;
  movementType: string;
  /** Which way the row points, decided by the backend from the movement TYPE. */
  isInward: boolean;
  quantityMicro: string;
  unitCostMicro?: string;
  valueMinor?: string;
  balanceAfterMicro: string;
  documentType: string;
  reason: string;
  occurredAt: string;
}

export interface Discrepancy {
  variantId: string;
  sku: string;
  projectedOnHandMicro: string;
  ledgerOnHandMicro: string;
  /** Turns "the total is wrong" into "it went wrong here". */
  firstSuspectMovementId: string;
}

export interface LedgerCheck {
  checked: number;
  discrepancies: Discrepancy[];
  healthy: boolean;
}

export interface AdjustInput {
  warehouseId: string;
  productId: string;
  variantId: string;
  quantityMicro: string;
  unitCostMicro: string;
  /** Which way. A signed quantity would put the direction in the number. */
  increase: boolean;
  reason: string;
}

export function stockOnHand(warehouseId: string): Promise<StockRow[]> {
  return call<StockRow[]>("Inventory", "Stock", warehouseId);
}

export function stockMovements(variantId: string, warehouseId: string): Promise<MovementRow[]> {
  return call<MovementRow[]>("Inventory", "Movements", variantId, warehouseId);
}

export function checkLedger(): Promise<LedgerCheck> {
  return call<LedgerCheck>("Inventory", "CheckLedger");
}

export function adjustStock(input: AdjustInput): Promise<MovementRow> {
  return call<MovementRow>("Inventory", "Adjust", input);
}

// ── Sales and point of sale ─────────────────────────────────────────────────────
//
// Every amount is a STRING of minor units and every quantity a string of micro units, for the
// reason money.ts gives: float64 loses integer precision above 2^53, and a till in a
// hyperinflated currency reaches that in ordinary trading (§17, §18).
//
// Nothing here adds two of them together. Totals are computed in Go, where they are exact
// integers, and re-read after every change.

export interface SalesDocument {
  id: string;
  documentType: string;
  status: string;
  number: string;
  partnerName: string;
  date: string;
  currency: string;
  netMinor: string;
  taxMinor: string;
  discountMinor: string;
  totalMinor: string;
  /** What is still owed. Empty on anything not posted — a draft has not been agreed. */
  outstandingMinor: string;
  isHeld: boolean;
  holdLabel: string;
}

export interface SalesLine {
  id: string;
  lineNumber: number;
  variantId: string;
  /** What the product was CALLED when the line was added, not what it is called now (§9.3). */
  productName: string;
  sku: string;
  uomCode: string;
  quantityMicro: string;
  unitPriceMinor: string;
  discountMinor: string;
  taxAmountMinor: string;
  netMinor: string;
  totalMinor: string;
  /** Which price list answered. A salesperson who cannot explain a price overrides it by hand. */
  priceListCode: string;
}

export interface SalesDetail {
  document: SalesDocument;
  lines: SalesLine[];
  /** A posted document is a record, not a form. */
  editable: boolean;
}

export interface Shift {
  id: string;
  terminal: string;
  status: string;
  openedAt: string;
  openingFloatMinor: string;
  closedAt: string;
  /** The three reconciliation figures, present only once a shift is closed. */
  expectedMinor?: string;
  countedMinor?: string;
  differenceMinor?: string;
}

export interface NewSale {
  warehouseId: string;
  partnerId: string;
  partnerName: string;
  date: string;
  currency: string;
}

/** No price field: the caller says WHAT and HOW MANY, and the price lists answer the rest. */
export interface NewSaleLine {
  documentId: string;
  variantId: string;
  uomId: string;
  quantityMicro: string;
}

export interface NewPayment {
  documentId: string;
  shiftId: string;
  method: string;
  amountMinor: string;
  reference: string;
  date: string;
  currency: string;
}

export function salesDocuments(documentType = "", status = ""): Promise<SalesDocument[]> {
  return call<SalesDocument[]>("Sales", "Documents", documentType, status);
}

export function salesDocument(documentId: string): Promise<SalesDetail> {
  return call<SalesDetail>("Sales", "Document", documentId);
}

export function draftSale(input: NewSale): Promise<SalesDocument> {
  return call<SalesDocument>("Sales", "Draft", input);
}

export function addSaleLine(input: NewSaleLine): Promise<SalesDetail> {
  return call<SalesDetail>("Sales", "AddLine", input);
}

export function removeSaleLine(documentId: string, lineId: string): Promise<SalesDetail> {
  return call<SalesDetail>("Sales", "RemoveLine", documentId, lineId);
}

export function postSale(documentId: string): Promise<SalesDocument> {
  return call<SalesDocument>("Sales", "Post", documentId);
}

export function cancelSale(documentId: string): Promise<boolean> {
  return call<boolean>("Sales", "Cancel", documentId);
}

export function holdSale(documentId: string, label: string): Promise<boolean> {
  return call<boolean>("Sales", "Hold", documentId, label);
}

export function resumeSale(documentId: string): Promise<SalesDetail> {
  return call<SalesDetail>("Sales", "Resume", documentId);
}

export function heldSales(): Promise<SalesDocument[]> {
  return call<SalesDocument[]>("Sales", "HeldSales");
}

export function takePayment(input: NewPayment): Promise<SalesDocument> {
  return call<SalesDocument>("Sales", "TakePayment", input);
}

export function openShift(terminal: string, floatMinor: string): Promise<Shift> {
  return call<Shift>("Sales", "OpenShift", terminal, floatMinor);
}

export function closeShift(
  shiftId: string,
  countedMinor: string,
  notes: string,
): Promise<Shift> {
  return call<Shift>("Sales", "CloseShift", shiftId, countedMinor, notes);
}

export function currentShift(terminal: string): Promise<Shift> {
  return call<Shift>("Sales", "CurrentShift", terminal);
}

/** A rendered document: a complete, self-contained page. */
export interface PrintedDocument {
  html: string;
  number: string;
}

/**
 * Renders a sales document for printing.
 *
 * The BROWSER prints it, which is why HTML comes back rather than the printer being driven from
 * Go: bidirectional text and Arabic letter shaping are decades of work sitting inside the webview
 * and inside nothing a Go program can reach offline.
 */
export function printSalesDocument(
  documentId: string,
  template: string,
  paper: string,
): Promise<PrintedDocument> {
  return call<PrintedDocument>("Sales", "Print", documentId, template, paper);
}

// ── Purchasing ──────────────────────────────────────────────────────────────────
//
// The other half of the stock cycle. Money as strings of minor units and quantities as strings
// of micro units, as everywhere — a purchase order for three thousand metres of cable is exactly
// the document where float64 stops being able to hold the answer (§17, §18).

export interface PurchaseOrder {
  id: string;
  status: string;
  number: string;
  supplierName: string;
  orderDate: string;
  expectedDate: string;
  currency: string;
  netMinor: string;
  taxMinor: string;
  totalMinor: string;
  supplierReference: string;
  /** Whether everything ordered has arrived. Computed from the lines, never stored. */
  fullyReceived: boolean;
}

export interface PurchaseOrderLine {
  id: string;
  lineNumber: number;
  variantId: string;
  /** What the product was CALLED when the order was placed, not what it is called now (§9.3). */
  productName: string;
  sku: string;
  uomCode: string;
  quantityMicro: string;
  receivedMicro: string;
  outstandingMicro: string;
  unitPriceMicro: string;
  taxAmountMinor: string;
  netMinor: string;
  totalMinor: string;
  supplierCode: string;
}

export interface PurchaseOrderDetail {
  order: PurchaseOrder;
  lines: PurchaseOrderLine[];
  /** A placed order has been SENT and a supplier is picking from it. */
  editable: boolean;
  receivable: boolean;
}

export interface GoodsReceipt {
  id: string;
  status: string;
  number: string;
  orderId: string;
  supplierName: string;
  receiptDate: string;
  deliveryNoteReference: string;
  receivedByName: string;
  valueMinor: string;
  /** Whether an invoice has taken this delivery up. The GRNI report's central column. */
  billed: boolean;
}

export interface PurchaseBill {
  id: string;
  status: string;
  number: string;
  supplierName: string;
  supplierInvoiceNumber: string;
  billDate: string;
  dueDate: string;
  currency: string;
  netMinor: string;
  taxMinor: string;
  totalMinor: string;
  /** What the supplier charged over what was ordered. Shown because a variance is ACCEPTED. */
  varianceMinor: string;
  outstandingMinor: string;
}

export interface SupplierPayment {
  id: string;
  number: string;
  supplierName: string;
  paymentDate: string;
  method: string;
  reference: string;
  amountMinor: string;
  status: string;
}

export interface NewPurchaseOrder {
  warehouseId: string;
  supplierId: string;
  supplierName: string;
  orderDate: string;
  expectedDate: string;
  currency: string;
  supplierReference: string;
  notes: string;
}

/** No price field: the purchase price lists answer at placement. */
export interface NewPurchaseOrderLine {
  orderId: string;
  variantId: string;
  uomId: string;
  quantityMicro: string;
  supplierCode: string;
}

export function purchaseOrders(status = ""): Promise<PurchaseOrder[]> {
  return call<PurchaseOrder[]>("Purchasing", "Orders", status);
}

export function purchaseOrder(orderId: string): Promise<PurchaseOrderDetail> {
  return call<PurchaseOrderDetail>("Purchasing", "Order", orderId);
}

export function draftPurchaseOrder(input: NewPurchaseOrder): Promise<PurchaseOrder> {
  return call<PurchaseOrder>("Purchasing", "DraftOrder", input);
}

export function addPurchaseOrderLine(
  input: NewPurchaseOrderLine,
): Promise<PurchaseOrderDetail> {
  return call<PurchaseOrderDetail>("Purchasing", "AddOrderLine", input);
}

export function removePurchaseOrderLine(
  orderId: string,
  lineId: string,
): Promise<PurchaseOrderDetail> {
  return call<PurchaseOrderDetail>("Purchasing", "RemoveOrderLine", orderId, lineId);
}

export function placePurchaseOrder(orderId: string): Promise<PurchaseOrder> {
  return call<PurchaseOrder>("Purchasing", "PlaceOrder", orderId);
}

export function cancelPurchaseOrder(orderId: string): Promise<boolean> {
  return call<boolean>("Purchasing", "CancelOrder", orderId);
}

export function goodsReceipts(orderId = "", status = ""): Promise<GoodsReceipt[]> {
  return call<GoodsReceipt[]>("Purchasing", "Receipts", orderId, status);
}

export function confirmGoodsReceipt(receiptId: string): Promise<GoodsReceipt> {
  return call<GoodsReceipt>("Purchasing", "ConfirmReceipt", receiptId);
}

export function purchaseBills(status = ""): Promise<PurchaseBill[]> {
  return call<PurchaseBill[]>("Purchasing", "Bills", status);
}

export function postPurchaseBill(billId: string): Promise<PurchaseBill> {
  return call<PurchaseBill>("Purchasing", "PostBill", billId);
}

export function supplierPayments(): Promise<SupplierPayment[]> {
  return call<SupplierPayment[]>("Purchasing", "Payments");
}

// ── Money out ───────────────────────────────────────────────────────────────────

export interface ExpenseCategory {
  id: string;
  code: string;
  name: string;
  /** Set for a SHIPPED category, so the screen renders it through the catalog. */
  nameKey: string;
  /** Whether the tax on this can be reclaimed. The one thing an operator must be told. */
  taxRecoverable: boolean;
}

export interface Expense {
  id: string;
  status: string;
  number: string;
  payeeName: string;
  expenseDate: string;
  reference: string;
  description: string;
  /** "immediate" or "on_account". */
  settlement: string;
  paidMethod: string;
  dueDate: string;
  currency: string;
  netMinor: string;
  taxMinor: string;
  totalMinor: string;
  /** Blank on an expense paid when recorded — absent, not zero. */
  outstandingMinor: string;
  isTemplate: boolean;
}

export interface ExpenseLine {
  id: string;
  lineNumber: number;
  categoryId: string;
  categoryName: string;
  description: string;
  netMinor: string;
  taxAmountMinor: string;
  taxRecoverable: boolean;
  totalMinor: string;
}

export interface ExpenseDetail {
  expense: Expense;
  lines: ExpenseLine[];
  editable: boolean;
}

export interface Debt {
  id: string;
  status: string;
  number: string;
  direction: string;
  kind: string;
  counterpartyName: string;
  debtDate: string;
  dueDate: string;
  method: string;
  reference: string;
  currency: string;
  amountMinor: string;
}

export interface DebtPosition {
  kind: string;
  /** SIGNED: positive means money came in on balance. The one place a sign belongs. */
  netMinor: string;
}

export interface NewExpense {
  partnerId: string;
  payeeName: string;
  expenseDate: string;
  reference: string;
  description: string;
  settlement: string;
  paidMethod: string;
  dueDate: string;
  currency: string;
}

export interface NewExpenseLine {
  expenseId: string;
  categoryId: string;
  description: string;
  netMinor: string;
}

export interface NewSettlement {
  partnerId: string;
  payeeName: string;
  paymentDate: string;
  method: string;
  reference: string;
  currency: string;
  amountMinor: string;
  expenseId: string;
}

export interface NewDebt {
  direction: string;
  kind: string;
  partnerId: string;
  counterpartyName: string;
  debtDate: string;
  dueDate: string;
  method: string;
  reference: string;
  description: string;
  currency: string;
  amountMinor: string;
}

export function expenseCategories(): Promise<ExpenseCategory[]> {
  return call<ExpenseCategory[]>("Expenses", "Categories");
}

export function expenses(status = ""): Promise<Expense[]> {
  return call<Expense[]>("Expenses", "Expenses", status);
}

export function expense(expenseId: string): Promise<ExpenseDetail> {
  return call<ExpenseDetail>("Expenses", "Expense", expenseId);
}

export function unsettledExpenses(): Promise<Expense[]> {
  return call<Expense[]>("Expenses", "Unsettled");
}

export function draftExpense(input: NewExpense): Promise<Expense> {
  return call<Expense>("Expenses", "Draft", input);
}

export function addExpenseLine(input: NewExpenseLine): Promise<ExpenseDetail> {
  return call<ExpenseDetail>("Expenses", "AddLine", input);
}

export function recordExpense(expenseId: string): Promise<Expense> {
  return call<Expense>("Expenses", "Record", expenseId);
}

export function cancelExpense(expenseId: string): Promise<boolean> {
  return call<boolean>("Expenses", "Cancel", expenseId);
}

export function settleExpense(input: NewSettlement): Promise<boolean> {
  return call<boolean>("Expenses", "Settle", input);
}

export function debts(kind = ""): Promise<Debt[]> {
  return call<Debt[]>("Expenses", "Debts", kind);
}

export function debtPositions(): Promise<DebtPosition[]> {
  return call<DebtPosition[]>("Expenses", "DebtPositions");
}

export function recordDebt(input: NewDebt): Promise<Debt> {
  return call<Debt>("Expenses", "RecordDebt", input);
}

// ── Partner balances ────────────────────────────────────────────────────────────

export interface PartnerBalance {
  partnerId: string;
  partnerName: string;
  receivableMinor: string;
  payableMinor: string;
  /** SIGNED. Both halves come alongside, because netting hides the both-roles case. */
  netMinor: string;
}

export interface OpenItem {
  documentType: string;
  documentId: string;
  documentNumber: string;
  date: string;
  dueDate: string;
  totalMinor: string;
  settledMinor: string;
  outstandingMinor: string;
}

export interface AgeBands {
  currentMinor: string;
  days30Minor: string;
  days60Minor: string;
  days90Minor: string;
  olderMinor: string;
}

export interface PartnerStatement {
  balance: PartnerBalance;
  receivable: OpenItem[];
  payable: OpenItem[];
  receivableAgeing: AgeBands;
}

export interface BalanceDiscrepancy {
  partnerId: string;
  partnerName: string;
  side: string;
  documentsMinor: string;
  ledgerMinor: string;
  differenceMinor: string;
}

/** `asAt` decides the ageing date. Empty means today. */
export function partnerStatement(partnerId: string, asAt = ""): Promise<PartnerStatement> {
  return call<PartnerStatement>("Partners", "Statement", partnerId, asAt);
}

export function verifyPartnerBalances(): Promise<BalanceDiscrepancy[]> {
  return call<BalanceDiscrepancy[]>("Partners", "VerifyBalances");
}

// ── Insight: statements, analyses, valuation, search, dashboard (Phase 8) ───────

export interface StatementNode {
  accountId: string;
  code: string;
  name: string;
  type: string;
  depth: number;
  isPostable: boolean;
  amountMinor: string;
  children: StatementNode[];
}

export interface ProfitAndLoss {
  requestedFrom: string;
  requestedTo: string;
  /**
   * The whole fiscal periods the figures actually come from.
   *
   * Differs from the requested range when a request lands mid-period. A screen showing one
   * without the other reports a month's figures under a fortnight's heading.
   */
  coveredFrom: string;
  coveredTo: string;
  coveredPeriods: number;

  revenue: StatementNode[];
  expense: StatementNode[];

  revenueMinor: string;
  expenseMinor: string;
  /** The LEDGER's answer, including costs no sale knows about. Never labelled "profit" alone. */
  netProfitMinor: string;
}

export interface BalanceSheet {
  asAt: string;
  asset: StatementNode[];
  liability: StatementNode[];
  equity: StatementNode[];

  assetMinor: string;
  liabilityMinor: string;
  equityMinor: string;
  /** The profit not yet closed into retained earnings, already included in equityMinor. */
  resultMinor: string;

  outOfBalanceMinor: string;
  balanced: boolean;
}

export interface AnalysisRow {
  key: string;
  label: string;
  id: string;
  documents: number;

  quantityMicro: string;
  revenueMinor: string;
  /** Empty on a spend analysis: a purchase has no margin, because the cost is the purchase. */
  costMinor: string;
  grossMarginMinor: string;
  marginPercentMicro: string;
}

export interface Analysis {
  from: string;
  to: string;
  rows: AnalysisRow[];
  total: AnalysisRow;
}

export interface ValuedLine {
  warehouseId: string;
  warehouseName: string;
  productId: string;
  variantId: string;
  variantSku: string;
  productName: string;
  quantityMicro: string;
  avgCostMicro: string;
  valueMinor: string;
}

export interface Valuation {
  lines: ValuedLine[];
  totalMinor: string;
  /** Distinguishes "the books agree" from "nobody asked the books". */
  hasLedger: boolean;
  ledgerMinor: string;
  differenceMinor: string;
  reconciled: boolean;
}

export interface SearchResult {
  kind: string;
  id: string;
  label: string;
  subtitle: string;
}

export interface SearchResponse {
  query: string;
  results: SearchResult[];
  /** Modules that could not answer. "Nothing found" and "search is broken" differ. */
  failed: string[];
}

export interface Tile {
  key: string;
  source: string;
  kind: string;
  amountMinor: string;
  count: number;
  periodic: boolean;
  failed: boolean;
}

export interface DashboardBoard {
  from: string;
  to: string;
  tiles: Tile[];
}

export function profitAndLoss(from: string, to: string): Promise<ProfitAndLoss> {
  return call<ProfitAndLoss>("Insight", "ProfitAndLoss", from, to);
}

export function balanceSheet(asAt: string): Promise<BalanceSheet> {
  return call<BalanceSheet>("Insight", "BalanceSheet", asAt);
}

export function salesByPeriod(from: string, to: string, grouping: string): Promise<Analysis> {
  return call<Analysis>("Insight", "SalesByPeriod", from, to, grouping);
}

export function salesByProduct(from: string, to: string): Promise<Analysis> {
  return call<Analysis>("Insight", "SalesByProduct", from, to);
}

export function salesByPartner(from: string, to: string): Promise<Analysis> {
  return call<Analysis>("Insight", "SalesByPartner", from, to);
}

export function spendByPeriod(from: string, to: string, grouping: string): Promise<Analysis> {
  return call<Analysis>("Insight", "SpendByPeriod", from, to, grouping);
}

export function spendByProduct(from: string, to: string): Promise<Analysis> {
  return call<Analysis>("Insight", "SpendByProduct", from, to);
}

export function spendBySupplier(from: string, to: string): Promise<Analysis> {
  return call<Analysis>("Insight", "SpendBySupplier", from, to);
}

export function stockValuation(): Promise<Valuation> {
  return call<Valuation>("Insight", "Valuation");
}

export function globalSearch(query: string, limit = 8): Promise<SearchResponse> {
  return call<SearchResponse>("Insight", "Search", query, limit);
}

/** An empty range asks the backend for month-to-date, from ITS clock rather than the browser's. */
export function dashboard(from = "", to = ""): Promise<DashboardBoard> {
  return call<DashboardBoard>("Insight", "Dashboard", from, to);
}

// ── Operations: backups, restore, import, notices (Phase 9) ─────────────────────

export interface BackupFile {
  /** The identifier passed back to restore. Never a path. */
  name: string;
  takenAt: string;
  reason: string;
  schemaVersion: number;
  sizeBytes: number;
  appVersion: string;
  /** Whether THIS build can read it. A newer backup is listed and marked, never hidden. */
  restorable: boolean;
}

export interface RestoreIntent {
  from: string;
  replacingVersion: number;
  restoringVersion: number;
  safetyBackup: string;
  preparedAt: string;
}

export interface ImportRow {
  /** The line in the user's spreadsheet, header counted. */
  line: number;
  key: string;
  ok: boolean;
  message: string;
  code: string;
}

export interface ImportReport {
  dryRun: boolean;
  kind: string;
  total: number;
  succeeded: number;
  failed: number;
  rows: ImportRow[];
}

export interface Notice {
  key: string;
  rule: string;
  severity: "danger" | "warning" | "info";
  messageKey: string;
  params: Record<string, string>;
  entityId: string;
  entityKind: string;
}

export interface NoticeCentre {
  notices: Notice[];
  /** How many are hidden, so a screen can offer to show them. */
  dismissed: number;
  /** Rules that could not run. Silence would otherwise read as "nothing is wrong". */
  failed: string[];
}

export function backups(): Promise<BackupFile[]> {
  return call<BackupFile[]>("Operations", "Backups");
}

export function takeBackup(): Promise<BackupFile> {
  return call<BackupFile>("Operations", "TakeBackup");
}

export function prepareRestore(name: string): Promise<RestoreIntent> {
  return call<RestoreIntent>("Operations", "PrepareRestore", name);
}

/** An empty `from` means nothing is waiting — the ordinary answer, not a failure. */
export function pendingRestore(): Promise<RestoreIntent> {
  return call<RestoreIntent>("Operations", "PendingRestore");
}

export function cancelRestore(): Promise<boolean> {
  return call<boolean>("Operations", "CancelRestore");
}

export function importProducts(contentBase64: string, dryRun: boolean): Promise<ImportReport> {
  return call<ImportReport>("Operations", "ImportProducts", contentBase64, dryRun);
}

export function importPartners(contentBase64: string, dryRun: boolean): Promise<ImportReport> {
  return call<ImportReport>("Operations", "ImportPartners", contentBase64, dryRun);
}

export function notices(): Promise<NoticeCentre> {
  return call<NoticeCentre>("Operations", "Notices");
}

export function dismissNotice(key: string): Promise<boolean> {
  return call<boolean>("Operations", "DismissNotice", key);
}

export function restoreNotice(key: string): Promise<boolean> {
  return call<boolean>("Operations", "RestoreNotice", key);
}

// ── Purchasing: returns and landed costs (Phase 10.1) ──────────────────────────
//
// Phase 6 built both documents and shipped neither a binding nor a screen, so a return a user
// could create in the domain was unreachable from the interface for four phases.

export interface ReturnLine {
  id: string;
  lineNumber: number;
  receiptLineId: string;
  productName: string;
  variantSku: string;
  uomCode: string;
  quantityMicro: string;
  unitPriceMicro: string;
  /** The ORIGINAL delivery's cost, not today's average — which is why a credit and a stock
   *  movement can differ on the same line. */
  unitCostMicro: string;
  netMinor: string;
  totalMinor: string;
}

export interface SupplierReturn {
  id: string;
  number: string;
  status: string;
  supplierId: string;
  supplierName: string;
  returnDate: string;
  reason: string;
  currency: string;
  netMinor: string;
  taxMinor: string;
  totalMinor: string;
  /** What the goods cost US, at the original delivery's figure. */
  costMinor: string;
  lines: ReturnLine[];
}

export interface NewSupplierReturn {
  supplierId: string;
  supplierName: string;
  warehouseId: string;
  returnDate: string;
  reason: string;
  supplierReference: string;
  currency: string;
}

export interface LandedCost {
  id: string;
  receiptId: string;
  chargeType: string;
  description: string;
  /** How the charge is spread: by value, quantity, or weight. It decides where the money lands. */
  basis: string;
  currency: string;
  amountMinor: string;
  status: string;
}

export interface NewLandedCost {
  receiptId: string;
  chargeType: string;
  description: string;
  partnerId: string;
  basis: string;
  currency: string;
  amountMinor: string;
}

export function supplierReturns(status = ""): Promise<SupplierReturn[]> {
  return call<SupplierReturn[]>("Purchasing", "Returns", status);
}

export function supplierReturn(returnId: string): Promise<SupplierReturn> {
  return call<SupplierReturn>("Purchasing", "Return", returnId);
}

export function draftSupplierReturn(input: NewSupplierReturn): Promise<SupplierReturn> {
  return call<SupplierReturn>("Purchasing", "DraftReturn", input);
}

export function addReturnLine(input: {
  returnId: string;
  receiptLineId: string;
  quantityMicro: string;
  notes: string;
}): Promise<boolean> {
  return call<boolean>("Purchasing", "AddReturnLine", input);
}

export function postSupplierReturn(returnId: string): Promise<SupplierReturn> {
  return call<SupplierReturn>("Purchasing", "PostReturn", returnId);
}

export function cancelSupplierReturn(returnId: string): Promise<boolean> {
  return call<boolean>("Purchasing", "CancelReturn", returnId);
}

export function landedCosts(receiptId: string): Promise<LandedCost[]> {
  return call<LandedCost[]>("Purchasing", "LandedCosts", receiptId);
}

export function addLandedCost(input: NewLandedCost): Promise<LandedCost> {
  return call<LandedCost>("Purchasing", "AddLandedCost", input);
}

/** Separate from adding it: recording a charge is bookkeeping, deciding it belongs in the cost of
 *  these goods is a judgement somebody makes. */
export function applyLandedCost(chargeId: string): Promise<boolean> {
  return call<boolean>("Purchasing", "ApplyLandedCost", chargeId);
}

// ── Catalogue and stock: the write actions (Phase 10.10) ───────────────────────
//
// The services have existed since Phases 3 and 4; the bindings and the forms did not, so
// registering a product was only possible by writing a CSV and importing it.

export interface NewProduct {
  code: string;
  name: string;
  /** A unit code — "PCS", "KG". Empty means the default. */
  unit: string;
  /** A category code. Empty is ordinary: filing can happen later. */
  category: string;
  /** "goods" or "service". Empty means goods. */
  type: string;
}

export interface Unit {
  code: string;
  name: string;
  nameKey: string;
  symbol: string;
  categoryId: string;
  allowsFractional: boolean;
}

/** The units a product can be stocked in. Bound in 10.10, alongside the form that needs them. */
export function units(): Promise<Unit[]> {
  return call<Unit[]>("Catalog", "Units");
}

export function createProduct(input: NewProduct): Promise<ProductRow> {
  return call<ProductRow>("Catalog", "CreateProduct", input);
}

/**
 * Records what is ACTUALLY on the shelf.
 *
 * The movement is still a delta — this converts, so nobody has to work out that eleven is two
 * fewer than thirteen while holding a clipboard.
 */
export function countStock(input: {
  variantId: string;
  warehouseId: string;
  countedMicro: string;
  reason: string;
}): Promise<MovementRow> {
  return call<MovementRow>("Inventory", "CountStock", input);
}

// ── The write path that was missing (10.13) ────────────────────────────────────
//
// `TestEveryWriteBindingHasAFrontEndCaller` found seven bindings that changed something and had
// no caller here, so deliveries, bills, supplier payments and expense lines could be read and not
// created. The partner create bindings did not exist at all — a binding that was never written
// has nothing for that test to find.

export interface NewPartner {
  code: string;
  name: string;
  isCustomer: boolean;
  isSupplier: boolean;
  phone: string;
  email: string;
  taxNumber: string;
  paymentTermsDays: number;
  /** Minor units, like every amount. "0" means no limit. */
  creditLimitMinor: string;
}

export function createCustomer(input: NewPartner): Promise<PartnerRow> {
  return call<PartnerRow>("Partners", "CreateCustomer", input);
}

export function createSupplier(input: NewPartner): Promise<PartnerRow> {
  return call<PartnerRow>("Partners", "CreateSupplier", input);
}

/** Removes a line from a DRAFT expense. Refused once it is recorded. */
export function removeExpenseLine(expenseId: string, lineId: string): Promise<boolean> {
  return call<boolean>("Expenses", "RemoveLine", expenseId, lineId);
}

export interface NewReceipt {
  orderId: string;
  warehouseId: string;
  receiptDate: string;
  deliveryNoteReference: string;
}

/** Starts a delivery against a placed order. Nothing moves until it is confirmed. */
export function draftGoodsReceipt(input: NewReceipt): Promise<GoodsReceipt> {
  return call<GoodsReceipt>("Purchasing", "DraftReceipt", input);
}

/** Records what actually arrived on one order line. Less than ordered is ordinary. */
export function receiveLine(input: {
  receiptId: string;
  orderLineId: string;
  quantityMicro: string;
  notes: string;
}): Promise<boolean> {
  return call<boolean>("Purchasing", "ReceiveLine", input);
}

export interface NewBill {
  supplierId: string;
  supplierName: string;
  supplierInvoiceNumber: string;
  billDate: string;
  dueDate: string;
  currency: string;
}

export function draftBill(input: NewBill): Promise<PurchaseBill> {
  return call<PurchaseBill>("Purchasing", "DraftBill", input);
}

/** Puts a confirmed delivery onto a bill. The accrual it raised clears when the bill posts. */
export function addBillLine(input: { billId: string; receiptLineId: string }): Promise<boolean> {
  return call<boolean>("Purchasing", "AddBillLine", input);
}

export function paySupplier(input: {
  supplierId: string;
  supplierName: string;
  billId: string;
  paymentDate: string;
  method: string;
  reference: string;
  currency: string;
  amountMinor: string;
}): Promise<boolean> {
  return call<boolean>("Purchasing", "Pay", input);
}

/** Closes an order that will receive nothing further, so it stops appearing as outstanding. */
export function closePurchaseOrder(orderId: string): Promise<boolean> {
  return call<boolean>("Purchasing", "CloseOrder", orderId);
}

/** What a person may change about a product. The code identifies it and is not editable. */
export interface EditProduct {
  code: string;
  name: string;
  description: string;
  /** A category code. Empty means filed nowhere. */
  category: string;
}

export function updateProduct(input: EditProduct): Promise<ProductRow> {
  return call<ProductRow>("Catalog", "UpdateProduct", input);
}

// ── exchange rates ──────────────────────────────────────────────────────────────

export interface Rate {
  from: string;
  to: string;
  /** Which kind of rate: market for trade, official for the state (§G.1). */
  rateType: string;
  /** The rate ×10⁹, as a decimal string. Never parsed into a number. */
  rateNano: string;
  /** The same figure, formatted for display. */
  rate: string;
  validFrom: string;
  source: string;
  recordedAt: string;
  /** Whether this is the row the engine would use today. */
  inForce: boolean;
}

export interface RateType {
  code: string;
  name: string;
}

export interface NewRate {
  from: string;
  to: string;
  rate: string;
  rateTypeCode: string;
  validFrom: string;
  source: string;
}

export function rateTypes(): Promise<RateType[]> {
  return call<RateType[]>("Money", "RateTypes");
}

export function rates(from: string, to: string, rateTypeCode = "", limit = 0): Promise<Rate[]> {
  return call<Rate[]>("Money", "Rates", from, to, rateTypeCode, limit);
}

/** Records a rate and returns the pair's whole history, so a screen redraws from one answer. */
export function setRate(input: NewRate): Promise<Rate[]> {
  return call<Rate[]>("Money", "SetRate", input);
}

// ── exporting ───────────────────────────────────────────────────────────────────

/** The formats an export can be asked for. */
export type ExportFormat = "csv" | "xlsx" | "docx" | "pdf";

export interface ExportedFile {
  filename: string;
  mimeType: string;
  /** The file itself. Base64 because the boundary is JSON. */
  contentBase64: string;
  /** How many data rows it holds, so a screen can say what it produced. */
  rows: number;
}

export function exportAnalysis(
  analysis: Analysis,
  title: string,
  withMargin: boolean,
  format: ExportFormat,
): Promise<ExportedFile> {
  return call<ExportedFile>("Insight", "ExportAnalysis", analysis, title, withMargin, format);
}

export function exportValuation(
  valuation: Valuation,
  format: ExportFormat,
): Promise<ExportedFile> {
  return call<ExportedFile>("Insight", "ExportValuation", valuation, format);
}

export function exportStatement(
  nodes: StatementNode[],
  title: string,
  from: string,
  to: string,
  format: ExportFormat,
): Promise<ExportedFile> {
  return call<ExportedFile>("Insight", "ExportStatement", nodes, title, from, to, format);
}
