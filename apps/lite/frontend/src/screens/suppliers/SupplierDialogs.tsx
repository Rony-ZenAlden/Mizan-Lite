import { useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { CashSource, SupplierEntry } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { formErrors } from "@/screens/stock/forms";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { Dialog } from "@/ui/Dialog";
import { SelectField, TextField } from "@/ui/Field";

/** Runs an act through the PIN when the owner's switch asks for it, keeping Go's refusal for the form. */
export function useSupplierAct<T>(onDone: (result: T) => void) {
  const { withOwner } = useOwner();
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const run = async (act: () => Promise<T>) => {
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

/** Where the money came from, or went: the drawer — which then expects less, or more — or the owner's own pocket. */
export function SourceField({ label, value, onChange, error }: { label: string; value: CashSource; onChange: (s: CashSource) => void; error?: string }) {
  const { t } = useLocale();
  return (
    <SelectField label={label} value={value} onChange={(e) => onChange(e.target.value as CashSource)} error={error}>
      <option value="drawer">{t("suppliers.source.drawer")}</option>
      <option value="owner">{t("suppliers.source.owner")}</option>
    </SelectField>
  );
}

/**
 * Money paid to a supplier, money a supplier paid back, or a balance brought over from the paper book (0.10.0). A payment
 * and an opening balance are the owner's acts; where money came from is chosen each time — the drawer or the owner's own
 * pocket (the owner's answer, 2026-09-24).
 */
export function SupplierMoneyDialog({
  kind,
  supplierId,
  name,
  currency,
  currencies,
  onDone,
  onClose,
}: {
  kind: "pay" | "refund" | "opening";
  supplierId: string;
  name: string;
  currency: string;
  currencies: string[];
  onDone: (e: SupplierEntry) => void;
  onClose: () => void;
}) {
  const client = useClient();
  const { t, tDynamic, errorText } = useLocale();
  const [chosen, setChosen] = useState(currency);
  const [amount, setAmount] = useState("");
  const [source, setSource] = useState<CashSource>("drawer");
  const [inShopsFavour, setInShopsFavour] = useState(false);
  const [note, setNote] = useState("");
  const { error, busy, run } = useSupplierAct(onDone);
  const errors = formErrors(error, errorText);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const input = { supplierId, currency: chosen, amount, source: kind === "opening" ? "" : source, inShopsFavour: kind === "opening" && inShopsFavour, note };
    void run(() => (kind === "pay" ? client.suppliers.pay(input) : kind === "refund" ? client.suppliers.refund(input) : client.suppliers.opening(input)));
  };

  return (
    <Dialog title={tDynamic(`suppliers.${kind}_title`, { name })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <SelectField label={t("suppliers.currency")} value={chosen} onChange={(e) => setChosen(e.target.value)} error={errors.field("currency")}>
          {currencies.map((c) => (
            <option key={c} value={c}>
              {tDynamic(`currency.${c}`)}
            </option>
          ))}
        </SelectField>
        <TextField
          label={t("suppliers.amount", { currency: tDynamic(`currency.${chosen}`) })}
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          error={errors.field("amount")}
          inputMode="decimal"
          dir="ltr"
          autoComplete="off"
          required
        />
        {kind === "opening" ? (
          <Checkbox label={t("suppliers.opening_favour")} checked={inShopsFavour} onChange={(e) => setInShopsFavour(e.target.checked)} />
        ) : (
          <SourceField label={t(kind === "pay" ? "suppliers.paid_from" : "suppliers.refund_into")} value={source} onChange={setSource} error={errors.field("source")} />
        )}
        <TextField label={t("suppliers.note")} value={note} onChange={(e) => setNote(e.target.value)} error={errors.field("note")} maxLength={200} autoComplete="off" />
        <p className="text-xs text-text-muted">{tDynamic(`suppliers.${kind}_hint`)}</p>
        <FormAlert text={errors.form} />
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

/** Reversing a payment, refund or opening balance entered by mistake, once, with its reason. The owner's act. */
export function SupplierReverseDialog({ entry, onDone, onClose }: { entry: SupplierEntry; onDone: (e: SupplierEntry) => void; onClose: () => void }) {
  const client = useClient();
  const { t, tDynamic, errorText } = useLocale();
  const [reason, setReason] = useState("");
  const { error, busy, run } = useSupplierAct(onDone);
  const errors = formErrors(error, errorText);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    void run(() => client.suppliers.reverse(entry.id, reason));
  };

  return (
    <Dialog title={t("suppliers.reverse_title", { kind: tDynamic(`suppliers.kind.${entry.kind}`) })} onClose={onClose}>
      <form className="space-y-3" onSubmit={submit}>
        <p className="text-sm">{t("suppliers.reverse_hint")}</p>
        <TextField label={t("suppliers.reason")} value={reason} onChange={(e) => setReason(e.target.value)} error={errors.field("reason")} maxLength={200} required autoComplete="off" />
        <FormAlert text={errors.form} />
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || reason.trim() === ""}>
            {t("suppliers.reverse")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
