import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { useClient } from "@/api/ClientContext";
import { BindingError } from "@/api/envelope";
import { useLocale } from "@/i18n/LocaleProvider";
import { LOCALES, type Locale } from "@/i18n/messages";
import { normaliseNumber } from "@/i18n/numbers";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { PinField, TextField } from "@/ui/Field";
import { ratePair } from "@/i18n/figures";
import { Spinner } from "@/ui/Spinner";

type View = { kind: "checking" } | { kind: "form" } | { kind: "recovery"; code: string } | { kind: "done" } | { kind: "error"; error: unknown };

/**
 * First run (L1 §8): until the shop has a name and an owner PIN, nothing else renders.
 *
 * A gate, not a route — after setup it cannot be navigated to. Whether it is needed is asked of Go, which
 * reads the data rather than a flag.
 */
export function FirstRunGate({ children }: { children: ReactNode }) {
  const client = useClient();
  const { t, errorText } = useLocale();
  const [view, setView] = useState<View>({ kind: "checking" });

  useEffect(() => {
    let cancelled = false;
    client.app
      .firstRunStatus()
      .then((s) => {
        if (!cancelled) setView(s.complete ? { kind: "done" } : { kind: "form" });
      })
      .catch((error) => {
        if (!cancelled) setView({ kind: "error", error });
      });
    return () => {
      cancelled = true;
    };
  }, [client]);

  switch (view.kind) {
    case "done":
      return <>{children}</>;
    case "checking":
      return (
        <main className="flex h-full items-center justify-center">
          <Spinner label={t("state.loading")} />
        </main>
      );
    case "error":
      return (
        <main className="flex h-full items-center justify-center p-8">
          <Alert tone="danger" title={errorText(view.error)} />
        </main>
      );
    case "recovery":
      return <RecoveryStep code={view.code} onFinish={() => setView({ kind: "done" })} />;
    default:
      return <FirstRunForm onComplete={(code) => setView({ kind: "recovery", code })} />;
  }
}

function fieldError(error: unknown, field: string, errorText: (e: unknown) => string): string | undefined {
  if (error instanceof BindingError && error.apiError.fields?.some((f) => f.field === field)) return errorText(error);
  return undefined;
}

function FirstRunForm({ onComplete }: { onComplete: (recoveryCode: string) => void }) {
  const client = useClient();
  const { t, tDynamic, errorText, locale, adoptStored } = useLocale();
  const [shopName, setShopName] = useState("");
  const [pin, setPin] = useState("");
  const [pinAgain, setPinAgain] = useState("");
  const [rate, setRate] = useState("");
  const [fetchedFrom, setFetchedFrom] = useState("");
  const [fetching, setFetching] = useState(false);
  const [localCurrency, setLocalCurrency] = useState("SYP");

  // The currency the rate prices is data (DESIGN C8): read from Go, SYP until it answers.
  useEffect(() => {
    let cancelled = false;
    client.fx
      .current()
      .then((r) => {
        if (!cancelled && r.localCurrency) setLocalCurrency(r.localCurrency);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [client]);
  const [mismatch, setMismatch] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (pin !== pinAgain) {
      setMismatch(true);
      return;
    }
    setMismatch(false);
    setBusy(true);
    try {
      const result = await client.app.completeFirstRun({ shopName, locale, pin, rate });
      onComplete(result.recoveryCode);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  // Fetched from the internet only when asked, and only into the field: the owner still confirms it by continuing.
  const fetchRate = async () => {
    setFetching(true);
    setError(null);
    try {
      const quote = await client.fx.fetchQuote();
      setRate(quote.rate);
      setFetchedFrom(tDynamic(`rates.provider.${quote.provider}`));
    } catch (e) {
      setError(e);
    } finally {
      setFetching(false);
    }
  };

  const typedRate = normaliseNumber(rate);
  // Read back without trailing zeros, so "15.000" — fifteen thousand to many shopkeepers — shows as the fifteen Go will
  // store (L3 H1). Text, not arithmetic.
  const readback = typedRate.ok && typedRate.value.includes(".") ? typedRate.value.replace(/0+$/, "").replace(/\.$/, "") : typedRate.ok ? typedRate.value : "";
  const rateError = fieldError(error, "rate", errorText) ?? (rate !== "" && !typedRate.ok ? tDynamic(typedRate.code) : undefined);
  const shopError = fieldError(error, "shopName", errorText);
  const pinError = mismatch ? t("pin.mismatch") : fieldError(error, "pin", errorText);
  const otherError =
    error && !shopError && !fieldError(error, "pin", errorText) && !fieldError(error, "rate", errorText) ? errorText(error) : null;

  return (
    <main className="flex h-full items-center justify-center overflow-auto p-8">
      <form onSubmit={submit} className="w-full max-w-lg space-y-5 rounded-lg border border-border bg-surface-raised p-8">
        <header className="space-y-1">
          <h1 className="text-2xl font-semibold">{t("firstrun.title")}</h1>
          <p className="text-sm text-text-muted">{t("firstrun.intro")}</p>
        </header>

        <TextField label={t("firstrun.shop_name")} value={shopName} onChange={(e) => setShopName(e.target.value)} error={shopError} required maxLength={100} />

        <fieldset className="space-y-2">
          <legend className="text-sm font-medium">{t("firstrun.language")}</legend>
          <div className="flex gap-2">
            {LOCALES.map((option: Locale) => (
              <button
                key={option}
                type="button"
                lang={option}
                aria-pressed={option === locale}
                onClick={() => adoptStored(option)}
                className={`flex-1 rounded-md border px-4 py-3 ${option === locale ? "border-primary bg-primary text-primary-fg" : "border-border"}`}
              >
                {t(option === "ar" ? "language.ar" : "language.en")}
              </button>
            ))}
          </div>
        </fieldset>

        <PinField label={t("firstrun.pin")} hint={t("firstrun.pin_hint")} value={pin} onChange={(e) => setPin(e.target.value)} error={pinError} required />
        <PinField label={t("firstrun.pin_confirm")} value={pinAgain} onChange={(e) => setPinAgain(e.target.value)} required />

        <div className="space-y-2">
          <TextField
            label={t("firstrun.rate", { currency: tDynamic(`currency.${localCurrency}`) })}
            value={rate}
            onChange={(e) => {
              setRate(e.target.value);
              setFetchedFrom("");
            }}
            error={rateError}
            hint={fetchedFrom ? t("firstrun.rate_fetched", { provider: fetchedFrom }) : undefined}
            inputMode="decimal"
            dir="ltr"
            required
          />
          {typedRate.ok ? (
            <p className="text-sm font-medium" data-testid="rate-readback">
              {t("firstrun.rate_readback", ratePair(readback, localCurrency, locale, tDynamic))}
            </p>
          ) : null}
          <Button onClick={() => void fetchRate()} disabled={fetching}>
            {t("firstrun.rate_fetch")}
          </Button>
        </div>

        {otherError ? <Alert tone="danger" title={otherError} /> : null}

        <Button variant="primary" type="submit" disabled={busy} className="w-full">
          {t("firstrun.submit")}
        </Button>
      </form>
    </main>
  );
}

function RecoveryStep({ code, onFinish }: { code: string; onFinish: () => void }) {
  const { t } = useLocale();
  const [written, setWritten] = useState(false);
  return (
    <main className="flex h-full items-center justify-center p-8">
      <section className="w-full max-w-lg space-y-5 rounded-lg border border-border bg-surface-raised p-8">
        <h1 className="text-2xl font-semibold">{t("recovery.title")}</h1>
        <p className="text-sm">{t("recovery.body")}</p>
        <p dir="ltr" data-testid="recovery-code" className="rounded-md bg-surface p-4 text-center font-mono text-2xl tracking-widest">
          {code}
        </p>
        <Checkbox label={t("recovery.confirm")} checked={written} onChange={(e) => setWritten(e.target.checked)} />
        <Button variant="primary" disabled={!written} onClick={onFinish} className="w-full">
          {t("recovery.finish")}
        </Button>
      </section>
    </main>
  );
}
