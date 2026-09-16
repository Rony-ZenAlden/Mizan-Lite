import { useCallback, useEffect, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { Customer, Outstanding } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatInteger } from "@/i18n/numbers";
import { Money } from "@/screens/sales/Money";
import { Alert } from "@/ui/Alert";
import { RangeExport } from "@/exports/ExportButtons";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { TextField } from "@/ui/Field";
import { Balances } from "./Balances";
import { CustomerForm } from "./CustomerForm";
import { formatDate } from "@/i18n/time";
import { CustomerStatement } from "./CustomerStatement";

/** How long typing pauses before a search is sent. */
export const CUSTOMER_SEARCH_DEBOUNCE_MS = 200;

/**
 * Customers and their debts (L5 §11.2): who owes what — one balance per currency, never added together, each with its
 * reference in the other currency at today's rate (Q-L5.8) — the day's repayments per currency, and a customer's statement.
 */
export function CustomersScreen() {
  const client = useClient();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [text, setText] = useState("");
  const [owingOnly, setOwingOnly] = useState(false);
  const [includeInactive, setIncludeInactive] = useState(false);
  const [customers, setCustomers] = useState<Customer[] | null>(null);
  const [outstanding, setOutstanding] = useState<Outstanding | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [creating, setCreating] = useState(false);
  const [open, setOpen] = useState<Customer | null>(null);

  const load = useCallback(async () => {
    try {
      const [found, out] = await Promise.all([client.customers.search({ text, owingOnly, includeInactive }), client.customers.outstanding()]);
      setCustomers(found);
      setOutstanding(out);
      setError(null);
    } catch (e) {
      setError(e);
    }
  }, [client, text, owingOnly, includeInactive]);

  useEffect(() => {
    const timer = setTimeout(() => void load(), CUSTOMER_SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [load]);

  const local = outstanding?.localCurrency ?? "";

  return (
    <section className="space-y-4">
      <header className="flex items-center justify-between gap-4">
        <h2 className="text-xl font-semibold">{t("customers.title")}</h2>
        <Button variant="primary" onClick={() => setCreating(true)}>
          {t("customers.new")}
        </Button>
      </header>

      <div className="flex flex-wrap items-end gap-4">
        <div className="min-w-64 flex-1">
          <TextField label={t("customers.search")} value={text} onChange={(e) => setText(e.target.value)} type="search" autoComplete="off" />
        </div>
        <Checkbox label={t("customers.owing_only")} checked={owingOnly} onChange={(e) => setOwingOnly(e.target.checked)} />
        <Checkbox label={t("customers.include_inactive")} checked={includeInactive} onChange={(e) => setIncludeInactive(e.target.checked)} />
      </div>

      {error ? <Alert tone="danger" title={errorText(error)} /> : null}

      {outstanding && outstanding.today.length > 0 ? (
        <div className="grid gap-3 sm:grid-cols-2" aria-label={t("customers.today")}>
          {outstanding.today.map((d) => (
            <dl key={d.currency} data-testid={`debt-today-${d.currency}`} className="grid grid-cols-[1fr_auto] gap-x-4 gap-y-1 rounded-lg border border-border bg-surface-raised p-4 text-sm">
              <dt className="col-span-2 font-semibold">{t("customers.today_in", { currency: tDynamic(`currency.${d.currency}`) })}</dt>
              <dt>{t("customers.today_payments", { count: formatInteger(d.payments, locale) })}</dt>
              <dd>
                <Money value={d.settled} currency={d.currency} />
              </dd>
              <dt>{t("customers.today_cash_in")}</dt>
              <dd>
                <Money value={d.cashIn} currency={d.currency} />
              </dd>
              <dt>{t("customers.today_charged")}</dt>
              <dd>
                <Money value={d.charged} currency={d.currency} />
              </dd>
            </dl>
          ))}
        </div>
      ) : null}

      {customers && customers.length === 0 ? <p className="text-text-muted">{text ? t("customers.no_results") : t("customers.empty")}</p> : null}
      {customers && customers.length > 0 ? (
        <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
          <table className="w-full text-sm">
            <thead className="bg-surface text-text-muted">
              <tr>
                <th className="p-2 text-start">{t("customers.col.name")}</th>
                <th className="p-2 text-start">{t("customers.col.phone")}</th>
                <th className="p-2 text-start">{t("customers.col.balances")}</th>
                <th className="p-2 text-start">{t("customers.col.owed_since")}</th>
                <th className="p-2 text-start">{t("customers.col.last_payment")}</th>
                <th className="p-2 text-start">{t("customers.col.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {customers.map((c) => (
                <tr key={c.id} className={`border-t border-border ${c.active ? "" : "text-text-muted"}`}>
                  <td className="p-2">
                    {c.name}
                    {c.active ? null : <span className="ms-2 text-xs">{t("customers.inactive")}</span>}
                  </td>
                  <td className="p-2">
                    <bdi dir="ltr">{c.phone}</bdi>
                  </td>
                  <td className="p-2">
                    <Balances balances={c.balances} withReference rate={outstanding?.rate ?? ""} />
                  </td>
                  <td className="p-2">
                    <bdi dir="ltr">
                      {c.balances
                        .map((b) => formatDate(b.owedSince))
                        .filter(Boolean)
                        .join(" · ")}
                    </bdi>
                  </td>
                  <td className="p-2">
                    <bdi dir="ltr">
                      {c.balances
                        .map((b) => formatDate(b.lastPayment))
                        .filter(Boolean)
                        .join(" · ")}
                    </bdi>
                  </td>
                  <td className="p-2">
                    <Button onClick={() => setOpen(c)}>{t("customers.statement")}</Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      <RangeExport title={t("customers.export_ledger")} onExport={(from, to, format) => client.exports.debtLedger(from, to, format)} />

      {creating ? (
        <CustomerForm
          onSaved={(c) => {
            setCreating(false);
            setOpen(c);
            void load();
          }}
          onClose={() => setCreating(false)}
        />
      ) : null}
      {open ? <CustomerStatement customer={open} localCurrency={local} onChanged={() => void load()} onClose={() => setOpen(null)} /> : null}
    </section>
  );
}
