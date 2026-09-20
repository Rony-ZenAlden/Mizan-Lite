import { useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { CashEntry } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { formErrors } from "@/screens/stock/forms";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { Dialog } from "@/ui/Dialog";
import { SelectField, TextField } from "@/ui/Field";

export type MoneyKind = "expense" | "withdrawal" | "deposit";

/** An expense, a withdrawal or a deposit (L6 §7.3). The owner's: Go refuses it outside owner mode and the PIN is asked. */
export function MoneyDialog({ kind, categories, onDone, onClose }: { kind: MoneyKind; categories: string[]; onDone: (e: CashEntry) => void; onClose: () => void }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText } = useLocale();
  const [currency, setCurrency] = useState("SYP");
  const [amount, setAmount] = useState("");
  const [category, setCategory] = useState(categories[0] ?? "other");
  const [fromDrawer, setFromDrawer] = useState(true);
  // An expense is the day's small change unless the shop says it recurs (2026-09-20). Nothing is posted automatically:
  // marking it monthly tells the reports how to read it, and the shop still records it when it pays it.
  const [recurrence, setRecurrence] = useState<"once" | "monthly">("once");
  const [note, setNote] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const errors = formErrors(error, errorText);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onDone(
        await withOwner(() =>
          client.cash.record({
            kind,
            currency,
            amount,
            category: kind === "expense" ? category : "",
            fromDrawer: kind === "expense" && fromDrawer,
            recurrence: kind === "expense" ? recurrence : "once",
            note,
          }),
        ),
      );
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={tDynamic(`cash.${kind}.title`)} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <SelectField label={t("cash.currency")} value={currency} onChange={(e) => setCurrency(e.target.value)}>
          <option value="SYP">{t("currency.SYP")}</option>
          <option value="USD">{t("currency.USD")}</option>
        </SelectField>
        <TextField label={t("cash.amount")} value={amount} onChange={(e) => setAmount(e.target.value)} inputMode="decimal" dir="ltr" error={errors.field("amount")} required />
        {kind === "expense" ? (
          <>
            <SelectField label={t("cash.category")} value={category} onChange={(e) => setCategory(e.target.value)} error={errors.field("category")}>
              {categories.map((c) => (
                <option key={c} value={c}>
                  {tDynamic(`cash.category.${c}`)}
                </option>
              ))}
            </SelectField>
            <SelectField
              label={t("cash.recurrence")}
              value={recurrence}
              onChange={(e) => setRecurrence(e.target.value as "once" | "monthly")}
              hint={t("cash.recurrence_hint")}
              data-testid="cash-recurrence"
            >
              <option value="once">{t("cash.recurrence.once")}</option>
              <option value="monthly">{t("cash.recurrence.monthly")}</option>
            </SelectField>
            <Checkbox label={t("cash.from_drawer")} checked={fromDrawer} onChange={(e) => setFromDrawer(e.target.checked)} />
          </>
        ) : null}
        <TextField label={t("cash.note")} value={note} onChange={(e) => setNote(e.target.value)} maxLength={200} error={errors.field("note")} />
        {errors.form ? <Alert tone="danger" title={errors.form} /> : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || amount.trim() === ""}>
            {t("action.save")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

/** A closing count in one currency. Anyone at the counter may count (Q-L6.7): no PIN. */
export function CountDialog({ currency, expected, onDone, onClose }: { currency: string; expected: string; onDone: (e: CashEntry) => void; onClose: () => void }) {
  const client = useClient();
  const { t, tDynamic, errorText } = useLocale();
  const [counted, setCounted] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const errors = formErrors(error, errorText);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onDone(await client.cash.count({ currency, counted, note }));
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t("cash.count.title", { currency: tDynamic(`currency.${currency}`) })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <p className="text-sm">{t("cash.count.explain")}</p>
        <p className="text-sm text-text-muted">{t("cash.count.hint", { expected, currency: tDynamic(`currency.short.${currency}`) })}</p>
        <TextField label={t("cash.counted")} value={counted} onChange={(e) => setCounted(e.target.value)} inputMode="decimal" dir="ltr" error={errors.field("amount")} required />
        <TextField label={t("cash.note")} value={note} onChange={(e) => setNote(e.target.value)} maxLength={200} error={errors.field("note")} />
        {errors.form ? <Alert tone="danger" title={errors.form} /> : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || counted.trim() === ""}>
            {t("cash.count.save")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

/** Reversing a cash book entry, with a reason. The owner's. */
export function ReverseCashDialog({ entry, onDone, onClose }: { entry: CashEntry; onDone: (e: CashEntry) => void; onClose: () => void }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText } = useLocale();
  const [reason, setReason] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const errors = formErrors(error, errorText);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onDone(await withOwner(() => client.cash.reverse(entry.id, reason)));
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t("cash.reverse.title", { kind: tDynamic(`cash.kind.${entry.kind}`) })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <TextField label={t("cash.reverse.reason")} value={reason} onChange={(e) => setReason(e.target.value)} maxLength={200} error={errors.field("reason")} required />
        {errors.form ? <Alert tone="danger" title={errors.form} /> : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || reason.trim() === ""}>
            {t("cash.reverse.confirm")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
