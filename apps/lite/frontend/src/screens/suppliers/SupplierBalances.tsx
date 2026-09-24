import type { SupplierBalance } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { isNegative, unsigned } from "@/screens/customers/Balances";
import { Money, isZero } from "@/screens/sales/Money";

/**
 * A supplier's balances, one per currency and never added together (0.10.0). Above zero is what the shop owes the
 * supplier; below zero the supplier owes the shop — money paid ahead, or paid for goods that arrived damaged.
 */
export function SupplierBalances({ balances }: { balances: SupplierBalance[] }) {
  const { t } = useLocale();
  const open = balances.filter((b) => !isZero(b.balance));
  if (open.length === 0) return <span className="text-text-muted">{t("suppliers.settled")}</span>;
  return (
    <ul className="space-y-1">
      {open.map((b) => (
        <li key={b.currency} data-testid={`supplier-balance-${b.currency}`}>
          {isNegative(b.balance) ? (
            <span className="text-success">
              {t("suppliers.owes_shop")} <Money value={unsigned(b.balance)} currency={b.currency} />
            </span>
          ) : (
            <span>
              {t("suppliers.shop_owes")} <Money value={b.balance} currency={b.currency} className="font-medium" />
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}
