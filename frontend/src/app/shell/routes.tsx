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
import { SupplierReturnsScreen } from "@/modules/purchasing/SupplierReturnsScreen";
import { SupplierPaymentsScreen } from "@/modules/purchasing/SupplierPaymentsScreen";
import { HelpScreen } from "@/modules/help/HelpScreen";

/**
 * One route.
 *
 * `permission` drives BOTH the navigation item and the route body's denied state, from one
 * declaration. Two lists would drift, and the symptom — a menu item that leads to a refusal —
 * is exactly the thing permission-aware navigation exists to prevent.
 */
export interface AppRoute {
  path: string;
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
  { path: "/", labelKey: "nav.overview", element: <DashboardScreen /> },
  { path: "/needs-attention", labelKey: "nav.notices", element: <NoticeCentre /> },
  {
    path: "/admin/users",
    labelKey: "nav.users",
    element: <UsersScreen />,
    permission: PERMISSIONS.userView,
  },
  {
    path: "/admin/roles",
    labelKey: "nav.roles",
    element: <RolesScreen />,
    permission: PERMISSIONS.roleView,
  },
  {
    path: "/admin/sessions",
    labelKey: "nav.sessions",
    element: <SessionsScreen />,
    permission: PERMISSIONS.sessionView,
  },
  {
    path: "/admin/audit",
    labelKey: "nav.audit",
    element: <AuditScreen />,
    permission: PERMISSIONS.auditView,
  },
  {
    path: "/catalog",
    labelKey: "nav.catalog",
    element: <CatalogScreen />,
    permission: PERMISSIONS.catalogView,
  },
  {
    path: "/inventory/stock",
    labelKey: "nav.stock",
    element: <StockScreen />,
    permission: PERMISSIONS.stockView,
  },
  // The till is placed FIRST among the working screens, above the ledgers it feeds. For a shop
  // this is the screen that runs all day, and burying it under administration would put the
  // most-used thing in the application behind the least-used.
  {
    path: "/pos",
    labelKey: "nav.pos",
    element: <POSTerminal />,
    permission: PERMISSIONS.saleDraft,
  },
  {
    path: "/sales/invoices",
    labelKey: "nav.invoices",
    element: <InvoicesScreen />,
    permission: PERMISSIONS.saleView,
  },
  {
    path: "/purchasing/orders",
    labelKey: "nav.purchaseOrders",
    element: <PurchaseOrdersScreen />,
    permission: PERMISSIONS.orderView,
  },
  {
    path: "/purchasing/bills",
    labelKey: "nav.bills",
    element: <BillsScreen />,
    permission: PERMISSIONS.billView,
  },
  {
    path: "/partners/customers",
    labelKey: "nav.customers",
    element: <PartnersScreen role="customer" />,
    permission: PERMISSIONS.customerView,
  },
  {
    path: "/partners/suppliers",
    labelKey: "nav.suppliers",
    element: <PartnersScreen role="supplier" />,
    permission: PERMISSIONS.supplierView,
  },
  {
    path: "/money/expenses",
    labelKey: "nav.expenses",
    element: <ExpensesScreen />,
    permission: PERMISSIONS.expenseView,
  },
  {
    path: "/money/debts",
    labelKey: "nav.debts",
    element: <DebtsScreen />,
    permission: PERMISSIONS.debtView,
  },
  {
    path: "/accounting/chart",
    labelKey: "nav.chart",
    element: <ChartScreen />,
    permission: PERMISSIONS.accountView,
  },
  {
    path: "/purchasing/receipts",
    labelKey: "nav.receipts",
    element: <ReceiptsScreen />,
    permission: PERMISSIONS.orderView,
  },
  {
    path: "/purchasing/returns",
    labelKey: "nav.supplierReturns",
    element: <SupplierReturnsScreen />,
    permission: PERMISSIONS.orderView,
  },
  {
    path: "/purchasing/payments",
    labelKey: "nav.supplierPayments",
    element: <SupplierPaymentsScreen />,
    permission: PERMISSIONS.billView,
  },
  {
    path: "/reports/statements",
    labelKey: "nav.statements",
    element: <StatementsScreen />,
    permission: PERMISSIONS.profitAndLossView,
  },
  {
    path: "/reports/analysis",
    labelKey: "nav.analysis",
    element: <AnalysisScreen />,
    permission: PERMISSIONS.salesAnalysisView,
  },
  {
    path: "/reports/valuation",
    labelKey: "nav.valuation",
    element: <ValuationScreen />,
    permission: PERMISSIONS.valuationView,
  },
  {
    path: "/operations/backups",
    labelKey: "nav.backups",
    element: <BackupScreen />,
    permission: PERMISSIONS.userManage,
  },
  {
    path: "/operations/import",
    labelKey: "nav.import",
    element: <ImportScreen />,
    permission: PERMISSIONS.catalogManage,
  },
  {
    path: "/operations/system",
    labelKey: "nav.system",
    element: <SystemPanel />,
    permission: PERMISSIONS.sessionView,
  },
  {
    path: "/accounting/trial-balance",
    labelKey: "nav.trialBalance",
    element: <TrialBalanceScreen />,
    permission: PERMISSIONS.accountView,
  },
  // No permission: changing your own password is not an administrative act, and the person who
  // most needs it may hold nothing at all (1.11 D3).
  { path: "/account/password", labelKey: "nav.password", element: <ChangePasswordScreen /> },
  // No permission: the guide is the one screen a user who can do nothing else must still reach,
  // because it is where they learn what they are looking at.
  { path: "/help", labelKey: "nav.help", element: <HelpScreen /> },
];
