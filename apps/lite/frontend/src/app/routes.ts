import type { ComponentType } from "react";
import type { MessageKey } from "@/i18n/messages";
import { HomeScreen } from "@/screens/home/HomeScreen";
import { OwnerScreen } from "@/screens/owner/OwnerScreen";
import { ProductsScreen } from "@/screens/products/ProductsScreen";

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
  { path: "/products", labelKey: "nav.products", Screen: ProductsScreen },
  { path: "/owner", labelKey: "nav.owner", Screen: OwnerScreen },
  { path: "/", labelKey: "nav.home", Screen: HomeScreen },
];
