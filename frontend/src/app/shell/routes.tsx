import type { ReactNode } from "react";
import { PERMISSIONS } from "@/lib/wails";
import { AuditScreen } from "@/modules/admin/AuditScreen";
import { RolesScreen } from "@/modules/admin/RolesScreen";
import { SessionsScreen } from "@/modules/admin/SessionsScreen";
import { UsersScreen } from "@/modules/admin/UsersScreen";
import { ChangePasswordScreen } from "@/modules/account/ChangePasswordScreen";
import { ChartScreen } from "@/modules/accounting/ChartScreen";
import { CatalogScreen } from "@/modules/catalog/CatalogScreen";
import { StockScreen } from "@/modules/inventory/StockScreen";
import { PartnersScreen } from "@/modules/partners/PartnersScreen";
import { POSTerminal } from "@/modules/sales/POSTerminal";
import { PurchaseOrdersScreen } from "@/modules/purchasing/PurchaseOrdersScreen";
import { BillsScreen } from "@/modules/purchasing/BillsScreen";
import { ExpensesScreen } from "@/modules/expenses/ExpensesScreen";
import { DebtsScreen } from "@/modules/expenses/DebtsScreen";
import { InvoicesScreen } from "@/modules/sales/InvoicesScreen";
import { TrialBalanceScreen } from "@/modules/accounting/TrialBalanceScreen";
import { SystemPanel } from "@/app/shell/SystemPanel";
import { DashboardScreen } from "@/modules/insight/DashboardScreen";
import { StatementsScreen } from "@/modules/insight/StatementsScreen";
import { AnalysisScreen } from "@/modules/insight/AnalysisScreen";
import { ValuationScreen } from "@/modules/insight/ValuationScreen";
import { NoticeCentre } from "@/modules/operations/NoticeCentre";
import { BackupScreen } from "@/modules/operations/BackupScreen";
import { ImportScreen } from "@/modules/operations/ImportScreen";
import { ReceiptsScreen } from "@/modules/purchasing/ReceiptsScreen";
import { ReceiveDeliveryScreen } from "@/modules/purchasing/ReceiveDeliveryScreen";
import { SupplierReturnsScreen } from "@/modules/purchasing/SupplierReturnsScreen";
import { SupplierPaymentsScreen } from "@/modules/purchasing/SupplierPaymentsScreen";
import { HelpScreen } from "@/modules/help/HelpScreen";
import { RatesScreen } from "@/modules/money/RatesScreen";

/**
 * One route.
 *
 * `permission` drives BOTH the navigation item and the route body's denied state, from one
 * declaration. Two lists would drift, and the symptom — a menu item that leads to a refusal —
 * is exactly the thing permission-aware navigation exists to prevent.
 */
/** The six areas the sidebar groups into. A flat list of thirty items is a list nobody scans. */
export type NavGroup =
  | "overview"
  | "sell"
  | "buy"
  | "stock"
  | "money"
  | "setup";

export const NAV_GROUPS: NavGroup[] = ["overview", "sell", "buy", "stock", "money", "setup"];

export interface AppRoute {
  path: string;
  /**
   * Whether this belongs in the collapsed "Advanced" section.
   *
   * # Why the flag marks the RARE case
   *
   * Thirty-one routes in one column is a list nobody scans, and grouping alone did not fix it —
   * six headings with thirty items under them is still thirty items. A shop uses four or five
   * screens all day and the rest occasionally, so the sidebar shows the daily ones and folds
   * everything else away.
   *
   * Omitting the flag puts a route in CORE, which is the loud failure rather than the quiet one:
   * `nav.test.tsx` caps the core list, so a route added without a decision fails the build
   * instead of quietly making the daily list longer.
   */
  advanced?: boolean;
  /** Which area of the sidebar this belongs under. */
  group: NavGroup;
  /** A translation key, so the menu is translated like everything else (§22). */
  labelKey: string;
  element: ReactNode;
  /** Empty means "everyone who is signed in". */
  permission?: string;
}

/**
 * The application's routes.
 *
 * The first real ones. Step 1.10 mounted the router with none, deliberately — inventing routes
 * before there were screens would have meant deleting them.
 *
 * Data rather than JSX: the shell renders the menu from this list and the router from the same
 * list, so a screen cannot be reachable without appearing in navigation or vice versa.
 */
export const ROUTES: AppRoute[] = [
  // The OVERVIEW is the landing screen, not the system panel.
  //
  // Until 10.9 the dashboard route rendered `SystemPanel` — health and job status — because it
  // was written in Phase 1, before any business figures existed. Phase 8 then built a dashboard
  // of revenue, margin and stock value and nothing routed to it, so the screen a shopkeeper
  // lands on showed them the outbox queue depth.
  { path: "/", group: "overview", labelKey: "nav.overview", element: <DashboardScreen /> },
  { path: "/needs-attention", group: "overview", labelKey: "nav.notices", element: <NoticeCentre /> },
  {
    path: "/admin/users", advanced: true,
    group: "setup",
    labelKey: "nav.users",
    element: <UsersScreen />,
    permission: PERMISSIONS.userView,
  },
  {
    path: "/admin/roles", advanced: true,
    group: "setup",
    labelKey: "nav.roles",
    element: <RolesScreen />,
    permission: PERMISSIONS.roleView,
  },
  {
    path: "/admin/sessions", advanced: true,
    group: "setup",
    labelKey: "nav.sessions",
    element: <SessionsScreen />,
    permission: PERMISSIONS.sessionView,
  },
  {
    path: "/admin/audit", advanced: true,
    group: "setup",
    labelKey: "nav.audit",
    element: <AuditScreen />,
    permission: PERMISSIONS.auditView,
  },
  {
    path: "/catalog",
    group: "stock",
    labelKey: "nav.catalog",
    element: <CatalogScreen />,
    permission: PERMISSIONS.catalogView,
  },
  {
    path: "/inventory/stock",
    group: "stock",
    labelKey: "nav.stock",
    element: <StockScreen />,
    permission: PERMISSIONS.stockView,
  },
  // The till is placed FIRST among the working screens, above the ledgers it feeds. For a shop
  // this is the screen that runs all day, and burying it under administration would put the
  // most-used thing in the application behind the least-used.
  {
    path: "/pos",
    group: "sell",
    labelKey: "nav.pos",
    element: <POSTerminal />,
    permission: PERMISSIONS.saleDraft,
  },
  {
    path: "/sales/invoices",
    group: "sell",
    labelKey: "nav.invoices",
    element: <InvoicesScreen />,
    permission: PERMISSIONS.saleView,
  },
  {
    path: "/purchasing/orders", advanced: true,
    group: "buy",
    labelKey: "nav.purchaseOrders",
    element: <PurchaseOrdersScreen />,
    permission: PERMISSIONS.orderView,
  },
  {
    path: "/purchasing/bills", advanced: true,
    group: "buy",
    labelKey: "nav.bills",
    element: <BillsScreen />,
    permission: PERMISSIONS.billView,
  },
  {
    path: "/partners/customers",
    group: "sell",
    labelKey: "nav.customers",
    element: <PartnersScreen role="customer" />,
    permission: PERMISSIONS.customerView,
  },
  {
    path: "/partners/suppliers", advanced: true,
    group: "buy",
    labelKey: "nav.suppliers",
    element: <PartnersScreen role="supplier" />,
    permission: PERMISSIONS.supplierView,
  },
  {
    path: "/money/expenses",
    group: "money",
    labelKey: "nav.expenses",
    element: <ExpensesScreen />,
    permission: PERMISSIONS.expenseView,
  },
  {
    path: "/money/debts", advanced: true,
    group: "money",
    labelKey: "nav.debts",
    element: <DebtsScreen />,
    permission: PERMISSIONS.debtView,
  },
  {
    path: "/money/rates", advanced: true,
    group: "money",
    labelKey: "nav.rates",
    element: <RatesScreen />,
    permission: PERMISSIONS.accountView,
  },
  {
    path: "/accounting/chart", advanced: true,
    group: "money",
    labelKey: "nav.chart",
    element: <ChartScreen />,
    permission: PERMISSIONS.accountView,
  },
  {
    path: "/purchasing/receive", advanced: true,
    group: "buy",
    labelKey: "nav.receive",
    element: <ReceiveDeliveryScreen />,
    permission: PERMISSIONS.receiptRecord,
  },
  {
    path: "/purchasing/receipts", advanced: true,
    group: "buy",
    labelKey: "nav.receipts",
    element: <ReceiptsScreen />,
    permission: PERMISSIONS.orderView,
  },
  {
    path: "/purchasing/returns", advanced: true,
    group: "buy",
    labelKey: "nav.supplierReturns",
    element: <SupplierReturnsScreen />,
    permission: PERMISSIONS.orderView,
  },
  {
    path: "/purchasing/payments", advanced: true,
    group: "buy",
    labelKey: "nav.supplierPayments",
    element: <SupplierPaymentsScreen />,
    permission: PERMISSIONS.billView,
  },
  {
    path: "/reports/statements", advanced: true,
    group: "money",
    labelKey: "nav.statements",
    element: <StatementsScreen />,
    permission: PERMISSIONS.profitAndLossView,
  },
  {
    path: "/reports/analysis", advanced: true,
    group: "money",
    labelKey: "nav.analysis",
    element: <AnalysisScreen />,
    permission: PERMISSIONS.salesAnalysisView,
  },
  {
    path: "/reports/valuation", advanced: true,
    group: "stock",
    labelKey: "nav.valuation",
    element: <ValuationScreen />,
    permission: PERMISSIONS.valuationView,
  },
  {
    path: "/operations/backups", advanced: true,
    group: "setup",
    labelKey: "nav.backups",
    element: <BackupScreen />,
    permission: PERMISSIONS.userManage,
  },
  {
    path: "/operations/import", advanced: true,
    group: "setup",
    labelKey: "nav.import",
    element: <ImportScreen />,
    permission: PERMISSIONS.catalogManage,
  },
  {
    path: "/operations/system", advanced: true,
    group: "setup",
    labelKey: "nav.system",
    element: <SystemPanel />,
    permission: PERMISSIONS.sessionView,
  },
  {
    path: "/accounting/trial-balance", advanced: true,
    group: "money",
    labelKey: "nav.trialBalance",
    element: <TrialBalanceScreen />,
    permission: PERMISSIONS.accountView,
  },
  // No permission: changing your own password is not an administrative act, and the person who
  // most needs it may hold nothing at all (1.11 D3).
  { path: "/account/password", advanced: true, group: "setup", labelKey: "nav.password", element: <ChangePasswordScreen /> },
  // No permission: the guide is the one screen a user who can do nothing else must still reach,
  // because it is where they learn what they are looking at.
  { path: "/help", group: "overview", labelKey: "nav.help", element: <HelpScreen /> },
];
