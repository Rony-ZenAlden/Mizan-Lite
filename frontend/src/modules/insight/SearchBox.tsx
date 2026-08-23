import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { globalSearch, type SearchResult } from "@/lib/wails";
import { Alert, EmptyState, Input } from "@/shared/ui";

/**
 * One box that finds a product, a customer, an invoice, or a bill.
 *
 * # Nothing found and search being broken are different answers
 *
 * The backend returns what it could find alongside the modules that failed, so a box showing
 * "nothing found" when the sales query is broken would be lying. That distinction is the reason
 * `failed` crosses the boundary at all.
 */
export function SearchBox({ onPick }: { onPick?: (result: SearchResult) => void }) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");

  // Two characters is the backend's minimum, and asking below it would spend a round trip to be
  // refused. The check is duplicated deliberately: the backend's is the one that MATTERS, and
  // this one only avoids a call nobody wants.
  const ready = query.trim().length >= 2;

  const found = useQuery({
    queryKey: ["insight", "search", query.trim()],
    queryFn: () => globalSearch(query.trim()),
    enabled: ready,
  });

  return (
    <div className="flex flex-col gap-3">
      <Input
        type="search"
        label={t("search.label")}
        value={query}
        placeholder={t("search.placeholder")}
        onChange={(event) => setQuery(event.target.value)}
      />

      {ready && found.data && (
        <>
          {found.data.failed.length > 0 && (
            <Alert tone="warning" title={t("search.partial")}>
              {t("search.partialHelp", { sources: found.data.failed.join(", ") })}
            </Alert>
          )}

          {found.data.results.length === 0 ? (
            <EmptyState title={t("search.none")} />
          ) : (
            <ul className="flex flex-col divide-y divide-border card">
              {found.data.results.map((result) => (
                <li key={`${result.kind}:${result.id}`}>
                  <button
                    type="button"
                    onClick={() => onPick?.(result)}
                    className="flex w-full flex-col items-start gap-0.5 px-4 py-2 text-start hover:bg-surface-muted"
                  >
                    <span className="text-sm text-text">{result.label}</span>
                    <span className="text-xs text-text-muted">
                      {t(`search.kind.${result.kind}`)}
                      {result.subtitle && ` · ${result.subtitle}`}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </div>
  );
}
