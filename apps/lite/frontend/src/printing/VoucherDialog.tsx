import type { Entry } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { Money } from "@/screens/sales/Money";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { PrintPanel } from "./PrintPanel";

/**
 * A debt payment's or refund's voucher (L7 §5.3): the printed bitmap and *Print*. Opened after a payment or refund is recorded —
 * printing itself when the settings say so (Q-L7.2) — and from a statement row for a reprint.
 */
export function VoucherDialog({ entry, auto = false, onClose }: { entry: Entry; auto?: boolean; onClose: () => void }) {
  const { t, tDynamic } = useLocale();
  return (
    <Dialog title={t("voucher.title", { kind: tDynamic(`debt.kind.${entry.kind}`) })} onClose={onClose}>
      <p className="text-sm">
        {entry.customerName ? <span className="font-semibold">{entry.customerName}</span> : null} <Money value={entry.amount} currency={entry.currency} />
      </p>
      <PrintPanel kind="entry" id={entry.id} auto={auto} preview />
      <div className="flex justify-end">
        <Button variant="primary" onClick={onClose}>
          {t("action.close")}
        </Button>
      </div>
    </Dialog>
  );
}

/** Whether a debt entry has a voucher: payments and refunds (L7 §5.3). */
export function hasVoucher(entry: Pick<Entry, "kind">): boolean {
  return entry.kind === "payment" || entry.kind === "refund";
}
