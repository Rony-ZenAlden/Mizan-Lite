import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { USDOnlyPlan } from "@/api/client";
import { BindingError } from "@/api/envelope";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal, formatInteger } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { useRate } from "@/rates/RateProvider";
import { Money } from "@/screens/sales/Money";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { Dialog } from "@/ui/Dialog";
import { Spinner } from "@/ui/Spinner";

/** How many prices the dialog lists before saying how many more there are. */
export const PRICES_SHOWN = 50;

const CODE_PLAN_CHANGED = "lite.usdmode.plan_changed";

/**
 * Going over to US dollars only (0.10.0, the owner's answer of 2026-09-24: "convert everything"). Go works out what every
 * pound price, balance and the drawer come to at the rate in force and shows it here, nothing written; the switch writes
 * exactly that — the token names it — or nothing. Every figure is Go's.
 */
export function USDOnlyDialog({ onDone, onClose }: { onDone: () => void; onClose: () => void }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { reload } = useRate();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [plan, setPlan] = useState<USDOnlyPlan | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [understood, setUnderstood] = useState(false);
  const [busy, setBusy] = useState(false);
  const closing = useRef(onClose);
  useLayoutEffect(() => {
    closing.current = onClose;
  });

  // The plan is read once, when the dialog opens; a plan that changed is read again when the switch says so.
  useEffect(() => {
    let cancelled = false;
    withOwner(() => client.settings.usdOnlyPlan())
      .then((p) => {
        if (!cancelled) setPlan(p);
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        if (e instanceof OwnerCancelled) closing.current();
        else setError(e);
      });
    return () => {
      cancelled = true;
    };
  }, [client, withOwner]);

  const confirm = async () => {
    if (!plan) return;
    setBusy(true);
    setError(null);
    try {
      await withOwner(() => client.settings.switchToUsdOnly(plan.token));
      await reload();
      onDone();
    } catch (e) {
      if (e instanceof OwnerCancelled) return;
      setError(e);
      // Something moved since the plan was shown: read it again, and ask again.
      if (e instanceof BindingError && e.code === CODE_PLAN_CHANGED) {
        setUnderstood(false);
        try {
          setPlan(await client.settings.usdOnlyPlan());
        } catch (again) {
          setError(again);
        }
      }
    } finally {
      setBusy(false);
    }
  };

  const name = (nameAr: string, nameEn: string) => (locale === "en" && nameEn ? nameEn : nameAr);
  const local = plan?.localCurrency ?? "";

  return (
    <Dialog title={t("usd_only.title")} onClose={onClose} wide>
      {error ? <Alert tone="danger" title={errorText(error)} /> : null}
      {!plan && !error ? <Spinner label={t("state.loading")} /> : null}
      {plan ? (
        <div className="space-y-4 text-sm" data-testid="usd-only-plan">
          <p>{t("usd_only.intro", { rate: formatDecimal(plan.rate, locale), currency: tDynamic(`currency.${local}`) })}</p>

          <section className="space-y-2" aria-label={t("usd_only.prices")}>
            <h3 className="font-semibold">{t("usd_only.prices_count", { count: formatInteger(plan.prices.length, locale) })}</h3>
            {plan.prices.length > 0 ? (
              <div className="max-h-64 overflow-auto rounded-md border border-border">
                <table className="w-full text-sm">
                  <thead className="bg-surface text-text-muted">
                    <tr>
                      <th className="p-2 text-start">{t("usd_only.col.item")}</th>
                      <th className="p-2 text-start">{t("usd_only.col.pounds")}</th>
                      <th className="p-2 text-start">{t("usd_only.col.dollars")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {plan.prices.slice(0, PRICES_SHOWN).map((p) => (
                      <tr key={p.productId} className="border-t border-border" data-testid="usd-only-price">
                        <td className="p-2">
                          {name(p.nameAr, p.nameEn)}
                          {p.raisedToCent ? <span className="ms-2 text-xs text-danger">{t("usd_only.raised")}</span> : null}
                        </td>
                        <td className="p-2">
                          <Money value={p.localPrice} currency={local} />
                        </td>
                        <td className="p-2">
                          <Money value={p.usdPrice} currency="USD" className="font-medium" />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}
            {plan.prices.length > PRICES_SHOWN ? (
              <p className="text-xs text-text-muted">{t("usd_only.more", { count: formatInteger(plan.prices.length - PRICES_SHOWN, locale) })}</p>
            ) : null}
            {plan.openItems > 0 ? <p className="text-xs text-text-muted">{t("usd_only.open_items")}</p> : null}
          </section>

          {(
            [
              ["customers", plan.customers],
              ["suppliers", plan.suppliers],
            ] as const
          ).map(([key, balances]) =>
            balances.length > 0 ? (
              <section key={key} className="space-y-2" aria-label={tDynamic(`usd_only.${key}`)}>
                <h3 className="font-semibold">{tDynamic(`usd_only.${key}`)}</h3>
                <div className="max-h-48 overflow-auto rounded-md border border-border">
                  <table className="w-full text-sm">
                    <tbody>
                      {balances.map((b) => (
                        <tr key={b.id} className="border-t border-border first:border-t-0" data-testid={`usd-only-${key}`}>
                          <td className="p-2">{b.name}</td>
                          <td className="p-2">
                            <Money value={b.local} currency={local} />
                          </td>
                          <td className="p-2">
                            <Money value={b.dollars} currency="USD" className="font-medium" />
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </section>
            ) : null,
          )}

          <p data-testid="usd-only-drawer">
            {t("usd_only.drawer")} <Money value={plan.drawerLocal} currency={local} /> → <Money value={plan.drawerDollars} currency="USD" />
          </p>

          <Checkbox label={t("usd_only.understood")} checked={understood} onChange={(e) => setUnderstood(e.target.checked)} />
          <div className="flex justify-end gap-2">
            <Button onClick={onClose}>{t("action.cancel")}</Button>
            <Button variant="primary" onClick={() => void confirm()} disabled={busy || !understood}>
              {t("usd_only.confirm")}
            </Button>
          </div>
        </div>
      ) : null}
    </Dialog>
  );
}
