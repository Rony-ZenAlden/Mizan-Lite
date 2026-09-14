import { useEffect, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { Customer } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { TextField } from "@/ui/Field";
import { Balances } from "./Balances";
import { CustomerFields } from "./CustomerForm";

/** How long typing pauses before the picker searches. */
export const PICKER_DEBOUNCE_MS = 150;

/**
 * The till's customer picker (L5 §3): search by name in any spelling or by phone in any digits, each match with what it
 * owes in each currency — so the right أبو محمد is charged — and a new customer without leaving the sale.
 */
export function CustomerPicker({ onPick, onClose }: { onPick: (c: Customer) => void; onClose: () => void }) {
  const client = useClient();
  const { t, errorText } = useLocale();
  const [text, setText] = useState("");
  const [found, setFound] = useState<Customer[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const timer = setTimeout(() => {
      client.customers
        .search({ text, owingOnly: false, includeInactive: false })
        .then((c) => {
          if (!cancelled) {
            setFound(c);
            setError(null);
          }
        })
        .catch((e) => {
          if (!cancelled) setError(e);
        });
    }, PICKER_DEBOUNCE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [client, text]);

  return (
    <Dialog title={t("customers.pick_title")} onClose={onClose}>
      {creating ? (
        <CustomerFields onSaved={onPick} onCancel={() => setCreating(false)} />
      ) : (
        <div className="space-y-3">
          <TextField label={t("customers.search")} value={text} onChange={(e) => setText(e.target.value)} type="search" autoComplete="off" />
          {error ? <Alert tone="danger" title={errorText(error)} /> : null}
          {found && found.length === 0 ? <p className="text-sm text-text-muted">{t("customers.no_results")}</p> : null}
          <ul aria-label={t("customers.matches")} className="max-h-80 space-y-1 overflow-auto">
            {(found ?? []).map((c) => (
              <li key={c.id}>
                <button
                  type="button"
                  onClick={() => onPick(c)}
                  className="flex w-full items-start justify-between gap-3 rounded-md border border-border p-2 text-start text-sm hover:bg-surface focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <span>
                    <span className="block font-medium">{c.name}</span>
                    {c.phone ? (
                      <bdi dir="ltr" className="text-xs text-text-muted">
                        {c.phone}
                      </bdi>
                    ) : null}
                  </span>
                  <span className="text-xs">
                    <Balances balances={c.balances} />
                  </span>
                </button>
              </li>
            ))}
          </ul>
          <div className="flex justify-between gap-2">
            <Button onClick={() => setCreating(true)}>{t("customers.new")}</Button>
            <Button onClick={onClose}>{t("action.cancel")}</Button>
          </div>
        </div>
      )}
    </Dialog>
  );
}
