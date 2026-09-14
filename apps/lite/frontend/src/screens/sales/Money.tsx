import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal } from "@/i18n/numbers";

/**
 * An amount Go formatted, with its currency's short name: "73,000 ل.س", "4.88 USD". Isolated left-to-right, so the
 * digits and the name keep their order inside Arabic text.
 */
export function Money({ value, currency, className }: { value: string; currency: string; className?: string }) {
  const { tDynamic, locale } = useLocale();
  return (
    <bdi dir="ltr" className={className}>
      {formatDecimal(value, locale)} {tDynamic(`currency.short.${currency}`)}
    </bdi>
  );
}

/**
 * True when a figure Go formatted is zero ("0", "0.00", "-0"). A comparison of text, not arithmetic: the screen only
 * decides whether to show a row (DESIGN D9).
 */
export function isZero(value: string): boolean {
  return /^-?0(\.0+)?$/.test(value);
}
