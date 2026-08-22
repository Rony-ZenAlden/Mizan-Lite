import { ROUTES, type AppRoute } from "@/app/shell/routes";
import { hasPermission } from "@/app/session/session";
import { PERMISSIONS } from "@/lib/wails";

/**
 * What the command palette can do, as data.
 *
 * # Why quick actions are declared here and not inside the palette
 *
 * The palette renders them; it does not decide what they are. Keeping the list separate is what
 * lets a test assert the RULES about it — that every action points at a route that exists, and
 * that every action asks for a permission the backend actually declares — without rendering a
 * dialog and typing into it.
 *
 * Both of those are silent failures otherwise. An action pointing at a path no route serves
 * lands the user on an empty screen; an action gated on an invented permission is either always
 * hidden or always shown, and nobody notices which.
 */
export interface QuickAction {
  /** Stable identifier, and the i18n key suffix. Never a translated label. */
  key: string;
  /** Where it goes. Must be a path some route serves. */
  to: string;
  /** Empty means "everyone who is signed in". */
  permission?: string;
}

/**
 * The verbs, as opposed to the places.
 *
 * A palette that only lists screens makes the user translate an intention ("sell something")
 * into a location ("the till"). These are the three intentions a shop has most often, named the
 * way somebody would say them out loud.
 *
 * Deliberately short. A palette whose action list is longer than its navigation list has stopped
 * being a shortcut and become a second menu.
 */
export const QUICK_ACTIONS: QuickAction[] = [
  { key: "newSale", to: "/pos", permission: PERMISSIONS.saleDraft },
  { key: "addProduct", to: "/catalog", permission: PERMISSIONS.catalogManage },
  { key: "viewReports", to: "/reports/statements", permission: PERMISSIONS.profitAndLossView },
  { key: "receiveDelivery", to: "/purchasing/receive", permission: PERMISSIONS.receiptRecord },
  { key: "takeBackup", to: "/operations/backups", permission: PERMISSIONS.userManage },
];

/**
 * Where a search result goes when somebody picks it.
 *
 * Keyed by the backend's `kind`, which is `module.entity`. An unknown kind returns undefined
 * rather than guessing a path — a palette that navigated somewhere plausible for a kind it did
 * not recognise would be confidently wrong, and the user would have no way to tell.
 */
const DESTINATION: Record<string, string> = {
  "catalog.product": "/catalog",
  "partner.partner": "/partners/customers",
  "sales.invoice": "/sales/invoices",
  "sales.credit_note": "/sales/invoices",
  "sales.order": "/sales/invoices",
  "sales.quotation": "/sales/invoices",
  "purchasing.bill": "/purchasing/bills",
};

export function destinationFor(kind: string): string | undefined {
  return DESTINATION[kind];
}

/** The kinds the palette knows how to open. Exported so a test can pin the set. */
export function knownKinds(): string[] {
  return Object.keys(DESTINATION);
}

/**
 * The routes a given session may navigate to.
 *
 * COSMETIC, exactly like the sidebar's filter (§FE.3). It hides what would refuse, so a user
 * does not learn to ignore errors. Every binding re-checks on the Go side (§14.3).
 */
export function navigableRoutes(permissions: string[]): AppRoute[] {
  return ROUTES.filter(
    (route) => !route.permission || hasPermission(permissions, route.permission),
  );
}

/** The quick actions a given session may run. Same rule, same reason. */
export function availableActions(permissions: string[]): QuickAction[] {
  return QUICK_ACTIONS.filter(
    (action) => !action.permission || hasPermission(permissions, action.permission),
  );
}
