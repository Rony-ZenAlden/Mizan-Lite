import { useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Customer } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formErrors } from "@/screens/stock/forms";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { TextField } from "@/ui/Field";

/**
 * A customer's name, phone and note (Q-L5.9). Anyone at the counter may create or edit one. Names are unique the way a
 * reader compares them — a second أبو محمد needs a nickname — and Go says so under the name.
 */
export function CustomerFields({ customer, onSaved, onCancel }: { customer?: Customer; onSaved: (c: Customer) => void; onCancel: () => void }) {
  const client = useClient();
  const { t, errorText } = useLocale();
  const [name, setName] = useState(customer?.name ?? "");
  const [phone, setPhone] = useState(customer?.phone ?? "");
  const [note, setNote] = useState(customer?.note ?? "");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const errors = formErrors(error, errorText);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    event.stopPropagation();
    setBusy(true);
    setError(null);
    try {
      onSaved(
        customer
          ? await client.customers.update({ id: customer.id, rowVersion: customer.rowVersion, name, phone, note })
          : await client.customers.create({ name, phone, note }),
      );
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="space-y-3" onSubmit={(e) => void submit(e)} aria-label={t("customers.form")}>
      <TextField label={t("customers.name")} value={name} onChange={(e) => setName(e.target.value)} error={errors.field("name")} hint={t("customers.name_hint")} maxLength={100} required autoComplete="off" />
      <TextField label={t("customers.phone")} value={phone} onChange={(e) => setPhone(e.target.value)} error={errors.field("phone")} inputMode="tel" dir="ltr" maxLength={20} autoComplete="off" />
      <TextField label={t("customers.note")} value={note} onChange={(e) => setNote(e.target.value)} error={errors.field("note")} maxLength={200} autoComplete="off" />
      {errors.form ? (
        <p role="alert" className="text-sm text-danger">
          {errors.form}
        </p>
      ) : null}
      <div className="flex justify-end gap-2">
        <Button onClick={onCancel}>{t("action.cancel")}</Button>
        <Button variant="primary" type="submit" disabled={busy || name.trim() === ""}>
          {t("action.save")}
        </Button>
      </div>
    </form>
  );
}

export function CustomerForm({ customer, onSaved, onClose }: { customer?: Customer; onSaved: (c: Customer) => void; onClose: () => void }) {
  const { t } = useLocale();
  return (
    <Dialog title={customer ? t("customers.edit_title", { name: customer.name }) : t("customers.new_title")} onClose={onClose}>
      <CustomerFields customer={customer} onSaved={onSaved} onCancel={onClose} />
    </Dialog>
  );
}
