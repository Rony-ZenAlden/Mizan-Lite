import { useCallback, useEffect, useState, type FormEvent } from "react";
import { useClient } from "@/api/ClientContext";
import type { OwnerEvent } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatCountdown } from "@/i18n/numbers";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { formatDateTime } from "@/i18n/time";
import { PinField } from "@/ui/Field";

/** How many history entries the screen shows. */
export const HISTORY_LIMIT = 100;

export function OwnerScreen() {
  const client = useClient();
  const { status, requestPin, refresh } = useOwner();
  const { t, tDynamic, errorText } = useLocale();
  const [events, setEvents] = useState<OwnerEvent[]>([]);
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [message, setMessage] = useState<{ tone: "danger" | "success"; text: string } | null>(null);

  const loadEvents = useCallback(async () => {
    try {
      setEvents(await client.owner.events(HISTORY_LIMIT));
    } catch (e) {
      setMessage({ tone: "danger", text: errorText(e) });
    }
  }, [client, errorText]);

  useEffect(() => {
    void loadEvents();
  }, [loadEvents]);

  const changePin = async (event: FormEvent) => {
    event.preventDefault();
    if (next !== again) {
      setMessage({ tone: "danger", text: t("pin.mismatch") });
      return;
    }
    try {
      await client.owner.changePin({ currentPin: current, newPin: next });
      setCurrent("");
      setNext("");
      setAgain("");
      setMessage({ tone: "success", text: t("owner.changed") });
    } catch (e) {
      setMessage({ tone: "danger", text: errorText(e) });
    } finally {
      await Promise.all([refresh(), loadEvents()]);
    }
  };

  const enter = async () => {
    try {
      await requestPin();
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setMessage({ tone: "danger", text: errorText(e) });
    } finally {
      await loadEvents();
    }
  };

  const details = (e: OwnerEvent) =>
    [e.action ? tDynamic(`owner.action.${e.action}`) : "", e.before && e.after ? `${e.before} → ${e.after}` : e.after].filter(Boolean).join(" · ");

  return (
    <section className="max-w-3xl space-y-6">
      <h2 className="text-xl font-semibold">{t("owner.title")}</h2>

      <div className="flex items-center justify-between gap-4 rounded-md border border-border bg-surface-raised p-4">
        <p>
          {status.elevatedSeconds > 0
            ? t("owner.status_elevated", { time: formatCountdown(status.elevatedSeconds) })
            : status.lockedSeconds > 0
              ? t("owner.status_locked", { time: formatCountdown(status.lockedSeconds) })
              : t("owner.status_off")}
        </p>
        {status.elevatedSeconds === 0 ? (
          <Button variant="primary" onClick={() => void enter()} disabled={status.lockedSeconds > 0}>
            {t("owner.enter")}
          </Button>
        ) : null}
      </div>

      {message ? <Alert tone={message.tone} title={message.text} /> : null}

      <form onSubmit={changePin} className="space-y-3 rounded-md border border-border bg-surface-raised p-4">
        <h3 className="font-semibold">{t("owner.change_title")}</h3>
        <PinField label={t("owner.current_pin")} value={current} onChange={(e) => setCurrent(e.target.value)} required />
        <PinField label={t("owner.new_pin")} value={next} onChange={(e) => setNext(e.target.value)} required />
        <PinField label={t("owner.new_pin_confirm")} value={again} onChange={(e) => setAgain(e.target.value)} required />
        <Button variant="primary" type="submit">
          {t("owner.change_submit")}
        </Button>
      </form>

      <div className="space-y-2">
        <h3 className="font-semibold">{t("owner.history_title")}</h3>
        {events.length === 0 ? (
          <p className="text-text-muted">{t("owner.history_empty")}</p>
        ) : (
          <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
            <table className="w-full text-sm">
              <thead className="bg-surface text-text-muted">
                <tr>
                  <th className="p-2 text-start">{t("owner.col.time")}</th>
                  <th className="p-2 text-start">{t("owner.col.event")}</th>
                  <th className="p-2 text-start">{t("owner.col.details")}</th>
                </tr>
              </thead>
              <tbody>
                {events.map((e) => (
                  <tr key={e.id} className="border-t border-border">
                    <td className="p-2">
                      <bdi dir="ltr">{formatDateTime(e.occurredAt)}</bdi>
                    </td>
                    <td className="p-2">{tDynamic(`owner.event.${e.kind}`)}</td>
                    <td className="p-2">
                      <bdi>{details(e)}</bdi>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </section>
  );
}
