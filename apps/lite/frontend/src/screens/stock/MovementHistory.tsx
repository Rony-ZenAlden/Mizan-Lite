import { useCallback, useEffect, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { History, Movement, Product, Unit } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { formatDate } from "@/i18n/time";
import { CostCorrectionDialog } from "./CostCorrectionDialog";

/** How many of a product's newest movements the history shows. */
export const HISTORY_LIMIT = 100;

/**
 * A product's movements, newest first. Costs appear only when Go sent them — in owner mode (Q-L2.4). A reversal is
 * offered on the one receipt Go names as reversible: the newest movement, when it is a receipt (L2 §5.5).
 */
export function MovementHistory({
  product,
  name,
  unit,
  onChanged,
  onClose,
}: {
  product: Product;
  name: string;
  unit: Unit;
  onChanged: () => void;
  onClose: () => void;
}) {
  const client = useClient();
  const { withOwner, status } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [history, setHistory] = useState<History | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [correcting, setCorrecting] = useState(false);

  const load = useCallback(async () => {
    try {
      setHistory(await client.stock.movements(product.id, HISTORY_LIMIT));
      setError(null);
    } catch (e) {
      setError(e);
    }
  }, [client, product.id]);

  // Owner mode starting or ending changes what Go sends, so the history is read again when it does.
  const ownerMode = status.elevatedSeconds > 0;
  useEffect(() => {
    void load();
  }, [load, ownerMode]);

  const reverse = async (movement: Movement) => {
    try {
      await withOwner(() => client.stock.reverseReceipt({ movementId: movement.id, note: "" }));
      onChanged();
      await load();
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    }
  };

  if (correcting) {
    return (
      <CostCorrectionDialog
        product={product}
        name={name}
        onClose={() => setCorrecting(false)}
        onDone={() => {
          setCorrecting(false);
          onChanged();
          void load();
        }}
      />
    );
  }

  const quantity = (value: string) => <bdi dir="ltr">{formatDecimal(value, locale)}</bdi>;
  const details = (m: Movement) =>
    [
      m.reason ? tDynamic(`stock.reason.${m.reason}`) : "",
      m.note,
      m.enteredCurrency
        ? m.rate
          ? t("stock.entered_rate", { cost: formatDecimal(m.enteredUnitCost, locale), currency: tDynamic(`currency.${m.enteredCurrency}`), rate: formatDecimal(m.rate, locale) })
          : t("stock.entered", { cost: formatDecimal(m.enteredUnitCost, locale), currency: tDynamic(`currency.${m.enteredCurrency}`) })
        : "",
    ]
      .filter(Boolean)
      .join(" · ");

  return (
    <Dialog title={t("stock.history_title", { name })} onClose={onClose} wide>
      <div className="flex justify-between gap-2">
        <p className="text-sm text-text-muted">{tDynamic(`uom.${unit.code}`)}</p>
        <Button onClick={() => setCorrecting(true)}>{t("stock.correct_cost")}</Button>
      </div>
      {error ? <Alert tone="danger" title={errorText(error)} /> : null}
      {history && history.movements.length === 0 ? <p className="text-text-muted">{t("stock.history_empty")}</p> : null}
      {history && history.movements.length > 0 ? (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full text-sm">
            <thead className="bg-surface text-text-muted">
              <tr>
                <th className="p-2 text-start">{t("stock.col.date")}</th>
                <th className="p-2 text-start">{t("stock.col.movement")}</th>
                <th className="p-2 text-start">{t("stock.col.quantity")}</th>
                <th className="p-2 text-start">{t("stock.col.after")}</th>
                {history.costsVisible ? <th className="p-2 text-start">{t("stock.col.unit_cost")}</th> : null}
                {history.costsVisible ? <th className="p-2 text-start">{t("stock.col.average_after")}</th> : null}
                <th className="p-2 text-start">{t("stock.col.details")}</th>
                <th className="p-2 text-start">{t("stock.col.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {history.movements.map((m) => (
                <tr key={m.id} className="border-t border-border">
                  <td className="p-2">
                    <bdi dir="ltr">{formatDate(m.businessDate)}</bdi>
                  </td>
                  <td className="p-2">{tDynamic(`stock.kind.${m.kind}`)}</td>
                  <td className="p-2">{quantity(m.quantity)}</td>
                  <td className="p-2">{quantity(m.onHandAfter)}</td>
                  {history.costsVisible ? <td className="p-2">{quantity(m.unitCost)}</td> : null}
                  {history.costsVisible ? <td className="p-2">{quantity(m.averageCostAfter)}</td> : null}
                  <td className="p-2">{details(m)}</td>
                  <td className="p-2">
                    {m.id === history.reversibleId ? <Button onClick={() => void reverse(m)}>{t("stock.reverse")}</Button> : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      <div className="flex justify-end">
        <Button onClick={onClose}>{t("action.close")}</Button>
      </div>
    </Dialog>
  );
}
