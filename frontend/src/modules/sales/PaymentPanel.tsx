import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { postSale, takePayment, type SalesDetail } from "@/lib/wails";
import { Alert, Button, Input, Select } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { compareMinor, formatMinor, parseMinor, subtractMinor } from "@/modules/accounting/money";

/** The methods a till takes. Mirrors the payment methods the posting rules know (§20.3). */
const METHODS = ["cash", "card", "bank_transfer", "cheque"] as const;

/**
 * Taking the money.
 *
 * # Posting comes first, then the payment
 *
 * The sale is POSTED before the payment is recorded, and the order is not incidental. Posting
 * issues the stock, writes the journal, and allocates the invoice number; a payment allocates
 * against that invoice. Recording money against a document that does not yet exist as an
 * obligation gives an unallocated receipt and a customer who is charged but not invoiced.
 *
 * Both happen inside one screen action, so an operator sees one button and one outcome.
 */
export function PaymentPanel({
  sale,
  shiftId,
  onSettled,
  onCancel,
}: {
  sale: SalesDetail;
  shiftId: string;
  onSettled: () => void;
  onCancel: () => void;
}) {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const [method, setMethod] = useState<string>("cash");
  const [typed, setTyped] = useState("");
  const [failure, setFailure] = useState<unknown>(null);

  const total = sale.document.totalMinor;
  // An empty field means the customer is paying the exact amount, which is what a card does and
  // what most cash customers do. Making them type it would be a keystroke on every sale.
  const tendered = typed.trim() === "" ? total : parseMinor(typed);

  const malformed = tendered === null;
  const short = !malformed && compareMinor(tendered, total) < 0;
  // Change is computed HERE, exactly, with BigInt — the operator needs it the instant they type
  // and cannot wait for a round trip to open the drawer. See money.ts for why not Number.
  const change = malformed ? "0" : subtractMinor(tendered, total);

  const settle = useMutation({
    mutationFn: async () => {
      await postSale(sale.document.id);
      return takePayment({
        documentId: sale.document.id,
        shiftId,
        method,
        // What is RECORDED is what the sale was worth, not what was handed over. The extra note
        // in a cash drawer is change, not revenue, and recording the tendered amount would
        // overstate the day by exactly the change given.
        amountMinor: total,
        reference: "",
        date: "",
        currency: sale.document.currency,
      });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["pos"] });
      void queryClient.invalidateQueries({ queryKey: ["sales"] });
      onSettled();
    },
    onError: setFailure,
  });

  return (
    <div className="flex flex-col gap-3 border-t border-border pt-3">
      <Select
        label={t("pos.method")}
        value={method}
        onValueChange={setMethod}
        options={METHODS.map((code) => ({ value: code, label: t(`pos.method.${code}`) }))}
      />

      {/* Cash is the only method where a customer hands over more than the amount. A card is
          charged exactly, so asking what was tendered would be a question with one answer. */}
      {method === "cash" && (
        <>
          <Input
            label={t("pos.tendered")}
            value={typed}
            inputMode="decimal"
            placeholder={formatMinor(total)}
            onChange={(event) => setTyped(event.target.value)}
            error={malformed ? t("pos.notAnAmount") : undefined}
          />
          {!malformed && (
            <div className="flex items-baseline justify-between rounded bg-surface-sunken px-3 py-2">
              <span className="text-sm text-text-muted">{t("pos.change")}</span>
              <span
                className={`font-mono text-xl tabular-nums ${short ? "text-danger" : "text-text"}`}
              >
                {short ? formatMinor(subtractMinor(total, tendered)) : formatMinor(change)}
              </span>
            </div>
          )}
          {short && <Alert tone="warning" title={t("pos.short")}>{t("pos.shortHelp")}</Alert>}
        </>
      )}

      {failure !== null && (
        <Alert tone="danger" title={t("pos.paymentFailed")}>
          {errorText(failure)}
        </Alert>
      )}

      <div className="flex gap-2">
        <Button
          variant="primary"
          // Refused while the tender is short: a till that accepts less than the total and calls
          // it settled has invented a debt nobody agreed to.
          disabled={malformed || short || settle.isPending}
          onClick={() => settle.mutate()}
        >
          {t("pos.complete")}
        </Button>
        <Button variant="ghost" onClick={onCancel} disabled={settle.isPending}>
          {t("common.cancel")}
        </Button>
      </div>
    </div>
  );
}
