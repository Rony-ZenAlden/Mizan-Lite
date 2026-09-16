import type { Locale } from "./messages";
import { formatDecimal } from "./numbers";

/**
 * The two sides of a rate sentence — "1 USD" and "15,000 SYP" — each a figure with its currency's short name, so an Arabic
 * sentence isolates each side as one figure and shows it the way `Money` does (L8 S2).
 */
export function ratePair(rate: string, localCurrency: string, locale: Locale, tDynamic: (key: string) => string): { usd: string; local: string } {
  return { usd: `1 ${tDynamic("currency.short.USD")}`, local: `${formatDecimal(rate, locale)} ${tDynamic(`currency.short.${localCurrency}`)}` };
}
