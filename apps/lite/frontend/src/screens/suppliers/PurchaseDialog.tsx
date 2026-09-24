import { useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { CashSource, Product, Purchase, PurchaseInput, PurchaseQuote, Supplier } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal, typeable } from "@/i18n/numbers";
import { isNegative, unsigned } from "@/screens/customers/Balances";
import { Money, isZero } from "@/screens/sales/Money";
import { formErrors } from "@/screens/stock/forms";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { CellInput, SelectField, TextField } from "@/ui/Field";
import { PurchaseTotals } from "./PurchaseView";
import { SourceField, useSupplierAct } from "./SupplierDialogs";

/** How long typing pauses before Go is asked to work the purchase out again. */
export const PURCHASE_QUOTE_DEBOUNCE_MS = 250;

/** How many products the item search offers at once. */
const MATCHES = 8;

type DiscountMode = "percent" | "amount";

interface Line {
  key: number;
  product: Product;
  quantity: string;
  damaged: string;
  unitCost: string;
  discountMode: DiscountMode;
  discount: string;
}

const blank = (product: Product, key: number): Line => ({ key, product, quantity: "", damaged: "", unitCost: "", discountMode: "percent", discount: "" });

/**
 * A purchase from a supplier (0.10.0): the items that arrived — how many, how many of them damaged, the price of one, a
 * discount off the line as a percentage or an amount — a discount off the whole invoice, and what was paid on the spot
 * and from where. Whatever is not paid goes on the supplier's account.
 *
 * Every figure is Go's: the form sends what was typed and shows the quote Go works out, line by line — what each line
 * costs and what one good unit comes to after every discount, which is the cost the stock book receives it at. Damaged
 * units are never received and never charged (the owner's answer, 2026-09-24).
 */
export function PurchaseDialog({
  suppliers: given,
  supplierId: initialSupplier = "",
  localCurrency,
  rate: rateInForce,
  onRecorded,
  onClose,
}: {
  /** The suppliers to choose from; every active one when not given. */
  suppliers?: Supplier[];
  supplierId?: string;
  localCurrency: string;
  rate: string;
  onRecorded: (p: Purchase) => void;
  onClose: () => void;
}) {
  const client = useClient();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [supplierId, setSupplierId] = useState(initialSupplier);
  const [currency, setCurrency] = useState("USD");
  const [rate, setRate] = useState("");
  const [supplierRef, setSupplierRef] = useState("");
  const [lines, setLines] = useState<Line[]>([]);
  const [invoiceDiscount, setInvoiceDiscount] = useState("");
  const [paidNow, setPaidNow] = useState("");
  const [paidFrom, setPaidFrom] = useState<CashSource>("drawer");
  const [note, setNote] = useState("");
  const [products, setProducts] = useState<Product[] | null>(null);
  const [loaded, setLoaded] = useState<Supplier[]>([]);
  // A scanner types a barcode and Enter faster than the products load when the dialog has just opened: the Enter is
  // remembered, and answered once they are here (found by the end-to-end journey J13).
  const [pendingEnter, setPendingEnter] = useState(false);
  const [search, setSearch] = useState("");
  const [quote, setQuote] = useState<PurchaseQuote | null>(null);
  const [quoteError, setQuoteError] = useState<unknown>(null);
  const nextKey = useRef(1);
  const asked = useRef(0);
  const { error, busy, run } = useSupplierAct(onRecorded);

  useEffect(() => {
    if (given) return;
    let cancelled = false;
    client.suppliers
      .list({ text: "", includeInactive: false })
      .then((all) => {
        if (!cancelled) setLoaded(all.suppliers);
      })
      .catch((e: unknown) => {
        if (!cancelled) setQuoteError(e);
      });
    return () => {
      cancelled = true;
    };
  }, [client, given]);

  useEffect(() => {
    let cancelled = false;
    client.catalog
      .products({ text: "", includeInactive: false })
      .then((all) => {
        // An open-priced item is never kept in stock, so it is never bought into it (2026-09-23).
        if (!cancelled) setProducts(all.filter((p) => p.active && !p.openPrice));
      })
      .catch((e: unknown) => {
        if (!cancelled) setQuoteError(e);
      });
    return () => {
      cancelled = true;
    };
  }, [client]);

  const needsRate = currency !== "USD";
  const input: PurchaseInput = useMemo(
    () => ({
      supplierId,
      currency,
      rate: needsRate ? rate : "",
      supplierRef,
      lines: lines.map((l) => ({
        productId: l.product.id,
        quantity: l.quantity,
        damaged: l.damaged,
        unitCost: l.unitCost,
        discountPercent: l.discountMode === "percent" ? l.discount : "",
        discountAmount: l.discountMode === "amount" ? l.discount : "",
      })),
      invoiceDiscount,
      paidNow,
      paidFrom: paidNow.trim() === "" ? "" : paidFrom,
      note,
    }),
    [supplierId, currency, needsRate, rate, supplierRef, lines, invoiceDiscount, paidNow, paidFrom, note],
  );
  // Go is asked only once there is something to work out: a supplier, and every line with a quantity and a price.
  const ready = supplierId !== "" && lines.length > 0 && lines.every((l) => l.quantity.trim() !== "" && l.unitCost.trim() !== "") && (!needsRate || rate.trim() !== "");

  useEffect(() => {
    if (!ready) {
      setQuote(null);
      setQuoteError(null);
      return;
    }
    const mine = ++asked.current;
    const timer = setTimeout(() => {
      client.suppliers
        .quotePurchase(input)
        .then((q) => {
          if (mine !== asked.current) return; // a later quote is on its way
          setQuote(q);
          setQuoteError(null);
        })
        .catch((e: unknown) => {
          if (mine !== asked.current) return;
          setQuote(null);
          setQuoteError(e);
        });
    }, PURCHASE_QUOTE_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [client, input, ready]);

  const name = (p: Product) => (locale === "en" && p.nameEn ? p.nameEn : p.nameAr);
  const matches = useMemo(() => {
    const text = search.trim().toLowerCase();
    if (text === "" || products === null) return [];
    const byBarcode = products.filter((p) => p.barcode !== "" && p.barcode === search.trim());
    if (byBarcode.length > 0) return byBarcode;
    return products.filter((p) => p.nameAr.toLowerCase().includes(text) || p.nameEn.toLowerCase().includes(text)).slice(0, MATCHES);
  }, [products, search]);

  const add = (product: Product) => {
    setLines((current) => [...current, blank(product, nextKey.current++)]);
    setSearch("");
  };

  useEffect(() => {
    if (!pendingEnter || products === null) return;
    setPendingEnter(false);
    const first = matches[0];
    if (first) {
      setLines((current) => [...current, blank(first, nextKey.current++)]);
      setSearch("");
    }
  }, [pendingEnter, products, matches]);
  const change = (key: number, patch: Partial<Line>) => setLines((current) => current.map((l) => (l.key === key ? { ...l, ...patch } : l)));
  const remove = (key: number) => setLines((current) => current.filter((l) => l.key !== key));

  const onSearchKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "Enter") return;
    event.preventDefault(); // Enter adds the item; it never records the purchase
    if (products === null) setPendingEnter(true);
    else if (matches[0]) add(matches[0]);
  };

  const chooseCurrency = (next: string) => {
    setCurrency(next);
    // Pre-filled from the rate in force, still editable: the rate the shop paid at is the one kept (as a delivery's).
    if (next !== "USD" && rate === "" && rateInForce) setRate(typeable(rateInForce));
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    void run(() => client.suppliers.recordPurchase(input));
  };

  // A refusal from recording, else from the latest quote: under the field or line it names.
  const errors = formErrors(error ?? quoteError, errorText);
  const lineError = (index: number, field: string) => errors.field(`lines.${index + 1}.${field}`);
  const quoted = (index: number) => quote?.purchase.lines.find((l) => l.lineNo === index + 1);
  const active = (given ?? loaded).filter((s) => s.active || s.id === supplierId);
  const balanceText = (value: string) =>
    isNegative(value) ? (
      <span className="text-success">
        {t("suppliers.owes_shop")} <Money value={unsigned(value)} currency={currency} />
      </span>
    ) : isZero(value) ? (
      t("suppliers.settled")
    ) : (
      <span>
        {t("suppliers.shop_owes")} <Money value={value} currency={currency} />
      </span>
    );

  return (
    <Dialog title={t("purchase.title")} onClose={onClose} wide>
      <form className="space-y-4" onSubmit={submit} aria-label={t("purchase.title")}>
        <div className="grid gap-3 sm:grid-cols-2">
          <SelectField label={t("purchase.supplier")} value={supplierId} onChange={(e) => setSupplierId(e.target.value)} error={errors.field("supplierId")} required>
            <option value="">{t("purchase.choose_supplier")}</option>
            {active.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
                {s.city ? ` — ${s.city}` : ""}
              </option>
            ))}
          </SelectField>
          <TextField label={t("purchase.ref")} value={supplierRef} onChange={(e) => setSupplierRef(e.target.value)} error={errors.field("supplierRef")} maxLength={40} dir="ltr" autoComplete="off" />
          <SelectField label={t("purchase.currency")} value={currency} onChange={(e) => chooseCurrency(e.target.value)} error={errors.field("currency")}>
            {[localCurrency, "USD"].filter(Boolean).map((c) => (
              <option key={c} value={c}>
                {tDynamic(`currency.${c}`)}
              </option>
            ))}
          </SelectField>
          {needsRate ? (
            <TextField
              label={t("purchase.rate")}
              value={rate}
              onChange={(e) => setRate(e.target.value)}
              error={errors.field("rate")}
              hint={rateInForce ? t("purchase.rate_hint", { rate: formatDecimal(typeable(rateInForce), locale) }) : undefined}
              inputMode="decimal"
              dir="ltr"
              autoComplete="off"
              required
            />
          ) : null}
        </div>

        <div className="space-y-2">
          <TextField
            label={t("purchase.add_item")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={onSearchKey}
            type="search"
            autoComplete="off"
          />
          {search.trim() !== "" && products !== null ? (
            matches.length === 0 ? (
              <p className="text-sm text-text-muted">{t("purchase.no_match")}</p>
            ) : (
              <ul className="flex flex-wrap gap-2" aria-label={t("purchase.matches")}>
                {matches.map((p) => (
                  <li key={p.id}>
                    <Button onClick={() => add(p)}>
                      {name(p)} — {tDynamic(`uom.${p.unitCode}`)}
                    </Button>
                  </li>
                ))}
              </ul>
            )
          ) : null}
        </div>

        {lines.length === 0 ? <p className="text-sm text-text-muted">{t("purchase.no_lines")}</p> : null}
        {lines.length > 0 ? (
          <div className="overflow-x-auto rounded-md border border-border">
            <table className="w-full text-sm">
              <thead className="bg-surface text-text-muted">
                <tr>
                  <th className="p-2 text-start">{t("purchase.col.item")}</th>
                  <th className="p-2 text-start">{t("purchase.col.quantity")}</th>
                  <th className="p-2 text-start">{t("purchase.col.damaged")}</th>
                  <th className="p-2 text-start">{t("purchase.col.unit_cost")}</th>
                  <th className="p-2 text-start">{t("purchase.col.discount")}</th>
                  <th className="p-2 text-start">{t("purchase.col.due")}</th>
                  <th className="p-2 text-start">{t("purchase.col.net_cost")}</th>
                  <th className="p-2" />
                </tr>
              </thead>
              <tbody>
                {lines.map((l, i) => {
                  const q = quoted(i);
                  const itemName = name(l.product);
                  return (
                    <tr key={l.key} className="border-t border-border align-top" data-testid="purchase-line-input">
                      <td className="p-2">
                        {itemName}
                        <span className="block text-xs text-text-muted">{tDynamic(`uom.${l.product.unitCode}`)}</span>
                        {lineError(i, "productId") ? (
                          <span role="alert" className="block text-xs text-danger">
                            {lineError(i, "productId")}
                          </span>
                        ) : null}
                      </td>
                      <td className="p-2">
                        <CellInput
                          aria-label={t("purchase.quantity_of", { name: itemName })}
                          value={l.quantity}
                          onChange={(e) => change(l.key, { quantity: e.target.value })}
                          error={lineError(i, "quantity")}
                          inputMode="decimal"
                          dir="ltr"
                          autoComplete="off"
                        />
                      </td>
                      <td className="p-2">
                        <CellInput
                          aria-label={t("purchase.damaged_of", { name: itemName })}
                          value={l.damaged}
                          onChange={(e) => change(l.key, { damaged: e.target.value })}
                          error={lineError(i, "damaged")}
                          inputMode="decimal"
                          dir="ltr"
                          autoComplete="off"
                        />
                      </td>
                      <td className="p-2">
                        <CellInput
                          aria-label={t("purchase.unit_cost_of", { name: itemName })}
                          value={l.unitCost}
                          onChange={(e) => change(l.key, { unitCost: e.target.value })}
                          error={lineError(i, "unitCost")}
                          inputMode="decimal"
                          dir="ltr"
                          autoComplete="off"
                        />
                      </td>
                      <td className="p-2">
                        <div className="flex gap-1">
                          <select
                            aria-label={t("purchase.discount_mode_of", { name: itemName })}
                            value={l.discountMode}
                            onChange={(e) => change(l.key, { discountMode: e.target.value as DiscountMode })}
                            className="rounded-md border border-border bg-surface-raised px-1 text-sm"
                          >
                            <option value="percent">{t("purchase.discount_percent")}</option>
                            <option value="amount">{t("purchase.discount_amount")}</option>
                          </select>
                          <CellInput
                            aria-label={t("purchase.discount_of", { name: itemName })}
                            value={l.discount}
                            onChange={(e) => change(l.key, { discount: e.target.value })}
                            error={lineError(i, "discount")}
                            inputMode="decimal"
                            dir="ltr"
                            autoComplete="off"
                          />
                        </div>
                      </td>
                      <td className="p-2" data-testid="line-due">
                        {q ? <Money value={q.due} currency={currency} className="font-medium" /> : null}
                      </td>
                      <td className="p-2" data-testid="line-net-cost">
                        {q && !isZero(q.good) ? <Money value={q.netUnitCost} currency={currency} /> : null}
                      </td>
                      <td className="p-2">
                        <Button onClick={() => remove(l.key)} aria-label={t("purchase.remove_of", { name: itemName })}>
                          {t("purchase.remove")}
                        </Button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : null}
        <p className="text-xs text-text-muted">{t("purchase.damaged_hint")}</p>

        <div className="grid gap-3 sm:grid-cols-2">
          <TextField
            label={t("purchase.invoice_discount", { currency: tDynamic(`currency.${currency}`) })}
            value={invoiceDiscount}
            onChange={(e) => setInvoiceDiscount(e.target.value)}
            error={errors.field("invoiceDiscount")}
            inputMode="decimal"
            dir="ltr"
            autoComplete="off"
          />
          <div className="space-y-1">
            <TextField
              label={t("purchase.paid_now", { currency: tDynamic(`currency.${currency}`) })}
              value={paidNow}
              onChange={(e) => setPaidNow(e.target.value)}
              error={errors.field("paidNow")}
              hint={t("purchase.paid_now_hint")}
              inputMode="decimal"
              dir="ltr"
              autoComplete="off"
            />
            {quote && !isZero(quote.purchase.due) ? <Button onClick={() => setPaidNow(typeable(quote.purchase.due))}>{t("purchase.pay_in_full")}</Button> : null}
          </div>
          {paidNow.trim() !== "" ? <SourceField label={t("suppliers.paid_from")} value={paidFrom} onChange={setPaidFrom} error={errors.field("paidFrom")} /> : null}
          <TextField label={t("purchase.note")} value={note} onChange={(e) => setNote(e.target.value)} error={errors.field("note")} maxLength={200} autoComplete="off" />
        </div>

        {quote ? (
          <div className="flex flex-wrap items-start justify-between gap-4 rounded-md border border-border p-3 text-sm" data-testid="purchase-quote">
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
              <dt className="text-text-muted">{t("purchase.balance_before")}</dt>
              <dd>{balanceText(quote.balanceBefore)}</dd>
              <dt className="font-semibold">{t("purchase.balance_after")}</dt>
              <dd className="font-semibold" data-testid="balance-after">
                {balanceText(quote.balanceAfter)}
              </dd>
            </dl>
            <PurchaseTotals purchase={quote.purchase} />
          </div>
        ) : null}

        {errors.form ? (
          <p role="alert" className="text-sm text-danger">
            {errors.form}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || !ready}>
            {t("purchase.record")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
