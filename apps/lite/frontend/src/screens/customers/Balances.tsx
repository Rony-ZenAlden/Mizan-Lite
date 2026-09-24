import type { Balance } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { DUAL_CLOSE, DUAL_OPEN, formatDecimal, splitDual } from "@/i18n/numbers";
import { Money, isZero } from "@/screens/sales/Money";

/** True when a figure Go formatted is below zero. A reading of text, not arithmetic (DESIGN D9). */
export function isNegative(value: string): boolean {
  return value.startsWith("-") && !isZero(value);
}

/** A figure Go formatted, without its sign: "-10.00" → "10.00", and both readings of a dual one. Text only. */
export function unsigned(value: string): string {
  const dual = splitDual(value);
  if (dual) return `${dual[0].trim().replace(/^-/, "")}${DUAL_OPEN}${dual[1].trim().replace(/^-/, "")}${DUAL_CLOSE}`;
  return value.startsWith("-") ? value.slice(1) : value;
}

/**
 * A customer's balances, one per currency and never added together (L5 H2). A balance below zero reads in the customer's
 * favour; with `withReference`, each carries its equivalent in the other currency at the rate named — for reference only
 * (Q-L5.8).
 */
export function Balances({ balances, withReference = false, rate = "" }: { balances: Balance[]; withReference?: boolean; rate?: string }) {
  const { t, tDynamic, locale } = useLocale();
  const owing = balances.filter((b) => !isZero(b.balance));
  if (owing.length === 0) return <span className="text-text-muted">{t("customers.nothing_owed")}</span>;
  return (
    <ul className="space-y-1">
      {owing.map((b) => (
        <li key={b.currency} data-testid={`balance-${b.currency}`}>
          {isNegative(b.balance) ? (
            <span className="text-success">
              {t("customers.in_favour")} <Money value={unsigned(b.balance)} currency={b.currency} />
            </span>
          ) : (
            <Money value={b.balance} currency={b.currency} className="font-medium" />
          )}
          {withReference && b.reference ? (
            <span className="ms-2 text-xs text-text-muted">
              {t("customers.reference", {
                amount: `${formatDecimal(unsigned(b.reference), locale)} ${tDynamic(`currency.short.${b.referenceCurrency}`)}`,
                rate: formatDecimal(rate, locale),
              })}
            </span>
          ) : null}
        </li>
      ))}
    </ul>
  );
}
