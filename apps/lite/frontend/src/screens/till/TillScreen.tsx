import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { Link } from "react-router-dom";
import { useOptionalNotifications } from "@/alerts/NotificationProvider";
import { useClient } from "@/api/ClientContext";
import type {
  CartInput,
  CartQuote,
  Customer,
  Product,
  Sale,
  SaleReturn,
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
import { dropHeld, forgetCart, heldCarts, holdCart, keepCart, recoverCart, takeHeld, MAX_HELD, type HeldCart } from "./openCart";
import { OpenItemDialog } from "./OpenItemDialog";
import { ReturnWizard } from "./ReturnWizard";
import { QuickRepayment } from "./QuickRepayment";
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
  const notifications = useOptionalNotifications();

  const [products, setProducts] = useState<Product[]>([]);
  const [units, setUnits] = useState<Map<string, Unit>>(new Map());
  const [code, setCode] = useState("");
  const [results, setResults] = useState<Product[] | null>(null);
  const [notFound, setNotFound] = useState("");
  const [weigh, setWeigh] = useState<Pick | null>(null);

  // The cart a restart or a power cut interrupted comes back (2026-09-20). Only what was picked and how much of it:
  // Go prices it again, because a restored total at yesterday's rate would be a lie about what the customer owes.
  const [cart, setCart] = useState<CartEntry[]>(() => recoverCart() ?? []);
  const [recovered, setRecovered] = useState(() => recoverCart() !== null);
  const [settlement, setSettlement] = useState("");
  const [saleDiscount, setSaleDiscount] = useState("");
  const [tenderCurrency, setTenderCurrency] = useState("");
  const [tendered, setTendered] = useState("");
  const [changeCurrency, setChangeCurrency] = useState("");
  const [payment, setPayment] = useState<"cash" | "credit">("cash");
  // Quick pay is the whole sale in the settlement currency with no change: the fields that would say otherwise stay shut until
  // the cashier opens them, and closing them puts every one back to that default, so nothing hidden can price a sale.
  const [details, setDetails] = useState(false);
  const [customer, setCustomer] = useState<Customer | null>(null);
  const [picking, setPicking] = useState(false);
  const [debtCurrency, setDebtCurrency] = useState("USD");

  const [quote, setQuote] = useState<CartQuote | null>(null);
  const [quoteError, setQuoteError] = useState<unknown>(null);
  // Which cart the quote (or the refusal) on screen answers: the cart as typed and the re-pricing it was asked for.
  const [answered, setAnswered] = useState<{ input: CartInput; requote: number } | null>(null);
  const [requote, setRequote] = useState(0);
  const [stale, setStale] = useState(false);
  const [payError, setPayError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [receipt, setReceipt] = useState<Sale | null>(null);
  // The sale just recorded, when no receipt panel opened: a line of confirmation above the scan field, which the next
  // scan clears. A cashier needs to know the sale went through; they do not need to dismiss anything to sell again.
  const [done, setDone] = useState<Sale | null>(null);
  // F8 opens the return wizard (2026-09-20): the customer is at the counter with the paper in hand.
  const [returning, setReturning] = useState(false);
  // An open-priced item waiting for its price (2026-09-23).
  const [pricing, setPricing] = useState<Pick | null>(null);
  // Carts put aside while the next customer is served (2026-09-23).
  const [held, setHeld] = useState<HeldCart[]>(() => heldCarts());
  // A customer settling what they owe, without leaving the till (2026-09-20).
  const [repaying, setRepaying] = useState(false);
  const [repaid, setRepaid] = useState(false);
  const [returned, setReturned] = useState<SaleReturn | null>(null);
  const printer = usePrinterSettings();
  const [loadError, setLoadError] = useState<unknown>(null);

  // Kept on every change, forgotten the moment the sale is recorded.
  useEffect(() => {
    keepCart(cart);
  }, [cart]);

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
        price: l.price,
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
  //
  // The cart is being priced from the very render it changes in until Go answers for THAT cart. It is derived, not set by
  // an effect: an effect runs after the render, and a key pressed in between saw a cart with no quote that was not being
  // priced either, and F9 did nothing, silently — J12 pressed it two milliseconds after a line was added (2026-09-23).
  const beingPriced = input.lines.length > 0 && (answered?.input !== input || answered.requote !== requote);
  const request = useRef(0);
  useEffect(() => {
    const id = ++request.current;
    if (input.lines.length === 0) {
      setQuote(null);
      setQuoteError(null);
      return;
    }
    const timer = setTimeout(() => {
      client.till
        .quote(input)
        .then((q) => {
          if (id !== request.current) return;
          setQuote(q);
          setQuoteError(null);
          setAnswered({ input, requote });
        })
        .catch((e) => {
          if (id !== request.current) return;
          setQuote(null);
          setQuoteError(e);
          setAnswered({ input, requote });
        });
    }, QUOTE_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [client, input, requote]);

  const name = (p: { nameAr: string; nameEn: string }) =>
    locale === "en" && p.nameEn ? p.nameEn : p.nameAr;

  const add = (pick: Pick) => {
    setResults(null);
    setNotFound("");
    if (pick.openPrice) {
      setPricing(pick);
      return;
    }
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
    openPrice: p.openPrice,
    priceCurrency: p.priceCurrency,
  });

  // The Misc button (2026-09-23): the shop's open-priced item, created the first time it is asked for.
  const openMisc = async () => {
    try {
      add(pickOf(await client.till.openItem()));
    } catch (e) {
      setLoadError(e);
    }
  };

  // Hold and resume (2026-09-23). Only what was picked is kept, never a price: a held cart is priced again when it comes
  // back, because the rate may have moved while it waited.
  const hold = () => {
    if (cart.length === 0) return;
    const put = holdCart({
      lines: cart,
      saleDiscount,
      payment: credit ? "credit" : "cash",
      customer: credit ? customer : null,
    });
    if (!put) return;
    setHeld(heldCarts());
    setCart([]);
    setSaleDiscount("");
    resetPayment();
    setDone(null);
    focusScan();
  };

  const resume = (id: string) => {
    // A cart already open is put aside first, so resuming never throws away what the cashier was doing.
    if (cart.length > 0) {
      holdCart({
        lines: cart,
        saleDiscount,
        payment: credit ? "credit" : "cash",
        customer: credit ? customer : null,
      });
    }
    const back = takeHeld(id);
    setHeld(heldCarts());
    if (!back) return;
    setCart(back.lines);
    setSaleDiscount(back.saleDiscount);
    if (back.payment === "credit" && back.customer) {
      choosePayment("credit");
      setCustomer(back.customer);
    } else {
      choosePayment("cash");
    }
    setStale(false);
    focusScan();
  };

  const drop = (id: string) => {
    dropHeld(id);
    setHeld(heldCarts());
    focusScan();
  };

  const scan = async (event: FormEvent) => {
    setDone(null);
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
    setDetails(false); // the next sale starts at quick pay again, whatever this one needed
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
      if (!quote || beingPriced || busy) return;
      setBusy(true);
      setPayError(null);
      try {
        const sale = await withOwner(() =>
          client.till.checkout({ cart: input, token: quote.token }),
        );
        // A shop with no printer has nothing to do with a receipt panel: it would stand between the cashier and the
        // next customer, waiting to be dismissed (the owner's report, 2026-09-20). The sale is recorded either way;
        // where a printer is set up the panel still opens, because that is where the paper comes from.
        if (printsItself(printer, sale.payment === "credit" ? "credit_sale" : "cash_sale") || (printer && printer.printer !== "")) {
          setReceipt(sale);
        } else {
          setDone(sale);
          setTimeout(focusScan, 0);
        }
        forgetCart();
        setRecovered(false);
        resetPayment();
        setCart([]);
        setSaleDiscount("");
        setTendered("");
        setTenderCurrency("");
        setChangeCurrency("");
        setStale(false);
        // A sale can take a product below its reorder level: the bell hears it now, not in a minute (2026-09-23).
        void notifications?.refresh();
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
    [quote, beingPriced, busy, withOwner, client, input, notifications, printer],
  );

  // Every product the shop sells, browsable without a search (the owner's request, 2026-09-16): the quick buttons the
  // owner pinned come first, in their slot order, then everything else by name. The grid scrolls; the cart beside it does
  // not move.
  const grid = useMemo(() => {
    const pinned = products.filter((p) => p.quickSlot > 0).sort((a, b) => a.quickSlot - b.quickSlot);
    const rest = products
      .filter((p) => p.quickSlot === 0)
      .sort((a, b) => (locale === "ar" ? a.nameAr : a.nameEn || a.nameAr).localeCompare(locale === "ar" ? b.nameAr : b.nameEn || b.nameAr, locale));
    return { pinned, all: [...pinned, ...rest] };
  }, [products, locale]);
  const local = rate?.localCurrency || quote?.localCurrency || "";
  // A dollars-only shop sells, takes and gives change in dollars, and reads no rate (0.10.0): Go refuses the rest.
  const usdOnly = rate?.usdOnly === true;
  const home = usdOnly ? "USD" : local;
  const offered = usdOnly ? ["USD"] : [local, "USD"].filter(Boolean);
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
    !beingPriced &&
    !busy &&
    !noRate &&
    cart.length > 0 &&
    !(credit && (!customer || quote?.needsCustomer));

  // A pay key pressed while the cart is still being priced pays as soon as Go's quote arrives — found by the end-to-end journey
  // (J3): a cashier's F9 straight after a scan did nothing, silently. Anything that changes the cart first cancels it.
  //
  // The request names the cart it was made for, so a change voids it by being a different cart. It used to be cancelled by
  // an effect on the cart, and an effect that ran late cancelled a request made AFTER the change it was reacting to (J12).
  const [payFor, setPayFor] = useState<CartInput | null>(null);
  const requestPay = () => {
    if (canPay) void pay();
    else if (beingPriced && !busy) setPayFor(input);
  };
  useEffect(() => {
    if (payFor === null || beingPriced) return;
    setPayFor(null);
    if (payFor === input && canPay) void pay();
  }, [payFor, input, beingPriced, canPay, pay]);

  // The counter's keys (L8 D-L8.9): F9 pays, F4 switches cash and credit, F2 goes to the last line's quantity, + and − on an empty
  // scan field change a counted last line by one, Escape clears search results. Not while a dialog is open — it has the keyboard.
  const keys = useRef<(event: KeyPress) => void>(() => {});
  keys.current = (event: KeyPress) => {
    if (receipt || picking || weigh || returning || repaying || pricing) return;
    const last = cart[cart.length - 1];
    const inScan = document.activeElement === scanField.current;
    if (event.key === "F9") {
      event.preventDefault();
      requestPay();
    } else if (event.key === "F6") {
      event.preventDefault();
      hold();
    } else if (event.key === "F8") {
      event.preventDefault();
      setReturning(true);
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

  const showDetails = (open: boolean) => {
    setDetails(open);
    if (!open) {
      setSaleDiscount("");
      setTenderCurrency("");
      setTendered("");
      setChangeCurrency("");
    }
    setStale(false);
  };

  const choosePayment = (next: "cash" | "credit") => {
    setPayment(next);
    setStale(false);
    if (next === "credit") {
      // On credit the sale is charged in the default debt currency (Q-L5.1), one tap from the other.
      setSettlement(usdOnly || debtCurrency === local ? "" : debtCurrency);
      if (!customer) setPicking(true);
      setDetails(true); // choosing credit IS the explicit choice: "paid now" is the field the cashier came for
    } else {
      showDetails(false);
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
      {recovered && cart.length > 0 ? (
        <Alert tone="success" title={t("till.cart_recovered")} testId="cart-recovered" />
      ) : null}
      {returned ? (
        <Alert
          tone="success"
          title={t("returns.recorded", { number: returned.returnNo, amount: returned.refund })}
          testId="return-recorded"
        />
      ) : null}
      {done ? (
        <Alert tone="success" title={t("till.sale_done", { number: String(done.receiptNo), total: done.total })} testId="sale-done">
          <button type="button" className="underline" onClick={() => setReceipt(done)}>
            {t("till.sale_done_receipt")}
          </button>
        </Alert>
      ) : null}

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
          <div className="flex flex-wrap gap-2">
            <Button onClick={() => void openMisc()} data-testid="till-misc">
              {t("till.misc")}
            </Button>
            <Button onClick={hold} disabled={cart.length === 0 || held.length >= MAX_HELD} data-testid="till-hold">
              {t("till.hold")}
            </Button>
            <Button onClick={() => setReturning(true)} data-testid="till-return">
              {t("returns.title")}
            </Button>
            <Button onClick={() => setRepaying(true)} data-testid="till-repay">
              {t("till.repay")}
            </Button>
          </div>
          {repaid ? <Alert tone="success" title={t("till.repaid")} testId="till-repaid" /> : null}
          {held.length > 0 ? (
            <div className="flex flex-wrap gap-2" data-testid="held-carts" aria-label={t("till.held_label")}>
              {held.map((h, i) => (
                <span key={h.id} className="flex items-center gap-1 rounded-md border border-border px-2 py-1 text-sm" data-testid="held-cart">
                  <button type="button" className="underline" onClick={() => resume(h.id)}>
                    {t("till.held_item", {
                      number: String(i + 1),
                      count: String(h.lines.length),
                      time: new Date(h.at).toLocaleTimeString(locale === "ar" ? "ar-u-nu-latn" : "en", {
                        hour: "2-digit",
                        minute: "2-digit",
                        hour12: false,
                      }),
                    })}
                    {h.customer ? ` · ${h.customer.name}` : ""}
                  </button>
                  <button type="button" aria-label={t("till.held_drop", { number: String(i + 1) })} onClick={() => drop(h.id)}>
                    ×
                  </button>
                </span>
              ))}
            </div>
          ) : null}
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

          {grid.all.length > 0 ? (
            <>
              <p className="text-xs text-text-muted">{t("till.quick_first")}</p>
              <div
                role="group"
                aria-label={t("till.all_products")}
                data-testid="till-products"
                className="grid max-h-[26rem] grid-cols-2 gap-2 overflow-y-auto overscroll-contain pe-1 sm:grid-cols-3 xl:grid-cols-4"
              >
                {grid.all.map((p, i) => (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => add(pickOf(p))}
                    data-quick={i < grid.pinned.length ? "true" : undefined}
                    className={`min-h-16 rounded-md border p-2 text-sm font-medium hover:bg-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                      i < grid.pinned.length ? "border-primary/40 bg-surface-raised" : "border-border bg-surface-raised/60"
                    }`}
                  >
                    {name(p)}
                  </button>
                ))}
              </div>
            </>
          ) : (
            <p className="text-sm text-text-muted">{t("till.products_empty")}</p>
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
                        <td className="p-2" data-testid="cart-line-total">
                          {priced && usdOnly ? (
                            // A dollars-only shop reads each line in dollars alone: the pounds beside them were the
                            // owner's report of 2026-09-24 (0.10.1).
                            <p>
                              <Money value={priced.netUsd} currency="USD" />
                            </p>
                          ) : priced ? (
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
            {offered.length > 1 ? (
            <div
              role="group"
              aria-label={t("till.settlement")}
              className="flex flex-wrap gap-2"
            >
              {offered.map((currency) => {
                const chosen = (settlement || home) === currency;
                return (
                  <button
                    key={currency}
                    type="button"
                    aria-pressed={chosen}
                    onClick={() => {
                      setSettlement(currency === home ? "" : currency);
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
            ) : null}

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
                  {usdOnly ? null : (
                    <p className="text-sm text-text-muted">
                      {t("till.total_other")}{" "}
                      <Money value={quote.totalOther} currency={other} />
                    </p>
                  )}
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
                  {usdOnly ? null : (
                    <p
                      className={`text-xs ${quote.rateStale ? "text-danger" : "text-text-muted"}`}
                    >
                      {t("till.rate", ratePair(quote.rate, quote.localCurrency, locale, tDynamic))}
                      {quote.rateStale ? ` · ${t("header.rate_stale")}` : ""}
                    </p>
                  )}
                </>
              ) : (
                <p className="text-3xl font-semibold text-text-muted">—</p>
              )}
            </div>

            <p className="text-sm text-text-muted" data-testid="till-quick-pay">
              {t("till.quick_pay")}
            </p>

            <button
              type="button"
              aria-expanded={details}
              onClick={() => showDetails(!details)}
              className="text-sm text-primary underline"
              data-testid="till-details-toggle"
            >
              {details ? t("till.details_hide") : t("till.details_show")}
            </button>

            {details ? (
              <div className="grid gap-3 sm:grid-cols-2" data-testid="till-details">
                <TextField
                  label={t("till.sale_discount", {
                    currency: tDynamic(`currency.${settlement || home}`),
                  })}
                  value={saleDiscount}
                  onChange={(e) => setSaleDiscount(e.target.value)}
                  error={errors.field("saleDiscount")}
                  inputMode="decimal"
                  dir="ltr"
                  autoComplete="off"
                />
                {usdOnly ? null : (
                  <SelectField
                    label={t("till.tender_currency")}
                    value={tenderCurrency}
                    onChange={(e) => setTenderCurrency(e.target.value)}
                  >
                    <option value="">{t("till.same_as_total")}</option>
                    {offered.map((c) => (
                      <option key={c} value={c}>
                        {tDynamic(`currency.${c}`)}
                      </option>
                    ))}
                  </SelectField>
                )}
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
                {credit || usdOnly ? null : (
                  <SelectField
                    label={t("till.change_currency")}
                    value={changeCurrency}
                    onChange={(e) => setChangeCurrency(e.target.value)}
                    error={errors.field("changeCurrency")}
                    hint={t("till.change_default_hint")}
                  >
                    <option value="">{t("till.change_default")}</option>
                    {offered.map((c) => (
                      <option key={c} value={c}>
                        {tDynamic(`currency.${c}`)}
                      </option>
                    ))}
                  </SelectField>
                )}
              </div>
            ) : null}

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

      {repaying ? (
        <QuickRepayment
          localCurrency={local}
          onDone={() => {
            setRepaying(false);
            setRepaid(true);
            setTimeout(focusScan, 0);
          }}
          onClose={() => {
            setRepaying(false);
            setTimeout(focusScan, 0);
          }}
        />
      ) : null}

      {pricing ? (
        <OpenItemDialog
          pick={pricing}
          currency={pricing.priceCurrency || local || "SYP"}
          onAdd={(price, quantity) => {
            const pick = pricing;
            setPricing(null);
            setCart((c) => addToCart(c, pick, quantity, price));
            setStale(false);
            setTimeout(focusScan, 0);
          }}
          onClose={() => {
            setPricing(null);
            setTimeout(focusScan, 0);
          }}
        />
      ) : null}

      {returning ? (
        <ReturnWizard
          onClose={() => {
            setReturning(false);
            setTimeout(focusScan, 0);
          }}
          onDone={(r) => {
            setReturning(false);
            setReturned(r);
            setTimeout(focusScan, 0);
          }}
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
