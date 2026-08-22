import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { countStock, type StockRow } from "@/lib/wails";
import { Alert, Button, Dialog, Input } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatQuantity } from "@/modules/inventory/quantity";

/**
 * Correcting what the system believes is on a shelf (Step 10.10).
 *
 * # It asks what is THERE, not what the difference is
 *
 * The movement recorded underneath is a delta — that is what the ledger stores and the right
 * shape for it. But a delta is the wrong question to ask somebody holding a shelf: they know
 * there are eleven, and making them work out that eleven is two fewer than thirteen is arithmetic
 * the computer should do, and the subtraction a tired person gets wrong at the end of a long day.
 *
 * The conversion happens on the backend, so the ledger is unchanged and the screen is the only
 * thing that got easier.
 */
export function CountDialog({
  row,
  warehouseId,
  onClose,
}: {
  row: StockRow | null;
  warehouseId: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const [counted, setCounted] = useState("");
  const [reason, setReason] = useState("");

  const record = useMutation({
    mutationFn: countStock,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["inventory"] });
      setCounted("");
      setReason("");
      onClose();
    },
  });

  if (!row) return null;

  // Typed in whole units; the boundary wants micro-units. Done with strings rather than
  // arithmetic on a float, for the reason every quantity in this codebase is an integer.
  const asMicro = (value: string): string => {
    const [whole = "0", fraction = ""] = value.trim().split(".");
    return `${whole}${(fraction + "000000").slice(0, 6)}`.replace(/^0+(?=\d)/, "");
  };

  const ready = counted.trim() !== "" && !Number.isNaN(Number(counted));

  return (
    <Dialog open={row !== null} onOpenChange={(open) => !open && onClose()} title={t("stock.count")}>
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium text-text">{row.productName}</span>
          <span className="font-mono text-xs text-text-muted">{row.sku}</span>
        </div>

        {/* What Mizan currently believes, shown so the difference is visible without being the
            thing typed. */}
        <p className="text-sm text-text-muted">
          {t("stock.currentlyBelieves", { quantity: formatQuantity(row.onHandMicro) })}
        </p>

        {record.isError && (
          <Alert tone="danger" title={t("stock.countFailed")}>{errorText(record.error)}</Alert>
        )}

        <Input
          label={t("stock.countedLabel")}
          value={counted}
          onChange={(event) => setCounted(event.target.value)}
          placeholder={t("stock.countedHint")}
        />
        <Input
          label={t("stock.reason")}
          value={reason}
          onChange={(event) => setReason(event.target.value)}
          placeholder={t("stock.reasonHint")}
        />

        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button
            disabled={!ready || record.isPending}
            onClick={() =>
              record.mutate({
                variantId: row.variantId,
                warehouseId,
                countedMicro: asMicro(counted),
                reason: reason.trim(),
              })
            }
          >
            {t("stock.recordCount")}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
