import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { SESSION_KEY } from "@/app/gates/AuthGate";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { changeMyPassword, me } from "@/lib/wails";
import { Alert, Button, Input } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";

/**
 * Changing your own password.
 *
 * # The current password is required (1.11 D2)
 *
 * The session already proves who is asking, so the field looks redundant. It is not: the threat
 * is an UNATTENDED TERMINAL, which is the normal state of a shop counter. A session left open is
 * not consent to change the credential that outlives it.
 *
 * This screen is also what closes the `mustChange` dead end Step 1.10 left. A user whose
 * password was set by an administrator lands here and can go no further until they replace it.
 */
export function ChangePasswordScreen({ forced = false }: { forced?: boolean }) {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");

  const change = useMutation({
    mutationFn: () => changeMyPassword(current, next),
    onSuccess: async () => {
      setCurrent("");
      setNext("");
      setConfirm("");
      // Refetched rather than assumed: `mustChange` is now false on the backend, and the gate
      // above reads it from this query. Guessing the new value here would let the two disagree.
      queryClient.setQueryData(SESSION_KEY, await me());
    },
  });

  const mismatch = confirm !== "" && next !== confirm;
  const ready = current !== "" && next !== "" && next === confirm;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!ready || change.isPending) return;
    change.mutate();
  }

  return (
    <div className={forced ? "flex h-full items-center justify-center p-6" : "p-0"}>
      <form
        onSubmit={submit}
        className="flex w-full max-w-sm flex-col gap-4"
        aria-labelledby="change-password-title"
      >
        <h2 id="change-password-title" className="text-base font-medium text-text">
          {t("account.password.title")}
        </h2>

        {forced ? (
          <Alert tone="warning" title={t("auth.mustChange.title")}>
            {t("account.password.forced")}
          </Alert>
        ) : null}
        {change.isError ? (
          <Alert tone="danger" title={t("account.password.failed")}>{errorText(change.error)}</Alert>
        ) : null}
        {change.isSuccess ? (
          <Alert tone="success" title={t("account.password.done")}>
            {t("account.password.done.help")}
          </Alert>
        ) : null}

        <Input
          label={t("account.password.current")}
          type="password"
          value={current}
          onChange={(event) => setCurrent(event.target.value)}
          autoComplete="current-password"
          hint={t("account.password.current.hint")}
          required
        />
        <Input
          label={t("account.password.new")}
          type="password"
          value={next}
          onChange={(event) => setNext(event.target.value)}
          autoComplete="new-password"
          required
        />
        <Input
          label={t("account.password.confirm")}
          type="password"
          value={confirm}
          onChange={(event) => setConfirm(event.target.value)}
          autoComplete="new-password"
          error={mismatch ? t("setup.field.adminPassword.mismatch") : undefined}
          required
        />

        <Button type="submit" disabled={!ready} loading={change.isPending}>
          {t("account.password.submit")}
        </Button>
      </form>
    </div>
  );
}
