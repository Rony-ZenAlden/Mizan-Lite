import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Product } from "@/api/client";

/** How many products a search offers at once. */
export const SEARCH_MATCHES = 8;

/**
 * Finding a stocked product by name or barcode, for the forms that put goods in or take them out — a purchase, a
 * write-off (0.10.0). Open-priced items are never offered: they are never counted in stock (2026-09-23).
 *
 * A scanner types a barcode and Enter faster than the products load when a form has just opened. The Enter is
 * remembered and answered once they are here (found by the end-to-end journey J13).
 */
export function useProductSearch(onPick: (p: Product) => void) {
  const client = useClient();
  const [products, setProducts] = useState<Product[] | null>(null);
  const [search, setSearch] = useState("");
  const [pendingEnter, setPendingEnter] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const picked = useRef(onPick);
  useLayoutEffect(() => {
    picked.current = onPick;
  });

  useEffect(() => {
    let cancelled = false;
    client.catalog
      .products({ text: "", includeInactive: false })
      .then((all) => {
        if (!cancelled) setProducts(all.filter((p) => p.active && !p.openPrice));
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e);
      });
    return () => {
      cancelled = true;
    };
  }, [client]);

  const matches = useMemo(() => {
    const text = search.trim().toLowerCase();
    if (text === "" || products === null) return [];
    const byBarcode = products.filter((p) => p.barcode !== "" && p.barcode === search.trim());
    if (byBarcode.length > 0) return byBarcode;
    return products.filter((p) => p.nameAr.toLowerCase().includes(text) || p.nameEn.toLowerCase().includes(text)).slice(0, SEARCH_MATCHES);
  }, [products, search]);

  const pick = (p: Product) => {
    picked.current(p);
    setSearch("");
  };

  useEffect(() => {
    if (!pendingEnter || products === null) return;
    setPendingEnter(false);
    const first = matches[0];
    if (first) {
      picked.current(first);
      setSearch("");
    }
  }, [pendingEnter, products, matches]);

  /** Enter picks the first match; it never submits the form around the field. */
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "Enter") return;
    event.preventDefault();
    if (products === null) setPendingEnter(true);
    else if (matches[0]) pick(matches[0]);
  };

  return { loaded: products !== null, search, setSearch, matches, pick, onKeyDown, error };
}
