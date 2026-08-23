import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { hasPermission, useSession } from "@/app/session/session";
import { PERMISSIONS, rates as loadRates } from "@/lib/wails";
import { formatMinor } from "@/modules/accounting/money";
import { divideMinor } from "./convert";

/**
 * A figure, with what it is worth in the other currency underneath.
 *
 * # What the second line is, and what it is not
 *
 * It is a CONVENIENCE. Nothing is posted from it, nothing is summed from it, and no document is
 * written with it — every figure a document actually carries is converted on the Go side at the
 * rate that document snapshots (§9.3). This is the shopkeeper's mental arithmetic, done for them.
 *
 * That distinction is why the second line is quieter than the first and why it says which rate it
 * used. A converted figure presented as confidently as the real one invites somebody to read the
 * wrong one off the screen.
 *
 * # Silent when it cannot be sure
 *
 * No rate recorded, no permission to see rates, an unparseable amount, or the same currency on
 * both sides: it renders the primary figure alone rather than a zero, a dash, or a stale number.
 * A conversion nobody can vouch for is worse than no conversion.
 */
export function DualAmount({
  minor,
  from = "SYP",
  to = "USD",
  className,
}: {
  /** The amount in minor units, as a string. Never a number. */
  minor: string;
  /** The currency `minor` is in. */
  from?: string;
  /** The currency to show underneath. */
  to?: string;
  className?: string;
}) {
  const { t } = useTranslation();
  const session = useSession();
  const maySee = hasPermission(session?.permissions ?? [], PERMISSIONS.accountView);

  /*
   * The rate is fetched for `to → from`, not `from → to`.
   *
   * A shop records "one dollar buys 15,000 pounds", because that is the number it is quoted. So
   * showing what a pound amount is worth in dollars is a DIVISION by that rate, which
   * `convertMinor` does by inverting: amount × 10⁹ / rateNano.
   */
  const history = useQuery({
    queryKey: ["money", "rates", to, from],
    queryFn: () => loadRates(to, from),
    enabled: maySee && from !== to,
    staleTime: 60_000,
  });

  const primary = formatMinor(minor);
  const rate = history.data?.find((row) => row.inForce);

  const converted = rate ? divideMinor(minor, rate.rateNano) : null;

  return (
    <span className={className}>
      <span className="tabular-nums">{primary}</span>
      {converted ? (
        <span className="ms-1.5 whitespace-nowrap text-xs font-normal text-text-muted">
          ≈ {formatMinor(converted)} {to}
          <span className="sr-only"> {t("rates.approx")}</span>
        </span>
      ) : null}
    </span>
  );
}
