import { useCallback, useEffect, useRef, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { DayReport, MonthReport, ProductsReport, StockReport } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { ExportButtons } from "@/exports/ExportButtons";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { TextField } from "@/ui/Field";
import { DayStatement } from "./DayStatement";
import { MonthTable } from "./MonthTable";
import { ProductsTable } from "./ProductsTable";
import { StockSection } from "./StockSection";

type Tab = "day" | "month" | "products" | "stock";
const TABS: Tab[] = ["day", "month", "products", "stock"];

/**
 * The owner's reports (L6 §10.2): Day, Month, Products and Stock. Every figure is Go's. Profit, costs, stock value and
 * expenses are the owner's (Q-L6.7): the screen asks for the PIN to show them and clears them when owner mode ends.
 * On screen only — printing is L7's (Q-L6.9).
 */
export function ReportsScreen() {
  const client = useClient();
  const { status, withOwner } = useOwner();
  const { t, tDynamic, errorText } = useLocale();
  const [tab, setTab] = useState<Tab>("day");
  const [date, setDate] = useState("");
  const [month, setMonth] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [day, setDay] = useState<DayReport | null>(null);
  const [monthReport, setMonthReport] = useState<MonthReport | null>(null);
  const [products, setProducts] = useState<ProductsReport | null>(null);
  const [stock, setStock] = useState<StockReport | null>(null);
  const [hidden, setHidden] = useState(false);
  const [error, setError] = useState<unknown>(null);

  const clear = () => {
    setDay(null);
    setMonthReport(null);
    setProducts(null);
    setStock(null);
  };

  const load = useCallback(async () => {
    setError(null);
    try {
      switch (tab) {
        case "day":
          setDay(await withOwner(() => client.reports.day(date)));
          break;
        case "month":
          setMonthReport(await withOwner(() => client.reports.month(month)));
          break;
        case "products":
          setProducts(await withOwner(() => client.reports.products(from, to)));
          break;
        case "stock":
          setStock(await withOwner(() => client.reports.stock(from, to)));
          break;
      }
      setHidden(false);
    } catch (e) {
      if (e instanceof OwnerCancelled) setHidden(true);
      else setError(e);
    }
  }, [client, withOwner, tab, date, month, from, to]);

  useEffect(() => {
    void load();
  }, [load]);

  // Owner mode ending takes the figures off the screen; the owner asks again to see them.
  const ownerMode = status.elevatedSeconds > 0;
  const wasOwner = useRef(ownerMode);
  useEffect(() => {
    if (wasOwner.current && !ownerMode) {
      clear();
      setHidden(true);
    }
    wasOwner.current = ownerMode;
  }, [ownerMode]);

  const openDay = (d: string) => {
    setDate(d);
    setTab("day");
  };

  // The export is of the report on screen, so it is offered once that report is there.
  const shown = { day, month: monthReport, products, stock }[tab] !== null;

  const rangeFields = (
    <>
      <div className="w-44">
        <TextField label={t("reports.from")} type="date" value={from || products?.from || stock?.from || ""} onChange={(e) => setFrom(e.target.value)} dir="ltr" />
      </div>
      <div className="w-44">
        <TextField label={t("reports.to")} type="date" value={to || products?.to || stock?.to || ""} onChange={(e) => setTo(e.target.value)} dir="ltr" />
      </div>
    </>
  );

  return (
    <section className="space-y-4">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <h2 className="text-xl font-semibold">{t("reports.title")}</h2>
        <div role="tablist" aria-label={t("reports.tabs")} className="flex gap-1">
          {TABS.map((name) => (
            <button
              key={name}
              type="button"
              role="tab"
              aria-selected={tab === name}
              onClick={() => setTab(name)}
              className={`rounded-md px-3 py-1 text-sm ${tab === name ? "bg-primary text-primary-fg" : "border border-border hover:bg-surface"}`}
            >
              {tDynamic(`reports.tab.${name}`)}
            </button>
          ))}
        </div>
      </header>

      <div className="flex flex-wrap items-end gap-3">
        {tab === "day" ? (
          <div className="w-44">
            <TextField label={t("reports.date")} type="date" value={date || day?.date || ""} onChange={(e) => setDate(e.target.value)} dir="ltr" />
          </div>
        ) : null}
        {tab === "month" ? (
          <div className="w-44">
            <TextField label={t("reports.month")} type="month" value={month || monthReport?.month || ""} onChange={(e) => setMonth(e.target.value)} dir="ltr" />
          </div>
        ) : null}
        {tab === "products" || tab === "stock" ? rangeFields : null}
      </div>

      {!hidden && shown ? (
        <ExportButtons
          onExport={(format) =>
            client.exports.report({
              kind: tab,
              date: date || day?.date || "",
              month: month || monthReport?.month || "",
              from: from || products?.from || stock?.from || "",
              to: to || products?.to || stock?.to || "",
              format,
            })
          }
        />
      ) : null}

      {error ? <Alert tone="danger" title={errorText(error)} /> : null}
      {hidden ? (
        <div className="space-y-2">
          <p className="text-text-muted">{t("reports.owner_hint")}</p>
          <Button variant="primary" onClick={() => void load()}>
            {t("reports.show")}
          </Button>
        </div>
      ) : null}

      {!hidden && tab === "day" && day ? <DayStatement report={day} /> : null}
      {!hidden && tab === "month" && monthReport ? <MonthTable report={monthReport} onOpenDay={openDay} /> : null}
      {!hidden && tab === "products" && products ? <ProductsTable report={products} /> : null}
      {!hidden && tab === "stock" && stock ? <StockSection report={stock} /> : null}
    </section>
  );
}
