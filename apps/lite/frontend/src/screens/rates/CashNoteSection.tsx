import { useEffect, useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { CashNote } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { formErrors, typedAmount } from "@/screens/stock/forms";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { TextField } from "@/ui/Field";

/**
 * Cash rounding (L4 §3.2, Q-L4.1): the smallest note a total in pounds is rounded to — 500 by default. Beside the rate,
 * because both change what every customer pays; changing it needs the owner.
 */
export function CashNoteSection() {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [note, setNote] = useState<CashNote | null>(null);
  const [typed, setTyped] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [saved, setSaved] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    client.till
      .cashNote()
      .then((n) => {
        if (!cancelled) setNote(n);
      })
      .catch((e) => {
        if (!cancelled) setError(e);
      });
    return () => {
      cancelled = true;
    };
  }, [client]);

  const code = typedAmount(typed);
  const errors = formErrors(error, errorText);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      setNote(await withOwner(() => client.till.setCashNote(typed)));
      setTyped("");
      setSaved(true);
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="space-y-3 rounded-lg border border-border bg-surface-raised p-5" onSubmit={(e) => void submit(e)} data-testid="cash-note">
      <h3 className="font-semibold">{t("cash_note.title")}</h3>
      {note ? (
        <p>
          {t("cash_note.current", {
            note: formatDecimal(note.note, locale),
            currency: tDynamic(`currency.${note.currency}`),
          })}
        </p>
      ) : null}
      <p className="text-xs text-text-muted">{t("cash_note.hint")}</p>
      {saved ? <Alert tone="success" title={t("cash_note.saved")} /> : null}
      <div className="grid gap-3 sm:grid-cols-2">
        <TextField
          label={t("cash_note.label")}
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          error={(code ? tDynamic(code) : undefined) ?? errors.field("cashNote") ?? errors.form ?? undefined}
          inputMode="numeric"
          dir="ltr"
          autoComplete="off"
        />
      </div>
      <div className="flex justify-end">
        <Button variant="primary" type="submit" disabled={busy || typed === "" || Boolean(code)}>
          {t("cash_note.save")}
        </Button>
      </div>
    </form>
  );
}
