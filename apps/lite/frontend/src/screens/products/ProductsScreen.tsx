import { useCallback, useEffect, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { ImportResult, Product } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { TextField } from "@/ui/Field";
import { ImportDialog } from "./ImportDialog";
import { ProductForm } from "./ProductForm";

/** How long typing pauses before a search is sent. */
export const SEARCH_DEBOUNCE_MS = 200;

/** The number of till buttons (Q-L1.6); mirrors catalog/domain.QuickSlots. */
export const QUICK_SLOTS = 24;

export function ProductsScreen() {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [text, setText] = useState("");
  const [includeInactive, setIncludeInactive] = useState(false);
  const [products, setProducts] = useState<Product[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [editing, setEditing] = useState<Product | "new" | null>(null);
  const [importing, setImporting] = useState(false);
  const [imported, setImported] = useState<ImportResult | null>(null);

  const load = useCallback(
    async (query: string, inactive: boolean) => {
      try {
        setProducts(await client.catalog.products({ text: query, includeInactive: inactive }));
        setError(null);
      } catch (e) {
        setError(e);
      }
    },
    [client],
  );

  useEffect(() => {
    const timer = setTimeout(() => void load(text, includeInactive), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [text, includeInactive, load]);

  const reload = () => void load(text, includeInactive);

  const act = async (run: () => Promise<unknown>) => {
    try {
      await run();
      reload();
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    }
  };

  const name = (p: Product) => (locale === "en" && p.nameEn ? p.nameEn : p.nameAr);

  return (
    <section className="space-y-4">
      <header className="flex items-center justify-between gap-4">
        <h2 className="text-xl font-semibold">{t("products.title")}</h2>
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => setImporting(true)}>{t("import.open")}</Button>
          <Button variant="primary" onClick={() => setEditing("new")}>
          {t("products.new")}
        </Button>
        </div>
      </header>

      <div className="flex flex-wrap items-end gap-4">
        <div className="min-w-64 flex-1">
          <TextField label={t("products.search")} value={text} onChange={(e) => setText(e.target.value)} type="search" autoComplete="off" />
        </div>
        <Checkbox label={t("products.include_inactive")} checked={includeInactive} onChange={(e) => setIncludeInactive(e.target.checked)} />
      </div>

      {error ? <Alert tone="danger" title={errorText(error)} /> : null}

      {products && products.length === 0 ? (
        <p className="text-text-muted">{text ? t("products.no_results") : t("products.empty")}</p>
      ) : null}

      {products && products.length > 0 ? (
        <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
          <table className="w-full text-sm">
            <thead className="bg-surface text-text-muted">
              <tr>
                <th className="p-2 text-start">{t("products.col.name")}</th>
                <th className="p-2 text-start">{t("products.col.unit")}</th>
                <th className="p-2 text-start">{t("products.col.price")}</th>
                <th className="p-2 text-start">{t("products.col.converted")}</th>
                <th className="p-2 text-start">{t("products.col.slot")}</th>
                <th className="p-2 text-start">{t("products.col.status")}</th>
                <th className="p-2 text-start">{t("products.col.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {products.map((p) => (
                <tr key={p.id} className="border-t border-border">
                  <td className="p-2">{name(p)}</td>
                  <td className="p-2">{tDynamic(`uom.${p.unitCode}`)}</td>
                  <td className="p-2">
                    <bdi dir="ltr">{formatDecimal(p.price, locale)}</bdi> {tDynamic(`currency.${p.priceCurrency}`)}
                  </td>
                  <td className="p-2 text-text-muted">
                    {p.convertedPrice ? (
                      <>
                        <bdi dir="ltr">≈ {formatDecimal(p.convertedPrice, locale)}</bdi> {tDynamic(`currency.${p.convertedCurrency}`)}
                      </>
                    ) : (
                      "—"
                    )}
                  </td>
                  <td className="p-2">
                    <select
                      aria-label={t("products.slot_for", { name: name(p) })}
                      value={p.quickSlot}
                      disabled={!p.active}
                      onChange={(e) => void act(() => client.catalog.setQuickSlot({ id: p.id, slot: Number(e.target.value) }))}
                      className="rounded-md border border-border bg-surface-raised px-2 py-1"
                    >
                      <option value={0}>{t("products.slot_none")}</option>
                      {Array.from({ length: QUICK_SLOTS }, (_, i) => i + 1).map((slot) => (
                        <option key={slot} value={slot}>
                          {slot}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td className="p-2">{p.active ? t("products.active") : t("products.inactive")}</td>
                  <td className="p-2">
                    <div className="flex gap-2">
                      <Button onClick={() => setEditing(p)}>{t("products.edit")}</Button>
                      {p.active ? (
                        <Button onClick={() => void act(() => withOwner(() => client.catalog.setActive({ id: p.id, rowVersion: p.rowVersion, active: false })))}>
                          {t("products.deactivate")}
                        </Button>
                      ) : (
                        <Button onClick={() => void act(() => client.catalog.setActive({ id: p.id, rowVersion: p.rowVersion, active: true }))}>
                          {t("products.reactivate")}
                        </Button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {imported ? <Alert tone="success" title={t("import.done", { created: String(imported.created), stock: String(imported.withStock) })} /> : null}
      {importing ? (
        <ImportDialog
          onClose={() => setImporting(false)}
          onImported={(result) => {
            setImporting(false);
            setImported(result);
            reload();
          }}
        />
      ) : null}
      {editing ? (
        <ProductForm
          product={editing === "new" ? undefined : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            reload();
          }}
        />
      ) : null}
    </section>
  );
}
