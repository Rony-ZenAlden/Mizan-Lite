import type { ComponentType } from "react";
import type { MessageKey } from "@/i18n/messages";
import { AboutScreen } from "@/screens/about/AboutScreen";
import { BackupsScreen } from "@/screens/backups/BackupsScreen";
import { CashScreen } from "@/screens/cash/CashScreen";
import { CustomersScreen } from "@/screens/customers/CustomersScreen";
import { OwnerScreen } from "@/screens/owner/OwnerScreen";
import { PrinterScreen } from "@/screens/printer/PrinterScreen";
import { ProductsScreen } from "@/screens/products/ProductsScreen";
import { RatesScreen } from "@/screens/rates/RatesScreen";
import { ReportsScreen } from "@/screens/reports/ReportsScreen";
import { SalesScreen } from "@/screens/sales/SalesScreen";
import { StockScreen } from "@/screens/stock/StockScreen";
import { TillScreen } from "@/screens/till/TillScreen";

export interface RouteDef {
  path: string;
  labelKey: MessageKey;
  Screen: ComponentType;
}

/**
 * Every route, as DATA. The router and the navigation both render from this one list, so a screen
 * cannot be routed without appearing in navigation, or listed without being routed (Mizan 10.13).
 * Gate G4 adds the other direction: every *Screen component must appear here.
 */
export const ROUTES: readonly RouteDef[] = [
  // The counter opens on the Till (L8 A-L8.3).
  { path: "/", labelKey: "nav.till", Screen: TillScreen },
  { path: "/sales", labelKey: "nav.sales", Screen: SalesScreen },
  { path: "/cash", labelKey: "nav.cash", Screen: CashScreen },
  { path: "/customers", labelKey: "nav.customers", Screen: CustomersScreen },
  { path: "/products", labelKey: "nav.products", Screen: ProductsScreen },
  { path: "/stock", labelKey: "nav.stock", Screen: StockScreen },
  { path: "/rates", labelKey: "nav.rates", Screen: RatesScreen },
  { path: "/reports", labelKey: "nav.reports", Screen: ReportsScreen },
  { path: "/printer", labelKey: "nav.printer", Screen: PrinterScreen },
  { path: "/backups", labelKey: "nav.backups", Screen: BackupsScreen },
  { path: "/owner", labelKey: "nav.owner", Screen: OwnerScreen },
  { path: "/about", labelKey: "nav.about", Screen: AboutScreen },
];
