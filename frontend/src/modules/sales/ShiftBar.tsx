import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { closeShift, openShift, type Shift } from "@/lib/wails";
import { Alert, Badge, Button, Input } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor, isZeroMinor, parseMinor } from "@/modules/accounting/money";

/**
 * Opening and closing a till.
 *
 * # A shift is what makes cash accountable
 *
 * Without one there is a drawer with money in it and no statement of what should be there. The
 * shift records the opening float, gathers every payment taken under it, and at close compares
 * the expectation against a physical count. The difference is posted — over or short — because a
 * difference that is not recorded is a difference nobody investigates.
 */
export function ShiftBar({
  terminal,
  shift,
  compact = false,
}: {
  terminal: string;
  shift?: Shift;
  compact?: boolean;
}) {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const [amount, setAmount] = useState("");
  const [notes, setNotes] = useState("");
  const [closing, setClosing] = useState(false);
  const [failure, setFailure] = useState<unknown>(null);

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["pos"] });

  const start = useMutation({
    mutationFn: () => openShift(terminal, parseMinor(amount || "0") ?? "0"),
    onSuccess: () => {
      setAmount("");
      setFailure(null);
      void refresh();
    },
    onError: setFailure,
  });

  const finish = useMutation({
    mutationFn: () => closeShift(shift!.id, parseMinor(amount || "0") ?? "0", notes),
    onSuccess: () => {
      setAmount("");
      setNotes("");
      setClosing(false);
      void refresh();
    },
    onError: setFailure,
  });

  const open = shift?.status === "open";

  // ── the closed state: a full-screen gate, not a banner ───────────────────────
  if (!open) {
    return (
      <section className="mx-auto flex max-w-md flex-col gap-4 p-6">
        <header className="flex flex-col gap-1">
          <h2 className="text-base font-medium text-text">{t("shift.openTitle")}</h2>
          <p className="text-sm text-text-muted">{t("shift.openHelp")}</p>
        </header>

        {/* The reconciliation of the shift that just ended, shown once. Whoever closed the till
            needs to see whether it balanced, and a figure that appears only in a report is a
            figure they will not read. */}
        {shift?.status === "closed" && shift.differenceMinor !== undefined && (
          <Alert
            tone={isZeroMinor(shift.differenceMinor) ? "success" : "warning"}
            title={t(isZeroMinor(shift.differenceMinor) ? "shift.balanced" : "shift.difference")}
          >
            {t("shift.reconciliation", {
              expected: formatMinor(shift.expectedMinor ?? "0"),
              counted: formatMinor(shift.countedMinor ?? "0"),
              difference: formatMinor(shift.differenceMinor),
            })}
          </Alert>
        )}

        {failure !== null && (
          <Alert tone="danger" title={t("shift.failed")}>{errorText(failure)}</Alert>
        )}

        <Input
          label={t("shift.float")}
          value={amount}
          inputMode="decimal"
          onChange={(event) => setAmount(event.target.value)}
        />
        <Button variant="primary" onClick={() => start.mutate()} disabled={start.isPending}>
          {t("shift.open")}
        </Button>
      </section>
    );
  }

  // ── the open state ───────────────────────────────────────────────────────────
  if (closing) {
    return (
      <section className="flex flex-col gap-3 card p-4">
        <h3 className="text-sm font-medium text-text">{t("shift.closeTitle")}</h3>
        {/* The expected figure is NOT shown while counting, for the reason a stock count is
            blind (0022): a tired person shown "412.50" counts 412.50. */}
        <p className="text-sm text-text-muted">{t("shift.countHelp")}</p>

        {failure !== null && (
          <Alert tone="danger" title={t("shift.failed")}>{errorText(failure)}</Alert>
        )}

        <Input
          label={t("shift.counted")}
          value={amount}
          inputMode="decimal"
          autoFocus
          onChange={(event) => setAmount(event.target.value)}
        />
        <Input
          label={t("shift.notes")}
          value={notes}
          onChange={(event) => setNotes(event.target.value)}
        />
        <div className="flex gap-2">
          <Button variant="primary" onClick={() => finish.mutate()} disabled={finish.isPending}>
            {t("shift.close")}
          </Button>
          <Button variant="ghost" onClick={() => setClosing(false)}>
            {t("common.cancel")}
          </Button>
        </div>
      </section>
    );
  }

  return (
    <div className="flex items-center justify-between card px-4 py-2">
      <span className="flex items-center gap-3 text-sm">
        <Badge tone="success">{t("shift.trading")}</Badge>
        <span className="text-text-muted">
          {terminal} · {t("shift.since", { time: shift?.openedAt ?? "" })}
        </span>
      </span>
      {!compact && <span className="text-sm text-text-muted">{shift?.terminal}</span>}
      <Button variant="secondary" onClick={() => setClosing(true)}>
        {t("shift.close")}
      </Button>
    </div>
  );
}
