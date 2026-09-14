import type { ComponentType } from "react";
import type { MessageKey } from "@/i18n/messages";
import { CustomersScreen } from "@/screens/customers/CustomersScreen";
import { HomeScreen } from "@/screens/home/HomeScreen";
import { OwnerScreen } from "@/screens/owner/OwnerScreen";
import { ProductsScreen } from "@/screens/products/ProductsScreen";
import { RatesScreen } from "@/screens/rates/RatesScreen";
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
  { path: "/till", labelKey: "nav.till", Screen: TillScreen },
  { path: "/sales", labelKey: "nav.sales", Screen: SalesScreen },
  { path: "/customers", labelKey: "nav.customers", Screen: CustomersScreen },
  { path: "/products", labelKey: "nav.products", Screen: ProductsScreen },
  { path: "/stock", labelKey: "nav.stock", Screen: StockScreen },
  { path: "/rates", labelKey: "nav.rates", Screen: RatesScreen },
  { path: "/owner", labelKey: "nav.owner", Screen: OwnerScreen },
  { path: "/", labelKey: "nav.home", Screen: HomeScreen },
];
