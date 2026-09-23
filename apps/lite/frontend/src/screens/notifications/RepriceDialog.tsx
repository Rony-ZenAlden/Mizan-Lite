import { useCallback, useEffect, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { RepriceProposal } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { typeable } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Money } from "@/screens/sales/Money";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { Dialog } from "@/ui/Dialog";
import { TextField } from "@/ui/Field";

/**
 * Re-pricing after the exchange rate moved (the owner's request, 2026-09-23) — a PROPOSAL.
 *
 * Nothing changes until the owner confirms, and closing the dialog changes nothing at all: the owner was explicit that
 * this is optional, never mandatory and never applied by itself. Each price proposed keeps the product's dollar value
 * where it was when it was priced; the owner may instead move every price by one percentage, untick any product, or
 * walk away. Go works out every figure — the screen never computes a price (DESIGN D9).
 */
export function RepriceDialog({ onDone, onClose }: { onDone: (count: number) => void; onClose: () => void }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, locale, errorText } = useLocale();
  const [proposal, setProposal] = useState<RepriceProposal | null>(null);
  const [percent, setPercent] = useState("");
  const [skipped, setSkipped] = useState<ReadonlySet<string>>(() => new Set());
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(
    async (p: string) => {
      setError(null);
      try {
        setProposal(await client.catalog.repriceProposal(p));
      } catch (e) {
        setError(e);
      }
    },
    [client],
  );

  useEffect(() => {
    void load("");
  }, [load]);

  const chosen = proposal?.items.filter((i) => !skipped.has(i.id)) ?? [];

  const toggle = (productId: string, include: boolean) =>
    setSkipped((s) => {
      const next = new Set(s);
      if (include) next.delete(productId);
      else next.add(productId);
      return next;
    });

  const apply = async () => {
    setBusy(true);
    setError(null);
    try {
      // The price the owner saw, as the shop types it: the new figure of a dual reading (0.9.9).
      const done = await withOwner(() =>
        client.catalog.bulkReprice(chosen.map((i) => ({ id: i.id, rowVersion: i.rowVersion, price: typeable(i.proposed) }))),
      );
      onDone(done.length);
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  const name = (i: { nameAr: string; nameEn: string }) => (locale === "en" && i.nameEn ? i.nameEn : i.nameAr);

  return (
    <Dialog title={t("reprice.title")} onClose={onClose} wide>
      <div className="space-y-3" data-testid="reprice-dialog">
        <p className="text-sm text-text-muted">{t("reprice.explain")}</p>
        <div className="flex items-end gap-2">
          <div className="flex-1">
            <TextField
              label={t("reprice.percent")}
              hint={t("reprice.percent_hint")}
              value={percent}
              onChange={(e) => setPercent(e.target.value)}
              inputMode="decimal"
              dir="ltr"
            />
          </div>
          <Button onClick={() => void load(percent.trim())}>{t("reprice.recalculate")}</Button>
        </div>
        {error ? <Alert tone="danger" title={errorText(error)} /> : null}
        {proposal && proposal.items.length === 0 ? (
          <p className="text-sm" data-testid="reprice-none">
            {t("reprice.none")}
          </p>
        ) : null}
        {proposal && proposal.items.length > 0 ? (
          <div className="max-h-96 overflow-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-xs text-text-muted">
                  <th className="p-1 text-start">{t("alerts.product")}</th>
                  <th className="p-1 text-start">{t("reprice.now")}</th>
                  <th className="p-1 text-start">{t("reprice.proposed")}</th>
                  <th className="p-1 text-start">{t("reprice.shift")}</th>
                </tr>
              </thead>
              <tbody>
                {proposal.items.map((i) => (
                  <tr key={i.id} data-testid="reprice-item" className={skipped.has(i.id) ? "opacity-50" : ""}>
                    <td className="p-1">
                      <Checkbox label={name(i)} checked={!skipped.has(i.id)} onChange={(e) => toggle(i.id, e.target.checked)} />
                    </td>
                    <td className="p-1">
                      <Money value={i.price} currency={i.currency} />
                    </td>
                    <td className="p-1 font-semibold">
                      <Money value={i.proposed} currency={i.currency} />
                    </td>
                    <td className="p-1">
                      <bdi dir="ltr">{i.shift}%</bdi>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
        <p className="text-xs text-text-muted">{t("reprice.nothing_until_confirmed")}</p>
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("reprice.not_now")}</Button>
          <Button variant="primary" onClick={() => void apply()} disabled={busy || chosen.length === 0} data-testid="reprice-apply">
            {t("reprice.apply", { count: String(chosen.length) })}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
