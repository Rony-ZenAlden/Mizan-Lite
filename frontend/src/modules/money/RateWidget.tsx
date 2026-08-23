import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { hasPermission, useSession } from "@/app/session/session";
import { PERMISSIONS, rates as loadRates } from "@/lib/wails";

/**
 * Today's trading rate, in the header.
 *
 * # Why this belongs in the chrome rather than on a screen
 *
 * In a country with a moving currency the rate is not a setting, it is a fact of the day — the
 * number a shopkeeper checks before quoting a price and again before paying a supplier. Putting
 * it behind a menu means it is consulted once in the morning and then trusted for eight hours,
 * which is exactly how a day's takings end up converted at yesterday's number.
 *
 * # It shows the MARKET rate, and says which one it is showing
 *
 * §G.1 binds trade to the market rate and the state-facing contexts to the official one. Those
 * are different numbers, sometimes by a lot. A widget showing an unlabelled figure would be read
 * as "the" rate, and the reader would have no way to know which of the two they were looking at.
 *
 * # Silent when there is nothing to say
 *
 * No rate recorded, no permission, or a shop whose books and prices are the same currency: the
 * widget renders nothing rather than an empty box or a zero. A header is not the place to explain
 * an absence.
 */
export function RateWidget({ from = "USD", to = "SYP" }: { from?: string; to?: string }) {
  const { t } = useTranslation();
  const session = useSession();
  const maySee = hasPermission(session?.permissions ?? [], PERMISSIONS.accountView);

  const history = useQuery({
    queryKey: ["money", "rates", from, to],
    queryFn: () => loadRates(from, to),
    enabled: maySee,
    // The rate changes when somebody records one, not on a timer. Refetching on focus catches
    // the case that actually happens: it was updated in another window, or on the till next door.
    refetchOnWindowFocus: true,
  });

  if (!maySee) return null;

  const current = history.data?.find((rate) => rate.inForce);
  if (!current) return null;

  return (
    <Link
      to="/money/rates"
      className={
        "hidden items-baseline gap-1.5 rounded-lg border border-border px-2.5 py-1 " +
        "text-xs transition-colors hover:border-border-strong hover:bg-surface-raised lg:flex"
      }
      title={t("rates.widgetTitle", { from, to })}
    >
      <span className="font-medium text-text-muted">
        {from}/{to}
      </span>
      <span className="font-semibold tabular-nums text-text">{trim(current.rate)}</span>
      {/* Which rate. An unlabelled figure gets read as "the" rate. */}
      <span className="text-text-muted">{t(`rates.type.${current.rateType}`)}</span>
    </Link>
  );
}

function trim(rate: string): string {
  if (!rate.includes(".")) return rate;
  return rate.replace(/0+$/, "").replace(/\.$/, "");
}
