import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { SESSION_KEY } from "@/app/gates/AuthGate";
import { SETUP_STATUS_KEY } from "@/app/gates/SetupGate";
import { usePreferences, useTranslation } from "@/app/providers/PreferencesProvider";
import { applySetup, isBindingError, login, type SetupOptions } from "@/lib/wails";
import { Alert, Button } from "@/shared/ui";
import type { Locale } from "@/i18n/messages";
import { StepFields } from "./steps";
import {
  canAdvance,
  initialState,
  STEPS,
  toInput,
  type Step,
  type WizardState,
} from "./wizardState";

/**
 * The first-run wizard (Addendum §C.2).
 *
 * # One object, submitted once (D5)
 *
 * Seven screens write into one draft; `Setup.Apply` is called exactly once at the end. The
 * backend does the whole of setup in a single transaction (1.9 D3), so there is no partial
 * state to hold — and a rejection returns the user to the offending step with everything they
 * typed intact.
 *
 * # It finishes by signing in (§5.4)
 *
 * `Apply` deliberately does not mint a session (1.9 D6). The wizard therefore logs in with the
 * credentials just typed, which PROVES THE ACCOUNT WORKS before the wizard closes. A configured
 * system nobody can enter is the worst possible outcome on a fresh install, and it is exactly
 * the failure that a wizard which ends at "Done!" would hide.
 */
export function SetupWizard({ options }: { options?: SetupOptions }) {
  const { t } = useTranslation();
  const { locale, setLocale } = usePreferences();
  const queryClient = useQueryClient();

  const [index, setIndex] = useState(0);
  const [state, setState] = useState<WizardState>(() => initialState(locale));
  // Clamped rather than asserted: noUncheckedIndexedAccess is on, and reaching for a
  // non-null assertion here would be silencing the compiler about the one thing it can
  // genuinely catch — an index walking off the end.
  const step: Step = STEPS[index] ?? STEPS[0];

  const finish = useMutation({
    mutationFn: async () => {
      await applySetup(toInput(state));
      // Signed in immediately after, with what the user typed. If this fails, the wizard says
      // so on its own last screen rather than dropping them at a login form with no context.
      return login(state.adminUsername, state.adminPassword, false);
    },
    onSuccess: (session) => {
      queryClient.setQueryData(SESSION_KEY, session);
      // The gate above re-asks and finds setup no longer required, which unmounts this wizard.
      void queryClient.invalidateQueries({ queryKey: SETUP_STATUS_KEY });
    },
  });

  function update(patch: Partial<WizardState>) {
    setState((current) => {
      const next = { ...current, ...patch };
      // The wizard's own chrome follows the language being chosen, immediately — §C.2 step 1
      // says the language choice "affects the wizard immediately", and a language picker that
      // does not change the screen it is on is a control nobody trusts.
      if (patch.locale && patch.locale !== current.locale) {
        void setLocale(patch.locale as Locale);
      }
      return next;
    });
  }

  const ready = canAdvance(step, state, options);
  const last = index === STEPS.length - 1;

  return (
    <div className="flex h-full items-center justify-center p-6">
      <section
        className="flex w-full max-w-2xl flex-col gap-5 rounded-lg border border-border bg-surface-raised p-6"
        aria-labelledby="wizard-title"
      >
        <header className="flex flex-col gap-1">
          <p className="text-xs uppercase tracking-wide text-text-muted">
            {t("setup.stepCounter", {
              current: String(index + 1),
              total: String(STEPS.length),
            })}
          </p>
          <h1 id="wizard-title" className="text-lg font-semibold text-text">
            {t(`setup.step.${step}.title`)}
          </h1>
          <p className="text-sm text-text-muted">{t(`setup.step.${step}.help`)}</p>
        </header>

        <ol className="flex flex-wrap gap-1" aria-label={t("setup.progress")}>
          {STEPS.map((name, position) => (
            <li
              key={name}
              aria-current={position === index ? "step" : undefined}
              className={
                "h-1.5 flex-1 rounded-full " +
                (position <= index ? "bg-primary" : "bg-surface-sunken")
              }
            />
          ))}
        </ol>

        {finish.isError ? <SubmitError error={finish.error} /> : null}

        <StepFields step={step} state={state} options={options} onChange={update} />

        <footer className="flex items-center justify-between gap-3 pt-2">
          <Button
            variant="ghost"
            onClick={() => setIndex((value) => Math.max(0, value - 1))}
            disabled={index === 0 || finish.isPending}
          >
            {t("setup.back")}
          </Button>
          {last ? (
            <Button
              onClick={() => finish.mutate()}
              disabled={!ready || finish.isPending}
              loading={finish.isPending}
            >
              {t("setup.finish")}
            </Button>
          ) : (
            <Button onClick={() => setIndex((value) => value + 1)} disabled={!ready}>
              {t("setup.next")}
            </Button>
          )}
        </footer>
      </section>
    </div>
  );
}

/**
 * A rejected submission.
 *
 * Rendered from the code, and the draft is untouched — the user goes back to the step that was
 * wrong with everything else still filled in.
 */
function SubmitError({ error }: { error: unknown }) {
  const { t } = useTranslation();
  return (
    <Alert tone="danger" title={t("setup.failed")}>
      {isBindingError(error) ? t(error.messageKey, error.params) : t("app.unknown_error")}
    </Alert>
  );
}
