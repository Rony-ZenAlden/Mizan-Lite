import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState, type FormEvent } from "react";
import { SESSION_KEY } from "@/app/gates/AuthGate";
import { usePreferences, useTranslation } from "@/app/providers/PreferencesProvider";
import { login, isBindingError, type SessionInfo } from "@/lib/wails";
import { Alert, Button, Checkbox, Input, Select } from "@/shared/ui";
import type { Locale } from "@/i18n/messages";

/**
 * The sign-in screen.
 *
 * Everything it can go wrong with — unknown user, wrong password, deactivated account, a
 * throttled attempt — arrives as a BindingError carrying a CODE and parameters, never prose
 * (§22.2). All of them render through the catalogue, so the message a customer reads is in
 * their own language.
 *
 * Note what is NOT distinguished: the backend returns the same code for an unknown user and a
 * wrong password (1.2). Telling them apart would be a username oracle, turning an untargeted
 * password spray into a targeted one. The screen shows one message for both, deliberately.
 */
export function LoginScreen() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  /*
   * Checked by default (10.17).
   *
   * Mizan runs on a shop's own machine, usually with one person at it all day. Unchecked meant
   * the 60-minute idle timeout applied to somebody who steps away to serve a customer — so the
   * common case was being signed out mid-shift, and "stay signed in" was a box nobody knew to
   * tick.
   *
   * This changes a DEFAULT, not a capability: the box is still there, and a shared machine
   * unticks it. What it does not do is put a credential in JavaScript — the session token stays
   * in the Go process (1.5 D3), and what survives a restart is a row in the local database that
   * only the Go side can present.
   */
  const [remember, setRemember] = useState(true);

  const signIn = useMutation<SessionInfo, unknown, void>({
    mutationFn: () => login(username, password, remember),
    onSuccess: (session) => {
      // Seeded rather than invalidated: the answer is in hand, and refetching would put an
      // avoidable round trip between "signed in" and the first screen appearing.
      queryClient.setQueryData(SESSION_KEY, session);
    },
  });

  const wait = useRetryCountdown(signIn.error);
  const busy = signIn.isPending;
  const blocked = wait > 0;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (busy || blocked) return;
    signIn.mutate();
  }

  return (
    <div className="flex h-full items-center justify-center p-6">
      <form
        onSubmit={submit}
        className="flex w-full max-w-sm flex-col gap-4 card bg-surface-raised p-6"
        aria-labelledby="login-title"
      >
        <div className="flex items-start justify-between gap-4">
          <div>
            <h1 id="login-title" className="text-lg font-semibold text-text">
              {t("app.title")}
            </h1>
            <p className="mt-1 text-sm text-text-muted">{t("auth.login.subtitle")}</p>
          </div>
          {/* The language picker is on the LOGIN screen, not only inside the app: a user who
              cannot read the interface cannot sign in to change it. */}
          <LocalePicker />
        </div>

        {signIn.isError ? <LoginError error={signIn.error} wait={wait} /> : null}

        <Input
          label={t("auth.login.username")}
          name="username"
          value={username}
          onChange={(event) => setUsername(event.target.value)}
          autoComplete="username"
          autoFocus
          required
          error={fieldError(signIn.error, "username", t)}
        />
        <Input
          label={t("auth.login.password")}
          name="password"
          type="password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          autoComplete="current-password"
          required
          error={fieldError(signIn.error, "password", t)}
        />

        <Checkbox
          checked={remember}
          onCheckedChange={setRemember}
          label={t("auth.login.remember")}
        />

        <Button type="submit" disabled={busy || blocked} loading={busy}>
          {blocked ? t("auth.login.retryIn", { seconds: String(wait) }) : t("auth.login.submit")}
        </Button>
      </form>
    </div>
  );
}

function LocalePicker() {
  const { locale, availableLocales, setLocale } = usePreferences();
  const { t } = useTranslation();
  return (
    <Select
      value={locale}
      onValueChange={(next) => void setLocale(next as Locale)}
      ariaLabel={t("action.changeLanguage")}
      options={availableLocales.map((code) => ({
        // Languages are named in their own language: someone looking for Arabic is looking
        // for "العربية", not for "Arabic" spelled in a script they may not read.
        value: code,
        label: t(`locale.name.${code}`),
      }))}
    />
  );
}

function LoginError({ error, wait }: { error: unknown; wait: number }) {
  const { t } = useTranslation();
  if (!isBindingError(error)) {
    return (
      <Alert tone="danger" title={t("auth.login.failed")}>
        {t("app.unknown_error")}
      </Alert>
    );
  }
  return (
    <Alert tone={wait > 0 ? "warning" : "danger"} title={t("auth.login.failed")}>
      {t(error.messageKey, error.params)}
    </Alert>
  );
}

/**
 * Turns a throttle error's `retryAfter` into a live countdown.
 *
 * §13.1's lockout is "delay, never lock" — a shop must never be permanently shut out by
 * someone mistyping a password. The backend attaches how long to wait (1.3 D6), and showing it
 * is the difference between a user who waits eleven seconds and one who telephones support.
 *
 * "Try again later" without a number is not feedback; it is an apology.
 */
function useRetryCountdown(error: unknown): number {
  const initial = retryAfterSeconds(error);
  const [remaining, setRemaining] = useState(initial);

  useEffect(() => {
    setRemaining(initial);
  }, [initial, error]);

  useEffect(() => {
    if (remaining <= 0) return;
    const timer = window.setTimeout(() => setRemaining((value) => value - 1), 1000);
    return () => window.clearTimeout(timer);
  }, [remaining]);

  return remaining > 0 ? remaining : 0;
}

function retryAfterSeconds(error: unknown): number {
  if (!isBindingError(error)) return 0;
  const raw = error.params.retryAfter ?? error.params.retry_after;
  const seconds = Number.parseInt(raw ?? "", 10);
  return Number.isFinite(seconds) && seconds > 0 ? seconds : 0;
}

/**
 * Finds the per-field failure for an input, if the backend reported one.
 *
 * The envelope has carried `fields` since Step 0.11 and nothing consumed it until now. Per this
 * project's repeated experience, a seam's first real use is where it turns out to be wrong —
 * so this is deliberately exercised by a test rather than assumed to work.
 */
function fieldError(
  error: unknown,
  field: string,
  t: (key: string, params?: Record<string, string>) => string,
): string | undefined {
  if (!isBindingError(error)) return undefined;
  const found = error.fields.find((item) => item.field === field);
  return found ? t(found.messageKey, found.params) : undefined;
}
