import { useCallback, useEffect, useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { RateHistoryRow, RateState } from "@/api/client";
import { BindingError } from "@/api/envelope";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal } from "@/i18n/numbers";
import { formatAge, formatDateTime } from "@/i18n/time";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { useRate } from "@/rates/RateProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { TextField } from "@/ui/Field";
import { ratePair } from "@/i18n/figures";
import { CashNoteSection } from "./CashNoteSection";

/** How many past rates the history shows. */
export const RATE_HISTORY_LIMIT = 100;

/** The code a change beyond 20% is refused with until confirmed (fx/domain.CodeLargeChange). */
export const CODE_LARGE_CHANGE = "lite.fx.large_change";

/**
 * The exchange rate (L3 §10, §14): the rate in force with its age, the mode, the last internet check and any proposal
 * waiting for the owner, the manual rate, and the history. Every figure is Go's.
 */
export function RatesScreen() {
  const client = useClient();
  const { withOwner } = useOwner();
  const { rate, adopt } = useRate();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [history, setHistory] = useState<RateHistoryRow[] | null>(null);
  const [typed, setTyped] = useState("");
  const [note, setNote] = useState("");
  const [confirm, setConfirm] = useState<{ inForce: string; typed: string; percent: string } | null>(null);
  const [message, setMessage] = useState<{ tone: "danger" | "success"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  const loadHistory = useCallback(async () => {
    try {
      setHistory(await client.fx.history(RATE_HISTORY_LIMIT));
    } catch (e) {
      setMessage({ tone: "danger", text: errorText(e) });
    }
  }, [client, errorText]);

  useEffect(() => {
    void loadHistory();
  }, [loadHistory]);

  /** Runs an act that answers with the rate in force, then refreshes the history. */
  const run = async (act: () => Promise<RateState>, success?: string) => {
    setBusy(true);
    setMessage(null);
    try {
      adopt(await act());
      if (success) setMessage({ tone: "success", text: success });
      await loadHistory();
      return true;
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setMessage({ tone: "danger", text: errorText(e) });
      return false;
    } finally {
      setBusy(false);
    }
  };

  const save = async (confirmLargeChange: boolean) => {
    setBusy(true);
    setMessage(null);
    try {
      adopt(await withOwner(() => client.fx.setRate({ rate: typed, note, confirmLargeChange })));
      setTyped("");
      setNote("");
      setConfirm(null);
      setMessage({ tone: "success", text: t("rates.saved") });
      await loadHistory();
    } catch (e) {
      if (e instanceof BindingError && e.code === CODE_LARGE_CHANGE) {
        const params = e.apiError.params ?? {};
        setConfirm({ inForce: params.inForce ?? "", typed: params.typed ?? "", percent: params.percent ?? "" });
      } else if (!(e instanceof OwnerCancelled)) {
        setConfirm(null);
        setMessage({ tone: "danger", text: errorText(e) });
      }
    } finally {
      setBusy(false);
    }
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    void save(false);
  };

  const pair = (value: string, currency: string) => (
    <span>{t("rates.pair", ratePair(value, currency, locale, tDynamic))}</span>
  );
  const provider = (name: string) => tDynamic(`rates.provider.${name}`);
  const justNow = t("age.just_now");
  const last = rate?.hasFetch ? rate.lastFetch : null;

  return (
    <section className="space-y-6">
      <header>
        <h2 className="text-xl font-semibold">{t("rates.title")}</h2>
      </header>

      {message ? <Alert tone={message.tone} title={message.text} /> : null}

      {rate ? (
        <div className="space-y-2 rounded-lg border border-border bg-surface-raised p-5" data-testid="rate-in-force">
          {rate.set ? (
            <>
              <p className="text-3xl font-semibold">{pair(rate.rate, rate.localCurrency)}</p>
              <p className="text-sm text-text-muted">
                {rate.source === "fetched" && rate.hasFetch
                  ? t("rates.source.fetched", { provider: provider(rate.lastFetch.provider) })
                  : tDynamic(`rates.source.${rate.source}`, { provider: "" })}{" "}
                · {t("rates.recorded", { age: formatAge(rate.ageSeconds, locale, justNow) })}
              </p>
              {rate.stale ? <Alert tone="danger" title={t("rates.stale")}>{t("rates.stale_body")}</Alert> : null}
            </>
          ) : (
            <Alert tone="danger" title={t("rates.none")}>
              {t("rates.none_body")}
            </Alert>
          )}
        </div>
      ) : null}

      {rate ? (
        <div className="space-y-3 rounded-lg border border-border bg-surface-raised p-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p className="text-sm font-medium">
                {t("rates.mode")}: {tDynamic(`rates.mode.${rate.mode}`)}
              </p>
              <p className="text-xs text-text-muted">{tDynamic(`rates.mode_hint.${rate.mode}`)}</p>
            </div>
            <Button
              disabled={busy}
              onClick={() => void run(() => withOwner(() => client.fx.setMode(rate.mode === "automatic" ? "manual" : "automatic")))}
            >
              {rate.mode === "automatic" ? t("rates.switch_to_manual") : t("rates.switch_to_automatic")}
            </Button>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-3">
            <div className="space-y-1 text-sm">
              {last ? (
                <>
                  <p>{t("rates.last_check", { age: formatAge(last.ageSeconds, locale, justNow) })}</p>
                  {last.outcome === "failed" ? (
                    <p className="text-danger">{tDynamic(last.errorCode)}</p>
                  ) : (
                    <p>
                      {last.change
                        ? t("rates.last_check_rate", { rate: formatDecimal(last.rate, locale), provider: provider(last.provider), change: last.change })
                        : `${formatDecimal(last.rate, locale)} (${provider(last.provider)})`}{" "}
                      {tDynamic(`rates.outcome.${last.outcome}`)}
                    </p>
                  )}
                  <p className="text-xs text-text-muted">{t("rates.internet_differs")}</p>
                </>
              ) : null}
              {!rate.canFetch ? <p className="text-xs text-text-muted">{t("rates.no_provider")}</p> : null}
            </div>
            <Button disabled={busy || !rate.canFetch} onClick={() => void run(() => client.fx.refresh())}>
              {t("rates.update_now")}
            </Button>
          </div>

          {last && last.acceptable ? (
            <Alert tone="danger" title={t("rates.proposal_title", { rate: formatDecimal(last.rate, locale) })}>
              <p>{rate.set ? t("rates.proposal_body", { change: last.change }) : t("rates.proposal_first")}</p>
              <div className="mt-2">
                <Button variant="primary" disabled={busy} onClick={() => void run(() => withOwner(() => client.fx.acceptProposal(last.id)))}>
                  {t("rates.accept")}
                </Button>
              </div>
            </Alert>
          ) : null}
        </div>
      ) : null}

      <form className="space-y-3 rounded-lg border border-border bg-surface-raised p-5" onSubmit={submit}>
        <h3 className="font-semibold">{t("rates.set_title")}</h3>
        <div className="grid gap-3 sm:grid-cols-2">
          <TextField
            label={t("rates.rate_label", { currency: tDynamic(`currency.${rate?.localCurrency ?? "SYP"}`) })}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            hint={t("rates.set_hint")}
            inputMode="decimal"
            dir="ltr"
            required
          />
          <TextField label={t("rates.note")} value={note} onChange={(e) => setNote(e.target.value)} maxLength={200} />
        </div>
        <div className="flex justify-end">
          <Button variant="primary" type="submit" disabled={busy || typed === ""}>
            {t("rates.save")}
          </Button>
        </div>
      </form>

      <CashNoteSection />

      <div className="space-y-2">
        <h3 className="font-semibold">{t("rates.history_title")}</h3>
        {history && history.length === 0 ? <p className="text-text-muted">{t("rates.history_empty")}</p> : null}
        {history && history.length > 0 ? (
          <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
            <table className="w-full text-sm">
              <thead className="bg-surface text-text-muted">
                <tr>
                  <th className="p-2 text-start">{t("rates.col.when")}</th>
                  <th className="p-2 text-start">{t("rates.col.rate")}</th>
                  <th className="p-2 text-start">{t("rates.col.change")}</th>
                  <th className="p-2 text-start">{t("rates.col.source")}</th>
                  <th className="p-2 text-start">{t("rates.col.note")}</th>
                </tr>
              </thead>
              <tbody>
                {history.map((row) => (
                  <tr key={row.id} className="border-t border-border">
                    <td className="p-2">
                      <bdi dir="ltr">{formatDateTime(row.recordedAt)}</bdi>
                    </td>
                    <td className="p-2">
                      <bdi dir="ltr">{formatDecimal(row.rate, locale)}</bdi>
                    </td>
                    <td className="p-2">
                      <bdi dir="ltr">{row.change ? `${row.change}%` : "—"}</bdi>
                    </td>
                    <td className="p-2">
                      {row.source === "fetched" ? t("rates.source.fetched", { provider: provider(row.provider) }) : tDynamic(`rates.source.${row.source}`)}
                    </td>
                    <td className="p-2">{row.note}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </div>

      {confirm ? (
        <Dialog title={t("rates.confirm_title")} onClose={() => setConfirm(null)}>
          <p>{tDynamic(CODE_LARGE_CHANGE, confirm)}</p>
          <div className="flex justify-end gap-2">
            <Button onClick={() => setConfirm(null)}>{t("rates.confirm_keep")}</Button>
            <Button variant="primary" disabled={busy} onClick={() => void save(true)}>
              {t("rates.confirm_accept", { typed: formatDecimal(confirm.typed, locale) })}
            </Button>
          </div>
        </Dialog>
      ) : null}

    </section>
  );
}
