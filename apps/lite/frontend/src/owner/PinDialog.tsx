import { useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { OwnerStatus } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { Dialog } from "@/ui/Dialog";
import { PinField, TextField } from "@/ui/Field";

type View = { kind: "pin" } | { kind: "recover" } | { kind: "new-code"; code: string };

/**
 * The owner PIN prompt, and — behind "Forgot the PIN?" — recovery with the recovery code.
 *
 * Recovery ends back on the PIN view: recovering sets a new PIN, it does not enter owner mode, so the act
 * that asked still needs the new PIN typed.
 */
export function PinDialog({ onElevated, onCancel }: { onElevated: (s: OwnerStatus) => void; onCancel: () => void }) {
  const client = useClient();
  const { t, errorText } = useLocale();
  const [view, setView] = useState<View>({ kind: "pin" });
  const [pin, setPin] = useState("");
  const [code, setCode] = useState("");
  const [newPin, setNewPin] = useState("");
  const [newPinAgain, setNewPinAgain] = useState("");
  const [written, setWritten] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submitPin = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onElevated(await client.owner.elevate(pin));
    } catch (e) {
      setError(errorText(e));
      setPin("");
    } finally {
      setBusy(false);
    }
  };

  const submitRecovery = async (event: FormEvent) => {
    event.preventDefault();
    if (newPin !== newPinAgain) {
      setError(t("pin.mismatch"));
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const result = await client.owner.recover({ recoveryCode: code, newPin });
      setView({ kind: "new-code", code: result.recoveryCode });
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  if (view.kind === "new-code") {
    return (
      <Dialog title={t("recover.done_title")} onClose={() => undefined}>
        <p className="text-sm">{t("recover.done_body")}</p>
        <p dir="ltr" className="rounded-md bg-surface p-3 text-center font-mono text-lg tracking-widest">
          {view.code}
        </p>
        <Checkbox label={t("recovery.confirm")} checked={written} onChange={(e) => setWritten(e.target.checked)} />
        <div className="flex justify-end gap-2">
          <Button
            variant="primary"
            disabled={!written}
            onClick={() => {
              setView({ kind: "pin" });
              setWritten(false);
            }}
          >
            {t("action.close")}
          </Button>
        </div>
      </Dialog>
    );
  }

  if (view.kind === "recover") {
    return (
      <Dialog title={t("recover.title")} onClose={onCancel}>
        <form className="space-y-3" onSubmit={submitRecovery}>
          <TextField label={t("recover.code")} value={code} onChange={(e) => setCode(e.target.value)} dir="ltr" autoComplete="off" required />
          <PinField label={t("recover.new_pin")} value={newPin} onChange={(e) => setNewPin(e.target.value)} required />
          <PinField label={t("recover.new_pin_confirm")} value={newPinAgain} onChange={(e) => setNewPinAgain(e.target.value)} required />
          {error ? (
            <p role="alert" className="text-sm text-danger">
              {error}
            </p>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button onClick={onCancel}>{t("action.cancel")}</Button>
            <Button variant="primary" type="submit" disabled={busy}>
              {t("recover.submit")}
            </Button>
          </div>
        </form>
      </Dialog>
    );
  }

  return (
    <Dialog title={t("pin.title")} onClose={onCancel}>
      <form className="space-y-3" onSubmit={submitPin}>
        <p className="text-sm text-text-muted">{t("pin.body")}</p>
        <PinField label={t("pin.label")} value={pin} onChange={(e) => setPin(e.target.value)} required />
        {error ? (
          <p role="alert" className="text-sm text-danger">
            {error}
          </p>
        ) : null}
        <div className="flex items-center justify-between gap-2">
          <button
            type="button"
            className="text-sm text-primary underline"
            onClick={() => {
              setError(null);
              setView({ kind: "recover" });
            }}
          >
            {t("pin.forgot")}
          </button>
          <div className="flex gap-2">
            <Button onClick={onCancel}>{t("action.cancel")}</Button>
            <Button variant="primary" type="submit" disabled={busy || pin === ""}>
              {t("action.confirm")}
            </Button>
          </div>
        </div>
      </form>
    </Dialog>
  );
}
