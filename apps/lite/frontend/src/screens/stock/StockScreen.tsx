import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { Currency, Finding, Product, Unit, Valuation } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { TextField } from "@/ui/Field";
import { AdjustDialog } from "./AdjustDialog";
import { CountDialog } from "./CountDialog";
import { MovementHistory } from "./MovementHistory";
import { OpenPackageDialog } from "./OpenPackageDialog";
import { ReceiveDialog } from "./ReceiveDialog";

/** How long typing pauses before a search is sent. */
export const STOCK_SEARCH_DEBOUNCE_MS = 200;

type Open = { kind: "receive" | "count" | "adjust" | "open" | "history"; product: Product } | null;

/**
 * Stock (L2): what is on hand for everyone, and — in owner mode, on request — what it cost and is worth, with the
 * ledger check's findings (Q-L2.4, L2 §7). Quantities and money arrive formatted by Go; nothing here computes them.
 */
export function StockScreen() {
  const client = useClient();
  const { withOwner, status } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [text, setText] = useState("");
  const [includeInactive, setIncludeInactive] = useState(false);
  const [products, setProducts] = useState<Product[] | null>(null);
  const [allProducts, setAllProducts] = useState<Product[]>([]);
  const [levels, setLevels] = useState<Map<string, string>>(new Map());
  const [units, setUnits] = useState<Map<string, Unit>>(new Map());
  const [currencies, setCurrencies] = useState<Currency[]>([]);
  const [valuation, setValuation] = useState<Valuation | null>(null);
  const [findings, setFindings] = useState<Finding[]>([]);
  const [error, setError] = useState<unknown>(null);
  const [open, setOpen] = useState<Open>(null);

  const loadProducts = useCallback(
    async (query: string, inactive: boolean) => {
      try {
        // An open-priced item is never counted (2026-09-23): it has no stock to receive, count or adjust.
        setProducts((await client.catalog.products({ text: query, includeInactive: inactive })).filter((p) => !p.openPrice));
        setError(null);
      } catch (e) {
        setError(e);
      }
    },
    [client],
  );

  const loadStock = useCallback(async () => {
    try {
      const [lv, all] = await Promise.all([client.stock.levels(), client.catalog.products({ text: "", includeInactive: true })]);
      setLevels(new Map(lv.map((l) => [l.productId, l.onHand])));
      setAllProducts(all);
    } catch (e) {
      setError(e);
    }
  }, [client]);

  useEffect(() => {
    let cancelled = false;
    Promise.all([client.catalog.units(), client.catalog.currencies()])
      .then(([u, c]) => {
        if (cancelled) return;
        setUnits(new Map(u.map((unit) => [unit.code, unit])));
        setCurrencies(c);
      })
      .catch((e) => {
        if (!cancelled) setError(e);
      });
    void loadStock();
    return () => {
      cancelled = true;
    };
  }, [client, loadStock]);

  useEffect(() => {
    const timer = setTimeout(() => void loadProducts(text, includeInactive), STOCK_SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [text, includeInactive, loadProducts]);

  const showCosts = async () => {
    try {
      const v = await withOwner(() => client.stock.valuation());
      setValuation(v);
      setFindings(await client.stock.verify());
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    }
  };

  const hideCosts = () => {
    setValuation(null);
    setFindings([]);
  };

  // Costs leave the screen when owner mode ends — on the transition, so a status not yet read from Go at mount does not
  // clear a valuation Go has just answered.
  const ownerMode = status.elevatedSeconds > 0;
  const wasOwner = useRef(ownerMode);
  useEffect(() => {
    if (wasOwner.current && !ownerMode) hideCosts();
    wasOwner.current = ownerMode;
  }, [ownerMode]);

  const refreshAfterAct = () => {
    void loadStock();
    if (valuation) {
      client.stock
        .valuation()
        .then(setValuation)
        .catch(() => hideCosts());
    }
  };

  const valued = useMemo(() => new Map((valuation?.lines ?? []).map((l) => [l.productId, l])), [valuation]);
  const byId = useMemo(() => new Map(allProducts.map((p) => [p.id, p])), [allProducts]);
  const name = (p: Product) => (locale === "en" && p.nameEn ? p.nameEn : p.nameAr);
  const amount = (value: string) => <bdi dir="ltr">{formatDecimal(value, locale)}</bdi>;
  const unitOf = (p: Product) => units.get(p.unitCode);

  const dialog = () => {
    if (!open) return null;
    const { kind, product } = open;
    const unit = unitOf(product);
    if (!unit) return null;
    const close = () => setOpen(null);
    const done = () => {
      close();
      refreshAfterAct();
    };
    switch (kind) {
      case "receive":
        return (
          <ReceiveDialog product={product} name={name(product)} unit={unit} currencies={currencies} firstMovement={!levels.has(product.id)} onDone={done} onClose={close} />
        );
      case "count":
        return <CountDialog product={product} name={name(product)} unit={unit} onHand={levels.get(product.id) ?? "0"} onDone={done} onClose={close} />;
      case "adjust":
        return <AdjustDialog product={product} name={name(product)} unit={unit} onDone={done} onClose={close} />;
      case "history":
        return <MovementHistory product={product} name={name(product)} unit={unit} onChanged={refreshAfterAct} onClose={close} />;
      case "open": {
        const content = byId.get(product.packageContentId);
        const contentUnit = content ? unitOf(content) : undefined;
        if (!content || !contentUnit) return null;
        return (
          <OpenPackageDialog
            pkg={product}
            name={name(product)}
            pkgUnit={unit}
            contentName={name(content)}
            contentUnit={contentUnit}
            onDone={refreshAfterAct}
            onClose={close}
          />
        );
      }
    }
  };

  return (
    <section className="space-y-4">
      <header className="flex items-center justify-between gap-4">
        <h2 className="text-xl font-semibold">{t("stock.title")}</h2>
        {valuation ? <Button onClick={hideCosts}>{t("stock.hide_costs")}</Button> : <Button onClick={() => void showCosts()}>{t("stock.show_costs")}</Button>}
      </header>

      <div className="flex flex-wrap items-end gap-4">
        <div className="min-w-64 flex-1">
          <TextField label={t("stock.search")} value={text} onChange={(e) => setText(e.target.value)} type="search" autoComplete="off" />
        </div>
        <Checkbox label={t("products.include_inactive")} checked={includeInactive} onChange={(e) => setIncludeInactive(e.target.checked)} />
      </div>

      {!valuation ? <p className="text-xs text-text-muted">{t("stock.costs_owner_hint")}</p> : null}
      {error ? <Alert tone="danger" title={errorText(error)} /> : null}
      {findings.length > 0 ? (
        <Alert tone="danger" title={t("stock.verify_findings", { count: String(findings.length) })}>
          <ul className="list-disc ps-5">
            {findings.map((f, i) => (
              <li key={`${f.code}-${f.productId}-${f.movementId}-${i}`}>
                {tDynamic(f.code)} {byId.get(f.productId) ? `— ${name(byId.get(f.productId)!)}` : ""}
              </li>
            ))}
          </ul>
        </Alert>
      ) : null}
      {valuation ? (
        <p role="status" className="font-semibold">
          {t("stock.total_value", { value: formatDecimal(valuation.total, locale) })}
          {valuation.localCurrency ? (
            <span className="ms-2 font-normal">
              {t("stock.total_value_local", {
                value: formatDecimal(valuation.totalLocal, locale),
                currency: tDynamic(`currency.${valuation.localCurrency}`),
                rate: formatDecimal(valuation.rate, locale),
              })}
            </span>
          ) : null}
        </p>
      ) : null}

      {products && products.length === 0 ? <p className="text-text-muted">{text ? t("stock.no_results") : t("stock.empty")}</p> : null}

      {products && products.length > 0 ? (
        <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
          <table className="w-full text-sm">
            <thead className="bg-surface text-text-muted">
              <tr>
                <th className="p-2 text-start">{t("stock.col.name")}</th>
                <th className="p-2 text-start">{t("stock.col.unit")}</th>
                <th className="p-2 text-start">{t("stock.col.on_hand")}</th>
                {valuation ? <th className="p-2 text-start">{t("stock.col.average_cost")}</th> : null}
                {valuation ? <th className="p-2 text-start">{t("stock.col.value")}</th> : null}
                {valuation?.localCurrency ? (
                  <th className="p-2 text-start">{t("stock.col.value_local", { currency: tDynamic(`currency.${valuation.localCurrency}`) })}</th>
                ) : null}
                <th className="p-2 text-start">{t("stock.col.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {products.map((p) => {
                const line = valued.get(p.id);
                return (
                  <tr key={p.id} className="border-t border-border">
                    <td className="p-2">{name(p)}</td>
                    <td className="p-2">{tDynamic(`uom.${p.unitCode}`)}</td>
                    <td className="p-2">{amount(levels.get(p.id) ?? "0")}</td>
                    {valuation ? <td className="p-2">{line ? amount(line.averageCost) : null}</td> : null}
                    {valuation ? <td className="p-2">{line ? amount(line.value) : null}</td> : null}
                    {valuation?.localCurrency ? <td className="p-2">{line?.valueLocal ? amount(line.valueLocal) : null}</td> : null}
                    <td className="p-2">
                      <div className="flex flex-wrap gap-2">
                        <Button onClick={() => setOpen({ kind: "receive", product: p })} disabled={!p.active}>
                          {t("stock.receive")}
                        </Button>
                        <Button onClick={() => setOpen({ kind: "count", product: p })}>{t("stock.count")}</Button>
                        <Button onClick={() => setOpen({ kind: "adjust", product: p })}>{t("stock.adjust")}</Button>
                        {p.packageContentId ? <Button onClick={() => setOpen({ kind: "open", product: p })}>{t("stock.open_package")}</Button> : null}
                        <Button onClick={() => setOpen({ kind: "history", product: p })}>{t("stock.history")}</Button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : null}

      {dialog()}
    </section>
  );
}
