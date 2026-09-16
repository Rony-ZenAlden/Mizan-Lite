import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { Link } from "react-router-dom";
import { useClient } from "@/api/ClientContext";
import type {
  CartInput,
  CartQuote,
  Customer,
  Product,
  Sale,
  Unit,
} from "@/api/client";
import { BindingError } from "@/api/envelope";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { useRate } from "@/rates/RateProvider";
import { Money, isZero } from "@/screens/sales/Money";
import { printsItself, usePrinterSettings } from "@/printing/PrintPanel";
import { ReceiptView } from "@/screens/sales/ReceiptView";
import { CustomerPicker } from "@/screens/customers/CustomerPicker";
import { formErrors } from "@/screens/stock/forms";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { CellInput, SelectField, TextField } from "@/ui/Field";
import { addToCart, minusOne, plusOne, type CartEntry, type Pick } from "./cart";
import { ratePair } from "@/i18n/figures";
import { QuantityDialog } from "./QuantityDialog";

/** How long the cart must be still before it is priced again. */
export const QUOTE_DEBOUNCE_MS = 150;

/** How many products a name search offers. */
export const SEARCH_RESULTS = 8;

/** The code a checkout is refused with when the rate or a price moved after the quote (sales/domain.CodeQuoteStale). */
export const CODE_QUOTE_STALE = "lite.sales.quote_stale";

/** The code a quote is refused with when there is no rate (sales/domain.CodeNoRate). */
export const CODE_NO_RATE = "lite.sales.no_rate";

/**
 * The till (L4 §10.2). A scan, a quick button or a search adds a product; a weighed one asks how much first. Every
 * figure — each line in pounds and dollars, the rounding to the smallest note, the change — is Go's quote of the cart as
 * typed, and Pay sends that quote's token, so a sale is never recorded at a total the cashier did not see (L4 §4).
 */
/** The part of a key press the till reads. */
type KeyPress = { key: string; preventDefault: () => void };

export function TillScreen() {
  const client = useClient();
  const { withOwner } = useOwner();
  const { rate } = useRate();
  const { t, tDynamic, errorText, locale } = useLocale();

  const [products, setProducts] = useState<Product[]>([]);
  const [units, setUnits] = useState<Map<string, Unit>>(new Map());
  const [code, setCode] = useState("");
  const [results, setResults] = useState<Product[] | null>(null);
  const [notFound, setNotFound] = useState("");
  const [weigh, setWeigh] = useState<Pick | null>(null);

  const [cart, setCart] = useState<CartEntry[]>([]);
  const [settlement, setSettlement] = useState("");
  const [saleDiscount, setSaleDiscount] = useState("");
  const [tenderCurrency, setTenderCurrency] = useState("");
  const [tendered, setTendered] = useState("");
  const [changeCurrency, setChangeCurrency] = useState("");
  const [payment, setPayment] = useState<"cash" | "credit">("cash");
  const [customer, setCustomer] = useState<Customer | null>(null);
  const [picking, setPicking] = useState(false);
  const [debtCurrency, setDebtCurrency] = useState("USD");

  const [quote, setQuote] = useState<CartQuote | null>(null);
  const [quoteError, setQuoteError] = useState<unknown>(null);
  const [pending, setPending] = useState(false);
  const [requote, setRequote] = useState(0);
  const [stale, setStale] = useState(false);
  const [payError, setPayError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [receipt, setReceipt] = useState<Sale | null>(null);
  const printer = usePrinterSettings();
  const [loadError, setLoadError] = useState<unknown>(null);

  const scanField = useRef<HTMLInputElement>(null);
  const focusScan = () => scanField.current?.focus();

  useEffect(() => {
    let cancelled = false;
    Promise.all([
      client.catalog.products({ text: "", includeInactive: false }),
      client.catalog.units(),
    ])
      .then(([p, u]) => {
        if (cancelled) return;
        setProducts(p);
        setUnits(new Map(u.map((unit) => [unit.code, unit])));
      })
      .catch((e) => {
        if (!cancelled) setLoadError(e);
      });
    return () => {
      cancelled = true;
    };
  }, [client]);

  // The currency a credit sale is charged in by default (Q-L5.1): read once; the cashier switches per sale.
  useEffect(() => {
    let cancelled = false;
    client.settings
      .get()
      .then((s) => {
        if (!cancelled && s.debtCurrency) setDebtCurrency(s.debtCurrency);
      })
      .catch(() => {
        // Dollars, the default, stand if settings cannot be read.
      });
    return () => {
      cancelled = true;
    };
  }, [client]);

  const input: CartInput = useMemo(
    () => ({
      lines: cart.map((l) => ({
        productId: l.productId,
        quantity: l.quantity,
        discountPercent: l.discountPercent,
      })),
      settlement,
      saleDiscount,
      tenderCurrency,
      tendered,
      changeCurrency: payment === "credit" ? "" : changeCurrency,
      payment,
      customerId: payment === "credit" ? (customer?.id ?? "") : "",
    }),
    [
      cart,
      settlement,
      saleDiscount,
      tenderCurrency,
      tendered,
      changeCurrency,
      payment,
      customer,
    ],
  );

  // Price the cart as typed whenever it settles. A newer request wins; an answer to an older one is dropped.
  const request = useRef(0);
  useEffect(() => {
    const id = ++request.current;
    if (input.lines.length === 0) {
      setQuote(null);
      setQuoteError(null);
      setPending(false);
      return;
    }
    setPending(true);
    const timer = setTimeout(() => {
      client.till
        .quote(input)
        .then((q) => {
          if (id !== request.current) return;
          setQuote(q);
          setQuoteError(null);
        })
        .catch((e) => {
          if (id !== request.current) return;
          setQuote(null);
          setQuoteError(e);
        })
        .finally(() => {
          if (id === request.current) setPending(false);
        });
    }, QUOTE_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [client, input, requote]);

  const name = (p: { nameAr: string; nameEn: string }) =>
    locale === "en" && p.nameEn ? p.nameEn : p.nameAr;

  const add = (pick: Pick) => {
    setResults(null);
    setNotFound("");
    if (pick.unitDecimals > 0) {
      setWeigh(pick);
      return;
    }
    setCart((c) => addToCart(c, pick, "1"));
    setStale(false);
    focusScan();
  };

  const pickOf = (p: Product): Pick => ({
    productId: p.id,
    nameAr: p.nameAr,
    nameEn: p.nameEn,
    unitCode: p.unitCode,
    unitDecimals: units.get(p.unitCode)?.inputDecimals ?? 0,
  });

  const scan = async (event: FormEvent) => {
    event.preventDefault();
    const typed = code.trim();
    if (typed === "") {
      // Enter on an empty scan field pays (L8 D-L8.9): the cashier's hands never leave the keyboard.
      requestPay();
      return;
    }
    setCode("");
    try {
      const found = await client.till.scan(typed);
      if (found.found && found.active) {
        add(found);
        return;
      }
      const matches = await client.catalog.products({
        text: typed,
        includeInactive: false,
      });
      if (matches.length === 0) {
        setResults(null);
        setNotFound(typed);
      } else {
        setNotFound("");
        setResults(matches.slice(0, SEARCH_RESULTS));
      }
    } catch (e) {
      setLoadError(e);
    }
    focusScan();
  };

  const update = (key: string, change: Partial<CartEntry>) => {
    setCart((c) => c.map((l) => (l.key === key ? { ...l, ...change } : l)));
    setStale(false);
  };

  const remove = (key: string) => {
    setCart((c) => c.filter((l) => l.key !== key));
    setStale(false);
    focusScan();
  };

  const resetPayment = () => {
    setPayment("cash");
    setCustomer(null);
    setSettlement("");
  };

  const clear = () => {
    resetPayment();
    setCart([]);
    setSaleDiscount("");
    setTendered("");
    setTenderCurrency("");
    setChangeCurrency("");
    setStale(false);
    setPayError(null);
    focusScan();
  };

  const pay = useCallback(
    async (event?: FormEvent) => {
      event?.preventDefault();
      if (!quote || pending || busy) return;
      setBusy(true);
      setPayError(null);
      try {
        const sale = await withOwner(() =>
          client.till.checkout({ cart: input, token: quote.token }),
        );
        setReceipt(sale);
        resetPayment();
        setCart([]);
        setSaleDiscount("");
        setTendered("");
        setTenderCurrency("");
        setChangeCurrency("");
        setStale(false);
      } catch (e) {
        if (e instanceof BindingError && e.code === CODE_QUOTE_STALE) {
          setStale(true);
          setRequote((n) => n + 1);
        } else if (!(e instanceof OwnerCancelled)) {
          setPayError(e);
        }
      } finally {
        setBusy(false);
      }
    },
    [quote, pending, busy, withOwner, client, input],
  );

  const quick = useMemo(
    () =>
      products
        .filter((p) => p.quickSlot > 0)
        .sort((a, b) => a.quickSlot - b.quickSlot),
    [products],
  );
  const local = rate?.localCurrency || quote?.localCurrency || "";
  const noRate = rate !== null && !rate.set;
  const errors = formErrors(quoteError, errorText);
  const lineError = (index: number, field: string) =>
    errors.field(`lines.${index + 1}.${field}`);
  const formProblem =
    quoteError instanceof BindingError && quoteError.code === CODE_NO_RATE
      ? null
      : errors.form;
  const other = quote ? quote.otherCurrency : "";
  const credit = payment === "credit";
  const canPay =
    Boolean(quote) &&
    !pending &&
    !busy &&
    !noRate &&
    cart.length > 0 &&
    !(credit && (!customer || quote?.needsCustomer));

  // A pay key pressed while the cart is still being priced pays as soon as Go's quote arrives — found by the end-to-end journey
  // (J3): a cashier's F9 straight after a scan did nothing, silently. Anything that changes the cart first cancels it.
  const [payWhenPriced, setPayWhenPriced] = useState(false);
  const requestPay = () => {
    if (canPay) void pay();
    else if (cart.length > 0 && pending && !busy) setPayWhenPriced(true);
  };
  useEffect(() => {
    if (!payWhenPriced || pending) return;
    setPayWhenPriced(false);
    if (canPay) void pay();
  }, [payWhenPriced, pending, canPay, pay]);
  useEffect(() => {
    setPayWhenPriced(false);
  }, [cart, payment, settlement, tendered, tenderCurrency, saleDiscount]);

  // The counter's keys (L8 D-L8.9): F9 pays, F4 switches cash and credit, F2 goes to the last line's quantity, + and − on an empty
  // scan field change a counted last line by one, Escape clears search results. Not while a dialog is open — it has the keyboard.
  const keys = useRef<(event: KeyPress) => void>(() => {});
  keys.current = (event: KeyPress) => {
    if (receipt || picking || weigh) return;
    const last = cart[cart.length - 1];
    const inScan = document.activeElement === scanField.current;
    if (event.key === "F9") {
      event.preventDefault();
      requestPay();
    } else if (event.key === "F4") {
      event.preventDefault();
      choosePayment(credit ? "cash" : "credit");
    } else if (event.key === "F2" && last) {
      event.preventDefault();
      const field = document.querySelector<HTMLInputElement>(`[data-quantity-of="${last.key}"]`);
      field?.focus();
      field?.select();
    } else if ((event.key === "+" || event.key === "-") && inScan && code === "" && last && last.unitDecimals === 0) {
      event.preventDefault();
      const next = event.key === "+" ? plusOne(last.quantity) : minusOne(last.quantity);
      if (next !== null) update(last.key, { quantity: next });
    } else if (event.key === "Escape" && (results || notFound)) {
      setResults(null);
      setNotFound("");
      focusScan();
    }
  };
  useEffect(() => {
    const listener = (event: KeyPress) => keys.current(event);
    window.addEventListener("keydown", listener);
    return () => window.removeEventListener("keydown", listener);
  }, []);

  const choosePayment = (next: "cash" | "credit") => {
    setPayment(next);
    setStale(false);
    if (next === "credit") {
      // On credit the sale is charged in the default debt currency (Q-L5.1), one tap from the other.
      setSettlement(debtCurrency === local ? "" : debtCurrency);
      if (!customer) setPicking(true);
    }
  };

  return (
    <section className="space-y-4">
      <header className="flex items-center justify-between gap-4">
        <h2 className="text-xl font-semibold">{t("till.title")}</h2>
        {cart.length > 0 ? (
          <Button onClick={clear}>{t("till.clear")}</Button>
        ) : null}
      </header>

      {noRate ? (
        <Alert tone="danger" title={t("till.no_rate")}>
          <Link to="/rates" className="underline">
            {t("till.no_rate_link")}
          </Link>
        </Alert>
      ) : null}
      {loadError ? <Alert tone="danger" title={errorText(loadError)} /> : null}

      <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
        <div className="space-y-4">
          <form onSubmit={(e) => void scan(e)}>
            <TextField
              ref={scanField}
              label={t("till.scan")}
              hint={t("till.scan_hint")}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              autoComplete="off"
              autoFocus
            />
          </form>
          <p className="text-xs text-text-muted" data-testid="till-keys">
            {t("till.keys")}
          </p>
          {notFound ? (
            <p role="status" className="text-sm text-danger">
              {t("till.not_found", { code: notFound })}
            </p>
          ) : null}
          {results ? (
            <ul aria-label={t("till.results")} className="space-y-1">
              {results.map((p) => (
                <li key={p.id}>
                  <Button
                    className="w-full justify-start"
                    onClick={() => add(pickOf(p))}
                  >
                    {name(p)}
                  </Button>
                </li>
              ))}
            </ul>
          ) : null}

          {quick.length > 0 ? (
            <div
              role="group"
              aria-label={t("till.quick")}
              className="grid grid-cols-3 gap-2 xl:grid-cols-4"
            >
              {quick.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => add(pickOf(p))}
                  className="min-h-16 rounded-md border border-border bg-surface-raised p-2 text-sm font-medium hover:bg-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {name(p)}
                </button>
              ))}
            </div>
          ) : (
            <p className="text-sm text-text-muted">{t("till.quick_empty")}</p>
          )}
        </div>

        <div className="space-y-4">
          {cart.length === 0 ? (
            <p className="rounded-md border border-dashed border-border p-6 text-center text-text-muted">
              {t("till.empty")}
            </p>
          ) : (
            <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
              <table className="w-full text-sm">
                <thead className="bg-surface text-text-muted">
                  <tr>
                    <th className="p-2 text-start">{t("till.col.product")}</th>
                    <th className="p-2 text-start">{t("till.col.quantity")}</th>
                    <th className="p-2 text-start">{t("till.col.discount")}</th>
                    <th className="p-2 text-start">{t("till.col.line")}</th>
                    <th className="p-2 text-start">
                      <span className="sr-only">{t("till.col.remove")}</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {cart.map((l, index) => {
                    const priced = quote?.lines[index];
                    const productProblem = lineError(index, "productId");
                    return (
                      <tr
                        key={l.key}
                        className="border-t border-border align-top"
                        data-testid="cart-line"
                      >
                        <td className="p-2">
                          <p className="font-medium">{name(l)}</p>
                          {priced ? (
                            <p className="text-xs text-text-muted">
                              <bdi dir="ltr">
                                {formatDecimal(priced.unitPrice, locale)}{" "}
                                {tDynamic(
                                  `currency.short.${priced.priceCurrency}`,
                                )}{" "}
                                / {tDynamic(`uom.${l.unitCode}`)}
                              </bdi>
                            </p>
                          ) : null}
                          {productProblem ? (
                            <p role="alert" className="text-xs text-danger">
                              {productProblem}
                            </p>
                          ) : null}
                          {priced?.warnings.map((w) => (
                            <p key={w} className="text-xs text-danger">
                              {tDynamic(`till.warning.${w}`, {
                                onHand: formatDecimal(priced.onHand, locale),
                                unit: tDynamic(`uom.${l.unitCode}`),
                              })}
                            </p>
                          ))}
                        </td>
                        <td className="w-28 p-2">
                          <CellInput
                            aria-label={t("till.quantity_of", {
                              name: name(l),
                            })}
                            value={l.quantity}
                            data-quantity-of={l.key}
                            onChange={(e) =>
                              update(l.key, { quantity: e.target.value })
                            }
                            error={lineError(index, "quantity")}
                            inputMode="decimal"
                            dir="ltr"
                            autoComplete="off"
                          />
                        </td>
                        <td className="w-24 p-2">
                          <CellInput
                            aria-label={t("till.discount_of", {
                              name: name(l),
                            })}
                            value={l.discountPercent}
                            onChange={(e) =>
                              update(l.key, { discountPercent: e.target.value })
                            }
                            error={lineError(index, "discountPercent")}
                            inputMode="decimal"
                            dir="ltr"
                            autoComplete="off"
                          />
                        </td>
                        <td className="p-2">
                          {priced ? (
                            <>
                              <p>
                                <Money
                                  value={priced.netLocal}
                                  currency={quote!.localCurrency}
                                />
                              </p>
                              <p className="text-xs text-text-muted">
                                <Money value={priced.netUsd} currency="USD" />
                              </p>
                            </>
                          ) : null}
                        </td>
                        <td className="p-2">
                          <Button
                            onClick={() => remove(l.key)}
                            aria-label={t("till.remove", { name: name(l) })}
                          >
                            ×
                          </Button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}

          {stale ? <Alert tone="danger" title={t("till.stale")} /> : null}
          {formProblem ? <Alert tone="danger" title={formProblem} /> : null}
          {payError ? (
            <Alert tone="danger" title={errorText(payError)} />
          ) : null}

          <form
            className="space-y-3 rounded-lg border border-border bg-surface-raised p-4"
            onSubmit={(e) => void pay(e)}
          >
            <div
              role="group"
              aria-label={t("till.payment")}
              className="flex flex-wrap gap-2"
            >
              {(["cash", "credit"] as const).map((kind) => (
                <button
                  key={kind}
                  type="button"
                  aria-pressed={payment === kind}
                  onClick={() => choosePayment(kind)}
                  className={`rounded-md px-3 py-1 text-sm ${payment === kind ? "bg-primary text-primary-fg" : "border border-border hover:bg-surface"}`}
                >
                  {kind === "cash" ? t("till.pay_cash") : t("till.pay_credit")}
                </button>
              ))}
            </div>
            {credit ? (
              <div
                data-testid="till-customer"
                className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border p-2 text-sm"
              >
                {customer ? (
                  <span>{t("till.credit_to", { name: customer.name })}</span>
                ) : (
                  <span className="text-danger">
                    {t("till.choose_customer")}
                  </span>
                )}
                <Button onClick={() => setPicking(true)}>
                  {customer
                    ? t("till.change_customer")
                    : t("till.pick_customer")}
                </Button>
                {errors.field("customerId") ? (
                  <p role="alert" className="w-full text-xs text-danger">
                    {errors.field("customerId")}
                  </p>
                ) : null}
              </div>
            ) : null}
            <div
              role="group"
              aria-label={t("till.settlement")}
              className="flex flex-wrap gap-2"
            >
              {[local, "USD"].filter(Boolean).map((currency) => {
                const chosen = (settlement || local) === currency;
                return (
                  <button
                    key={currency}
                    type="button"
                    aria-pressed={chosen}
                    onClick={() => {
                      setSettlement(currency === local ? "" : currency);
                      setStale(false);
                    }}
                    className={`rounded-md px-3 py-1 text-sm ${chosen ? "bg-primary text-primary-fg" : "border border-border hover:bg-surface"}`}
                  >
                    {t("till.settle_in", {
                      currency: tDynamic(`currency.${currency}`),
                    })}
                  </button>
                );
              })}
            </div>

            <div
              data-testid="till-total"
              aria-live="polite"
              className="space-y-1"
            >
              {quote ? (
                <>
                  <p className="text-3xl font-semibold">
                    <Money value={quote.total} currency={quote.settlement} />
                  </p>
                  <p className="text-sm text-text-muted">
                    {t("till.total_other")}{" "}
                    <Money value={quote.totalOther} currency={other} />
                  </p>
                  {!isZero(quote.rounding) ? (
                    <p className="text-sm text-text-muted">
                      {t("till.rounding", {
                        note: formatDecimal(quote.cashNote, locale),
                      })}{" "}
                      <Money
                        value={quote.rounding}
                        currency={quote.settlement}
                      />
                    </p>
                  ) : null}
                  {quote.discounted ? (
                    <p className="text-sm text-danger">
                      {t("till.discount_needs_owner")}
                    </p>
                  ) : null}
                  {quote.payment === "credit" && quote.debt ? (
                    <dl
                      data-testid="till-credit"
                      className="grid grid-cols-[1fr_auto] gap-x-3 text-sm"
                    >
                      <dt>{t("till.debt_added")}</dt>
                      <dd>
                        <Money
                          value={quote.debt}
                          currency={quote.settlement}
                          className="font-semibold"
                        />
                      </dd>
                      {!quote.needsCustomer ? (
                        <>
                          <dt>{t("till.balance_now")}</dt>
                          <dd>
                            <Money
                              value={quote.balanceBefore}
                              currency={quote.settlement}
                            />
                          </dd>
                          <dt>{t("till.balance_after")}</dt>
                          <dd>
                            <Money
                              value={quote.balanceAfter}
                              currency={quote.settlement}
                              className="font-semibold"
                            />
                          </dd>
                        </>
                      ) : null}
                    </dl>
                  ) : null}
                  <p
                    className={`text-xs ${quote.rateStale ? "text-danger" : "text-text-muted"}`}
                  >
                    {t("till.rate", ratePair(quote.rate, quote.localCurrency, locale, tDynamic))}
                    {quote.rateStale ? ` · ${t("header.rate_stale")}` : ""}
                  </p>
                </>
              ) : (
                <p className="text-3xl font-semibold text-text-muted">—</p>
              )}
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <TextField
                label={t("till.sale_discount", {
                  currency: tDynamic(`currency.${settlement || local}`),
                })}
                value={saleDiscount}
                onChange={(e) => setSaleDiscount(e.target.value)}
                error={errors.field("saleDiscount")}
                inputMode="decimal"
                dir="ltr"
                autoComplete="off"
              />
              <SelectField
                label={t("till.tender_currency")}
                value={tenderCurrency}
                onChange={(e) => setTenderCurrency(e.target.value)}
              >
                <option value="">{t("till.same_as_total")}</option>
                {[local, "USD"].filter(Boolean).map((c) => (
                  <option key={c} value={c}>
                    {tDynamic(`currency.${c}`)}
                  </option>
                ))}
              </SelectField>
              <TextField
                label={credit ? t("till.paid_now") : t("till.tendered")}
                hint={
                  credit ? t("till.paid_now_hint") : t("till.tendered_hint")
                }
                value={tendered}
                onChange={(e) => setTendered(e.target.value)}
                error={errors.field("tendered")}
                inputMode="decimal"
                dir="ltr"
                autoComplete="off"
              />
              {credit ? null : (
                <SelectField
                  label={t("till.change_currency")}
                  value={changeCurrency}
                  onChange={(e) => setChangeCurrency(e.target.value)}
                  error={errors.field("changeCurrency")}
                  hint={t("till.change_default_hint")}
                >
                  <option value="">{t("till.change_default")}</option>
                  {[local, "USD"].filter(Boolean).map((c) => (
                    <option key={c} value={c}>
                      {tDynamic(`currency.${c}`)}
                    </option>
                  ))}
                </SelectField>
              )}
            </div>

            {quote && quote.tenderGiven && !credit ? (
              <p data-testid="till-change" className="text-xl font-semibold">
                {t("till.change")}{" "}
                <Money value={quote.change} currency={quote.changeCurrency} />
              </p>
            ) : null}

            <Button
              variant="primary"
              type="submit"
              className="w-full min-h-12 text-lg"
              disabled={!canPay}
            >
              {t("till.pay")}
            </Button>
          </form>
        </div>
      </div>

      {weigh ? (
        <QuantityDialog
          name={name(weigh)}
          unitCode={weigh.unitCode}
          decimals={weigh.unitDecimals}
          onAdd={(quantity) => {
            setCart((c) => addToCart(c, weigh, quantity));
            setWeigh(null);
            setStale(false);
            setTimeout(focusScan, 0);
          }}
          onClose={() => {
            setWeigh(null);
            setTimeout(focusScan, 0);
          }}
        />
      ) : null}

      {picking ? (
        <CustomerPicker
          onPick={(c) => {
            setCustomer(c);
            setPicking(false);
            setStale(false);
          }}
          onClose={() => setPicking(false)}
        />
      ) : null}

      {receipt ? (
        <ReceiptView
          sale={receipt}
          autoPrint={printsItself(printer, receipt.payment === "credit" ? "credit_sale" : "cash_sale")}
          onClose={() => {
            setReceipt(null);
            setTimeout(focusScan, 0);
          }}
          onVoided={setReceipt}
        />
      ) : null}
    </section>
  );
}
