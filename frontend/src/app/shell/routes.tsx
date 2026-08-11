import type { ReactNode } from "react";
import { PERMISSIONS } from "@/lib/wails";
import { AuditScreen } from "@/modules/admin/AuditScreen";
import { RolesScreen } from "@/modules/admin/RolesScreen";
import { SessionsScreen } from "@/modules/admin/SessionsScreen";
import { UsersScreen } from "@/modules/admin/UsersScreen";
import { ChangePasswordScreen } from "@/modules/account/ChangePasswordScreen";
import { ChartScreen } from "@/modules/accounting/ChartScreen";
import { CatalogScreen } from "@/modules/catalog/CatalogScreen";
import { PartnersScreen } from "@/modules/partners/PartnersScreen";
import { TrialBalanceScreen } from "@/modules/accounting/TrialBalanceScreen";
import { SystemPanel } from "@/app/shell/SystemPanel";

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
  { path: "/", labelKey: "nav.dashboard", element: <SystemPanel /> },
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
    path: "/accounting/chart",
    labelKey: "nav.chart",
    element: <ChartScreen />,
    permission: PERMISSIONS.accountView,
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
];
