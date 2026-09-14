import { useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Entry } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { formErrors } from "@/screens/stock/forms";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { Dialog } from "@/ui/Dialog";
import { SelectField, TextField } from "@/ui/Field";

type Act = () => Promise<Entry>;

/** Runs an owner's act through the PIN, keeping Go's refusal for the form. */
function useOwnerAct(onDone: (e: Entry) => void) {
  const { withOwner } = useOwner();
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const run = async (act: Act) => {
    setBusy(true);
    setError(null);
    try {
      onDone(await withOwner(act));
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };
  return { error, busy, run };
}

function FormAlert({ text }: { text: string | null }) {
  return text ? (
    <p role="alert" className="text-sm text-danger">
      {text}
    </p>
  ) : null;
}

/**
 * An opening balance from the paper book (L5 §7.1) or a write-off (§7.2, Q-L5.7). Both are the owner's; a write-off needs a
 * reason and may take the whole balance.
 */
export function DebtAmountDialog({
  kind,
  customerId,
  name,
  currency,
  currencies,
  onDone,
  onClose,
}: {
  kind: "opening" | "write_off";
  customerId: string;
  name: string;
  currency: string;
  currencies: string[];
  onDone: (e: Entry) => void;
  onClose: () => void;
}) {
  const client = useClient();
  const { t, tDynamic, errorText } = useLocale();
  const [chosen, setChosen] = useState(currency);
  const [amount, setAmount] = useState("");
  const [all, setAll] = useState(false);
  const [note, setNote] = useState("");
  const { error, busy, run } = useOwnerAct(onDone);
  const errors = formErrors(error, errorText);
  const writeOff = kind === "write_off";

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const input = { customerId, currency: chosen, amount: all ? "" : amount, all, note };
    void run(() => (writeOff ? client.customers.writeOff(input) : client.customers.opening(input)));
  };

  return (
    <Dialog title={writeOff ? t("debt.write_off_title", { name }) : t("debt.opening_title", { name })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        {writeOff ? null : (
          <SelectField label={t("debt.currency")} value={chosen} onChange={(e) => setChosen(e.target.value)}>
            {currencies.map((c) => (
              <option key={c} value={c}>
                {tDynamic(`currency.${c}`)}
              </option>
            ))}
          </SelectField>
        )}
        {writeOff ? <Checkbox label={t("debt.write_off_all")} checked={all} onChange={(e) => setAll(e.target.checked)} /> : null}
        {all ? null : (
          <TextField
            label={t("debt.amount", { currency: tDynamic(`currency.${chosen}`) })}
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            error={errors.field("amount")}
            inputMode="decimal"
            dir="ltr"
            autoComplete="off"
            required
          />
        )}
        <TextField
          label={writeOff ? t("debt.reason") : t("debt.opening_note")}
          value={note}
          onChange={(e) => setNote(e.target.value)}
          error={errors.field("reason") ?? errors.field("note")}
          maxLength={200}
          required={writeOff}
          autoComplete="off"
        />
        <p className="text-xs text-text-muted">{writeOff ? t("debt.write_off_hint") : t("debt.opening_hint")}</p>
        <FormAlert text={errors.form} />
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || (!all && amount === "") || (writeOff && note.trim() === "")}>
            {t("action.save")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

/** Paying back a balance in the customer's favour (L5 §7.3, Q-L5.6): owner, reason, never past zero. */
export function RefundDialog({
  customerId,
  name,
  currency,
  currencies,
  onDone,
  onClose,
}: {
  customerId: string;
  name: string;
  currency: string;
  currencies: string[];
  onDone: (e: Entry) => void;
  onClose: () => void;
}) {
  const client = useClient();
  const { t, tDynamic, errorText } = useLocale();
  const [tenderCurrency, setTenderCurrency] = useState("");
  const [amount, setAmount] = useState("");
  const [all, setAll] = useState(true);
  const [reason, setReason] = useState("");
  const { error, busy, run } = useOwnerAct(onDone);
  const errors = formErrors(error, errorText);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    void run(() => client.customers.refund({ customerId, currency, tenderCurrency, amount: all ? "" : amount, all, reason }));
  };

  return (
    <Dialog title={t("debt.refund_title", { name })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <SelectField label={t("debt.refund_in")} value={tenderCurrency} onChange={(e) => setTenderCurrency(e.target.value)}>
          <option value="">{tDynamic(`currency.${currency}`)}</option>
          {currencies
            .filter((c) => c !== currency)
            .map((c) => (
              <option key={c} value={c}>
                {tDynamic(`currency.${c}`)}
              </option>
            ))}
        </SelectField>
        <Checkbox label={t("debt.refund_all")} checked={all} onChange={(e) => setAll(e.target.checked)} />
        {all ? null : (
          <TextField label={t("payment.amount")} value={amount} onChange={(e) => setAmount(e.target.value)} error={errors.field("amount")} inputMode="decimal" dir="ltr" autoComplete="off" />
        )}
        <TextField label={t("debt.reason")} value={reason} onChange={(e) => setReason(e.target.value)} error={errors.field("reason")} maxLength={200} required autoComplete="off" />
        <FormAlert text={errors.form} />
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || reason.trim() === "" || (!all && amount === "")}>
            {t("debt.refund")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

/** Reversing a mistaken entry, once (L5 §7.4): owner, reason. */
export function ReverseDialog({ entry, onDone, onClose }: { entry: Entry; onDone: (e: Entry) => void; onClose: () => void }) {
  const client = useClient();
  const { t, tDynamic, errorText } = useLocale();
  const [reason, setReason] = useState("");
  const { error, busy, run } = useOwnerAct(onDone);
  const errors = formErrors(error, errorText);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    void run(() => client.customers.reverse({ entryId: entry.id, reason }));
  };

  return (
    <Dialog title={t("debt.reverse_title", { kind: tDynamic(`debt.kind.${entry.kind}`) })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <p className="text-sm">{t("debt.reverse_hint")}</p>
        <TextField label={t("debt.reason")} value={reason} onChange={(e) => setReason(e.target.value)} error={errors.field("reason")} maxLength={200} required autoComplete="off" />
        <FormAlert text={errors.form} />
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || reason.trim() === ""}>
            {t("debt.reverse")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
