import { useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { Supplier } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { formErrors } from "@/screens/stock/forms";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { TextField } from "@/ui/Field";

/** A supplier's name, phone, city and note (0.10.0). Names are unique the way a reader compares them, as customers' are. */
export function SupplierForm({ supplier, onSaved, onClose }: { supplier?: Supplier; onSaved: (s: Supplier) => void; onClose: () => void }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, errorText } = useLocale();
  const [name, setName] = useState(supplier?.name ?? "");
  const [phone, setPhone] = useState(supplier?.phone ?? "");
  const [city, setCity] = useState(supplier?.city ?? "");
  const [note, setNote] = useState(supplier?.note ?? "");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const errors = formErrors(error, errorText);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onSaved(
        await withOwner(() =>
          supplier
            ? client.suppliers.update({ id: supplier.id, rowVersion: supplier.rowVersion, name, phone, city, note })
            : client.suppliers.create({ name, phone, city, note }),
        ),
      );
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={supplier ? t("suppliers.edit_title", { name: supplier.name }) : t("suppliers.new_title")} onClose={onClose}>
      <form className="space-y-3" onSubmit={(e) => void submit(e)} aria-label={t("suppliers.form")}>
        <TextField label={t("suppliers.name")} value={name} onChange={(e) => setName(e.target.value)} error={errors.field("name")} maxLength={100} required autoComplete="off" />
        <TextField label={t("suppliers.phone")} value={phone} onChange={(e) => setPhone(e.target.value)} error={errors.field("phone")} inputMode="tel" dir="ltr" maxLength={20} autoComplete="off" />
        <TextField label={t("suppliers.city")} value={city} onChange={(e) => setCity(e.target.value)} error={errors.field("city")} maxLength={60} autoComplete="off" />
        <TextField label={t("suppliers.note")} value={note} onChange={(e) => setNote(e.target.value)} error={errors.field("note")} maxLength={200} autoComplete="off" />
        {errors.form ? (
          <p role="alert" className="text-sm text-danger">
            {errors.form}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.cancel")}</Button>
          <Button variant="primary" type="submit" disabled={busy || name.trim() === ""}>
            {t("action.save")}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
