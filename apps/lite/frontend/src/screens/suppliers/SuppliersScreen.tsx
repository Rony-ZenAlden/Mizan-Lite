import { useCallback, useEffect, useRef, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { Purchase, Supplier, SupplierList } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatInteger } from "@/i18n/numbers";
import { formatDate } from "@/i18n/time";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { isNegative, unsigned } from "@/screens/customers/Balances";
import { Money } from "@/screens/sales/Money";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { TextField } from "@/ui/Field";
import { PurchaseDialog } from "./PurchaseDialog";
import { PurchaseView } from "./PurchaseView";
import { SupplierAccount } from "./SupplierAccount";
import { SupplierBalances } from "./SupplierBalances";
import { SupplierForm } from "./SupplierForm";

/** How long typing pauses before a search is sent. */
export const SUPPLIER_SEARCH_DEBOUNCE_MS = 200;

/** How many of the newest purchases the purchases tab lists. */
export const PURCHASES_SHOWN = 200;

type Tab = "suppliers" | "purchases";
type Open =
  | { kind: "new-supplier" }
  | { kind: "purchase"; supplierId: string }
  | { kind: "account"; supplier: Supplier }
  | { kind: "view"; purchase: Purchase }
  | null;

/**
 * The shop's suppliers and what it owes them (0.10.0, the owner's request of 2026-09-24): a book of its own, apart from
 * the customers' debts. Who the shop owes and how much — per currency, never added across them — its purchases on cash or
 * credit, and each supplier's account.
 *
 * With the PIN switch on the whole screen is the owner's: it shows what the shop's goods cost. It asks for the PIN to
 * show the figures and takes them off the screen when owner mode ends.
 */
export function SuppliersScreen() {
  const client = useClient();
  const { status, withOwner } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [tab, setTab] = useState<Tab>("suppliers");
  const [text, setText] = useState("");
  const [includeInactive, setIncludeInactive] = useState(false);
  const [list, setList] = useState<SupplierList | null>(null);
  const [purchases, setPurchases] = useState<Purchase[] | null>(null);
  const [hidden, setHidden] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [open, setOpen] = useState<Open>(null);

  const load = useCallback(async () => {
    try {
      const [found, bought] = await withOwner(() =>
        Promise.all([client.suppliers.list({ text, includeInactive }), client.suppliers.purchases({ supplierId: "", from: "", to: "", limit: PURCHASES_SHOWN })]),
      );
      setList(found);
      setPurchases(bought);
      setHidden(false);
      setError(null);
    } catch (e) {
      if (e instanceof OwnerCancelled) setHidden(true);
      else setError(e);
    }
  }, [client, withOwner, text, includeInactive]);

  useEffect(() => {
    const timer = setTimeout(() => void load(), SUPPLIER_SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [load]);

  // Owner mode ending takes the figures off the screen — when the PIN switch says they are the owner's. With it off they
  // are anyone's, and a countdown running out must not close a purchase half typed; so Go is asked, without the PIN.
  const ownerMode = status.elevatedSeconds > 0;
  const wasOwner = useRef(ownerMode);
  const recheck = useCallback(async () => {
    try {
      await client.suppliers.list({ text: "", includeInactive: false });
    } catch {
      setList(null);
      setPurchases(null);
      setOpen(null);
      setHidden(true);
    }
  }, [client]);
  useEffect(() => {
    if (wasOwner.current && !ownerMode) void recheck();
    wasOwner.current = ownerMode;
  }, [ownerMode, recheck]);

  // Stable, because this screen re-renders every second while the owner's countdown runs.
  const close = useCallback(() => setOpen(null), []);
  const changed = useCallback(() => void load(), [load]);
  const recorded = useCallback(
    (p: Purchase) => {
      setOpen({ kind: "view", purchase: p });
      void load();
    },
    [load],
  );
  const created = useCallback(
    (s: Supplier) => {
      setOpen({ kind: "account", supplier: s });
      void load();
    },
    [load],
  );

  const local = list?.localCurrency ?? "";
  const rate = list?.rate ?? "";
  const suppliers = list?.suppliers ?? [];

  if (hidden) {
    return (
      <section className="space-y-4">
        <h2 className="text-xl font-semibold">{t("suppliers.title")}</h2>
        <p className="text-text-muted">{t("suppliers.hidden")}</p>
        <Button variant="primary" onClick={() => void load()}>
          {t("suppliers.show")}
        </Button>
      </section>
    );
  }

  return (
    <section className="space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <h2 className="text-xl font-semibold">{t("suppliers.title")}</h2>
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => setOpen({ kind: "new-supplier" })}>{t("suppliers.new")}</Button>
          <Button variant="primary" onClick={() => setOpen({ kind: "purchase", supplierId: "" })} disabled={list === null || (suppliers.length === 0 && text === "")}>
            {t("suppliers.new_purchase")}
          </Button>
        </div>
      </header>

      {error ? <Alert tone="danger" title={errorText(error)} /> : null}

      {list && list.totals.length > 0 ? (
        <div className="grid gap-3 sm:grid-cols-2" aria-label={t("suppliers.totals")}>
          {list.totals.map((b) => (
            <div key={b.currency} data-testid={`supplier-total-${b.currency}`} className="rounded-lg border border-border bg-surface-raised p-4 text-sm">
              <p className="text-text-muted">{isNegative(b.balance) ? t("suppliers.total_owed_to_shop") : t("suppliers.total_owed")}</p>
              <p className="text-lg font-semibold">
                <Money value={unsigned(b.balance)} currency={b.currency} />
              </p>
            </div>
          ))}
        </div>
      ) : null}

      <div role="tablist" aria-label={t("suppliers.title")} className="flex gap-2 border-b border-border">
        {(["suppliers", "purchases"] as const).map((k) => (
          <button
            key={k}
            role="tab"
            type="button"
            aria-selected={k === tab}
            onClick={() => setTab(k)}
            className={`px-3 py-2 text-sm ${k === tab ? "border-b-2 border-primary font-semibold" : "text-text-muted"}`}
          >
            {tDynamic(`suppliers.tab.${k}`)}
          </button>
        ))}
      </div>

      {tab === "suppliers" ? (
        <>
          <div className="flex flex-wrap items-end gap-4">
            <div className="min-w-64 flex-1">
              <TextField label={t("suppliers.search")} value={text} onChange={(e) => setText(e.target.value)} type="search" autoComplete="off" />
            </div>
            <Checkbox label={t("suppliers.include_inactive")} checked={includeInactive} onChange={(e) => setIncludeInactive(e.target.checked)} />
          </div>
          {list && suppliers.length === 0 ? <p className="text-text-muted">{text ? t("suppliers.no_results") : t("suppliers.empty")}</p> : null}
          {suppliers.length > 0 ? (
            <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
              <table className="w-full text-sm">
                <thead className="bg-surface text-text-muted">
                  <tr>
                    <th className="p-2 text-start">{t("suppliers.col.name")}</th>
                    <th className="p-2 text-start">{t("suppliers.col.phone")}</th>
                    <th className="p-2 text-start">{t("suppliers.col.city")}</th>
                    <th className="p-2 text-start">{t("suppliers.col.balances")}</th>
                    <th className="p-2 text-start">{t("suppliers.col.actions")}</th>
                  </tr>
                </thead>
                <tbody>
                  {suppliers.map((s) => (
                    <tr key={s.id} className={`border-t border-border ${s.active ? "" : "text-text-muted"}`} data-testid="supplier-row">
                      <td className="p-2">
                        {s.name}
                        {s.active ? null : <span className="ms-2 text-xs">{t("suppliers.inactive")}</span>}
                      </td>
                      <td className="p-2">
                        <bdi dir="ltr">{s.phone}</bdi>
                      </td>
                      <td className="p-2">{s.city}</td>
                      <td className="p-2">
                        <SupplierBalances balances={s.balances} />
                      </td>
                      <td className="p-2">
                        <div className="flex flex-wrap gap-2">
                          <Button onClick={() => setOpen({ kind: "account", supplier: s })}>{t("suppliers.account")}</Button>
                          {s.active ? <Button onClick={() => setOpen({ kind: "purchase", supplierId: s.id })}>{t("suppliers.purchase")}</Button> : null}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </>
      ) : null}

      {tab === "purchases" ? (
        purchases && purchases.length === 0 ? (
          <p className="text-text-muted">{t("purchases.empty")}</p>
        ) : purchases ? (
          <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
            <table className="w-full text-sm">
              <thead className="bg-surface text-text-muted">
                <tr>
                  <th className="p-2 text-start">{t("purchases.col.no")}</th>
                  <th className="p-2 text-start">{t("purchases.col.date")}</th>
                  <th className="p-2 text-start">{t("purchases.col.supplier")}</th>
                  <th className="p-2 text-start">{t("purchases.col.ref")}</th>
                  <th className="p-2 text-start">{t("purchases.col.due")}</th>
                  <th className="p-2 text-start">{t("purchases.col.paid")}</th>
                  <th className="p-2 text-start">{t("purchases.col.status")}</th>
                  <th className="p-2" />
                </tr>
              </thead>
              <tbody>
                {purchases.map((p) => (
                  <tr key={p.id} className={`border-t border-border ${p.status === "voided" ? "text-text-muted line-through" : ""}`} data-testid="purchase-row">
                    <td className="p-2">
                      <bdi dir="ltr">{formatInteger(p.purchaseNo, locale)}</bdi>
                    </td>
                    <td className="p-2">
                      <bdi dir="ltr">{formatDate(p.businessDate)}</bdi>
                    </td>
                    <td className="p-2">{p.supplierName}</td>
                    <td className="p-2">
                      <bdi dir="ltr">{p.supplierRef}</bdi>
                    </td>
                    <td className="p-2">
                      <Money value={p.due} currency={p.currency} />
                    </td>
                    <td className="p-2">
                      <Money value={p.paidNow} currency={p.currency} />
                    </td>
                    <td className="p-2">
                      {tDynamic(`purchase.status.${p.status}`)}
                      {p.damagedLines > 0 ? <span className="ms-2 text-xs text-text-muted">{t("purchases.damaged_lines", { count: formatInteger(p.damagedLines, locale) })}</span> : null}
                    </td>
                    <td className="p-2">
                      <Button onClick={() => setOpen({ kind: "view", purchase: p })}>{t("purchases.view")}</Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null
      ) : null}

      {open?.kind === "new-supplier" ? <SupplierForm onSaved={created} onClose={close} /> : null}
      {open?.kind === "account" ? <SupplierAccount supplier={open.supplier} localCurrency={local} rate={rate} onChanged={changed} onClose={close} /> : null}
      {open?.kind === "purchase" ? (
        <PurchaseDialog supplierId={open.supplierId} localCurrency={local} rate={rate} onRecorded={recorded} onClose={close} />
      ) : null}
      {open?.kind === "view" ? <PurchaseView purchaseId={open.purchase.id} purchase={open.purchase} onChanged={changed} onClose={close} /> : null}
    </section>
  );
}
