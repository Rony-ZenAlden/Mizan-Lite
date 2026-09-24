import { useRate } from "./RateProvider";

/**
 * The currencies a screen may offer (0.10.0). A shop gone over to dollars only offers dollars alone — Go refuses pounds
 * at every door anyway, so a screen that offered them would offer a refusal. `home` is what a sale, a price or a payment
 * starts in: the local currency, or dollars in a dollars-only shop.
 *
 * `local` falls back to what a screen already knows (a DTO's localCurrency) before the rate has been read.
 */
export function useCurrencies(localFallback = "") {
  const { rate } = useRate();
  const usdOnly = rate?.usdOnly === true;
  const local = rate?.localCurrency || localFallback;
  return {
    usdOnly,
    local,
    home: usdOnly ? "USD" : local,
    currencies: usdOnly ? ["USD"] : [local, "USD"].filter(Boolean),
  };
}
