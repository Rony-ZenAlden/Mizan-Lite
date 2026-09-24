import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal, splitDual } from "@/i18n/numbers";

/**
 * An amount Go formatted, with its currency's short name: "73,000 ل.س", "4.88 USD". Isolated left-to-right, so the
 * digits and the name keep their order inside Arabic text.
 *
 * A shop reading both currencies (L10) receives two figures at once. The new one is the amount; the old one follows it
 * small and in brackets, so a shopkeeper who still thinks in old pounds can recognise the sum without the screen
 * pretending there are two prices.
 */
export function Money({ value, currency, className }: { value: string; currency: string; className?: string }) {
  const { tDynamic, locale } = useLocale();
  const short = tDynamic(`currency.short.${currency}`);
  const dual = splitDual(value);
  if (dual) {
    return (
      <bdi dir="ltr" className={className} data-testid="money-dual">
        {formatDecimal(dual[0], locale)} {short}{" "}
        <span className="text-[0.8em] opacity-70">({formatDecimal(dual[1], locale)})</span>
      </bdi>
    );
  }
  return (
    <bdi dir="ltr" className={className}>
      {formatDecimal(value, locale)} {short}
    </bdi>
  );
}

/**
 * True when a figure Go formatted is zero ("0", "0.00", "-0", and in a dual reading "0 (0)"). A comparison of text, not
 * arithmetic: the screen only decides whether to show a row (DESIGN D9). Until 0.10.0 a dual zero read as a figure, and a
 * settled balance showed "0 (0)" in a shop reading both pounds.
 */
export function isZero(value: string): boolean {
  const figure = splitDual(value)?.[0] ?? value;
  return /^-?0(\.0+)?$/.test(figure.trim());
}
