import { useCallback, useEffect, useRef, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { LossReport } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { formatDecimal, formatInteger } from "@/i18n/numbers";
import { formatDate } from "@/i18n/time";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Money } from "@/screens/sales/Money";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { TextField } from "@/ui/Field";
import { SpoilageDialog } from "./SpoilageDialog";

/**
 * Spoilage, damage and losses (0.10.0, the owner's request of 2026-09-24): stock written off as damaged, expired or
 * spoiled — recorded here — with everything else the shop lost over a period, at what the goods cost; and the goods that
 * arrived damaged from suppliers, which the supplier did not charge for and so are shown, not counted.
 *
 * Every figure is a cost, so with the PIN switch on the screen asks for the PIN to show them.
 */
export function LossesScreen() {
  const client = useClient();
  const { status, withOwner } = useOwner();
  const { t, tDynamic, errorText, locale } = useLocale();
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [report, setReport] = useState<LossReport | null>(null);
  const [hidden, setHidden] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [recording, setRecording] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setReport(await withOwner(() => client.reports.losses(from, to)));
      setHidden(false);
      setError(null);
    } catch (e) {
      if (e instanceof OwnerCancelled) setHidden(true);
      else setError(e);
    }
  }, [client, withOwner, from, to]);

  useEffect(() => {
    void load();
  }, [load]);

  // Owner mode ending takes the costs off the screen — when the PIN switch says they are the owner's (as Suppliers).
  const ownerMode = status.elevatedSeconds > 0;
  const wasOwner = useRef(ownerMode);
  const recheck = useCallback(async () => {
    try {
      await client.reports.losses(from, to);
    } catch {
      setReport(null);
      setRecording(false);
      setHidden(true);
    }
  }, [client, from, to]);
  useEffect(() => {
    if (wasOwner.current && !ownerMode) void recheck();
    wasOwner.current = ownerMode;
  }, [ownerMode, recheck]);

  const close = useCallback(() => setRecording(false), []);
  const recorded = useCallback(
    (message: string) => {
      setRecording(false);
      setNotice(message);
      void load();
    },
    [load],
  );

  const name = (nameAr: string, nameEn: string) => (locale === "en" && nameEn ? nameEn : nameAr);
  const reasonLabel = (reason: string) => (reason === "shortfall" ? t("losses.shortfall") : tDynamic(`stock.reason.${reason}`));
  const local = report?.localCurrency ?? "";

  if (hidden) {
    return (
      <section className="space-y-4">
        <h2 className="text-xl font-semibold">{t("losses.title")}</h2>
        <p className="text-text-muted">{t("losses.hidden")}</p>
        <Button variant="primary" onClick={() => void load()}>
          {t("losses.show")}
        </Button>
      </section>
    );
  }

  return (
    <section className="space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <h2 className="text-xl font-semibold">{t("losses.title")}</h2>
        <Button variant="primary" onClick={() => setRecording(true)}>
          {t("losses.record")}
        </Button>
      </header>
      {notice ? <Alert tone="success" title={notice} /> : null}
      {error ? <Alert tone="danger" title={errorText(error)} /> : null}

      <div className="flex flex-wrap items-end gap-4">
        <TextField label={t("losses.from")} type="date" value={from || report?.from || ""} onChange={(e) => setFrom(e.target.value)} dir="ltr" />
        <TextField label={t("losses.to")} type="date" value={to || report?.to || ""} onChange={(e) => setTo(e.target.value)} dir="ltr" />
        <p className="pb-2 text-xs text-text-muted">{t("losses.period_hint")}</p>
      </div>

      {report ? (
        <>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {report.byReason.map((r) => (
              <div key={r.reason} data-testid={`loss-total-${r.reason}`} className="rounded-lg border border-border bg-surface-raised p-4 text-sm">
                <p className="font-medium">{reasonLabel(r.reason)}</p>
                <p className="text-text-muted">{t("losses.lines_count", { count: formatInteger(r.lines, locale) })}</p>
                <p className="mt-1 font-semibold">
                  <Money value={r.value.usd} currency="USD" />
                </p>
                {local ? <Money value={r.value.local} currency={local} className="text-text-muted" /> : null}
              </div>
            ))}
            <div data-testid="loss-total" className="rounded-lg border border-border bg-surface-raised p-4 text-sm">
              <p className="font-medium">{t("losses.total")}</p>
              <p className="mt-1 text-lg font-semibold">
                <Money value={report.total.usd} currency="USD" />
              </p>
              {local ? <Money value={report.total.local} currency={local} className="text-text-muted" /> : null}
              {report.total.unconverted > 0 ? (
                <p className="mt-1 text-xs text-text-muted">{t("losses.unconverted", { count: formatInteger(report.total.unconverted, locale) })}</p>
              ) : null}
            </div>
          </div>

          {report.lines.length === 0 ? <p className="text-text-muted">{t("losses.empty")}</p> : null}
          {report.lines.length > 0 ? (
            <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
              <table className="w-full text-sm">
                <thead className="bg-surface text-text-muted">
                  <tr>
                    <th className="p-2 text-start">{t("losses.col.date")}</th>
                    <th className="p-2 text-start">{t("losses.col.product")}</th>
                    <th className="p-2 text-start">{t("losses.col.reason")}</th>
                    <th className="p-2 text-start">{t("losses.col.quantity")}</th>
                    <th className="p-2 text-start">{t("losses.col.value_usd")}</th>
                    {local ? <th className="p-2 text-start">{t("losses.col.value_local")}</th> : null}
                    <th className="p-2 text-start">{t("losses.col.note")}</th>
                  </tr>
                </thead>
                <tbody>
                  {report.lines.map((l) => (
                    <tr key={l.movementId} className="border-t border-border" data-testid="loss-line">
                      <td className="p-2">
                        <bdi dir="ltr">{formatDate(l.businessDate)}</bdi>
                      </td>
                      <td className="p-2">{name(l.nameAr, l.nameEn)}</td>
                      <td className="p-2">{reasonLabel(l.reason)}</td>
                      <td className="p-2">
                        <bdi dir="ltr">{formatDecimal(l.quantity, locale)}</bdi> {tDynamic(`uom.${l.unitCode}`)}
                      </td>
                      <td className="p-2">
                        <Money value={l.value.usd} currency="USD" />
                      </td>
                      {local ? (
                        <td className="p-2">
                          <Money value={l.value.local} currency={local} />
                        </td>
                      ) : null}
                      <td className="p-2 text-xs">{l.note}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}

          {report.arrival.length > 0 ? (
            <section className="space-y-2" aria-label={t("losses.arrival_title")}>
              <h3 className="font-semibold">{t("losses.arrival_title")}</h3>
              <p className="text-xs text-text-muted">{t("losses.arrival_hint")}</p>
              <div className="overflow-x-auto rounded-md border border-border bg-surface-raised">
                <table className="w-full text-sm">
                  <thead className="bg-surface text-text-muted">
                    <tr>
                      <th className="p-2 text-start">{t("losses.col.date")}</th>
                      <th className="p-2 text-start">{t("losses.col.supplier")}</th>
                      <th className="p-2 text-start">{t("losses.col.purchase")}</th>
                      <th className="p-2 text-start">{t("losses.col.product")}</th>
                      <th className="p-2 text-start">{t("losses.col.damaged")}</th>
                      <th className="p-2 text-start">{t("losses.col.not_charged")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {report.arrival.map((a, i) => (
                      <tr key={`${a.purchaseNo}-${a.productId}-${i}`} className="border-t border-border" data-testid="arrival-line">
                        <td className="p-2">
                          <bdi dir="ltr">{formatDate(a.businessDate)}</bdi>
                        </td>
                        <td className="p-2">{a.supplierName}</td>
                        <td className="p-2">
                          <bdi dir="ltr">{formatInteger(a.purchaseNo, locale)}</bdi>
                        </td>
                        <td className="p-2">{name(a.nameAr, a.nameEn)}</td>
                        <td className="p-2">
                          <bdi dir="ltr">{formatDecimal(a.damaged, locale)}</bdi> {tDynamic(`uom.${a.unitCode}`)}
                        </td>
                        <td className="p-2">
                          <Money value={a.value} currency={a.currency} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          ) : null}
        </>
      ) : null}

      {recording ? <SpoilageDialog onDone={recorded} onClose={close} /> : null}
    </section>
  );
}
