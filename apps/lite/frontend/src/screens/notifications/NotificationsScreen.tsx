import { useEffect, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { useNotifications } from "@/alerts/NotificationProvider";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Money } from "@/screens/sales/Money";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Spinner } from "@/ui/Spinner";
import { RepriceDialog } from "./RepriceDialog";

/**
 * The notification centre (the owner's request, 2026-09-23): the shop's own backups, prices the exchange rate has left
 * behind, what the shop is worth in dollars, and low stock — each with the step that deals with it.
 *
 * Opening it marks everything read: the bell's job is to say "look here", and once someone has looked it has done it.
 * What was unread when the centre opened keeps a "new" mark until the person leaves, so marking it read does not also
 * hide which it was.
 */
export function NotificationsScreen() {
  const { alerts, isUnread, markAllRead, refresh } = useNotifications();
  const { requestPin } = useOwner();
  const { t, tDynamic, locale } = useLocale();
  const [repricing, setRepricing] = useState(false);
  const [repriced, setRepriced] = useState<number | null>(null);
  const [fresh, setFresh] = useState<ReadonlySet<string>>(() => new Set());

  useEffect(() => {
    if (!alerts) return;
    const unread = alerts.notifications.filter(isUnread).map((n) => n.key);
    if (unread.length > 0) setFresh((prev) => new Set([...prev, ...unread]));
    markAllRead();
  }, [alerts, isUnread, markAllRead]);

  if (!alerts) return <Spinner label={t("state.loading")} />;

  const name = (i: { nameAr: string; nameEn: string }) => (locale === "en" && i.nameEn ? i.nameEn : i.nameAr);
  const isNew = (key: string) => (fresh.has(key) ? <NewMark /> : null);
  const collecting = !alerts.hasCapital && !alerts.ownerHidden && alerts.historyDays < alerts.historyNeeded;
  const nothing =
    alerts.backup === "" && alerts.stale.length === 0 && !alerts.hasCapital && alerts.lowStock.length === 0 && !alerts.ownerHidden;

  const unlock = async () => {
    try {
      await requestPin();
      await refresh();
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) throw e;
    }
  };

  return (
    <section className="mx-auto max-w-4xl space-y-4" data-testid="notifications-screen">
      <h2 className="text-xl font-semibold">{t("alerts.title")}</h2>
      {repriced !== null ? <Alert tone="success" title={t("reprice.done", { count: String(repriced) })} testId="reprice-done" /> : null}
      {alerts.ownerHidden ? (
        <div className="flex items-center gap-3 rounded-md border border-border p-3" data-testid="alerts-owner-hidden">
          <p className="flex-1 text-sm">{t("alerts.owner_hidden")}</p>
          <Button onClick={() => void unlock()}>{t("alerts.owner_unlock")}</Button>
        </div>
      ) : null}
      {nothing ? (
        <p className="rounded-lg border border-border bg-surface-raised p-5 text-sm" data-testid="alerts-nothing">
          {t("alerts.nothing")}
        </p>
      ) : null}

      {alerts.backup !== "" ? (
        <Section title={t("alerts.section_backup")} badge={isNew(`backup_${alerts.backup}`)} testId="alerts-backup">
          <p className="text-sm">{t(alerts.backup === "none" ? "backups.warning_none" : "backups.warning_old")}</p>
          <Link to="/backups" className="text-sm underline">
            {t("home.backups_open")}
          </Link>
        </Section>
      ) : null}

      {alerts.stale.length > 0 ? (
        <Section title={t("alerts.section_stale")} badge={isNew("stale_prices")} testId="alerts-stale">
          <p className="text-sm">
            {t("alerts.stale_summary", { count: String(alerts.stale.length) })}
            {alerts.lossCount > 0 ? ` ${t("alerts.loss_summary", { count: String(alerts.lossCount) })}` : ""}
          </p>
          <table className="w-full text-sm">
            <thead>
              <tr className="text-xs text-text-muted">
                <th className="p-1 text-start">{t("alerts.product")}</th>
                <th className="p-1 text-start">{t("reprice.now")}</th>
                <th className="p-1 text-start">{t("reprice.shift")}</th>
                <th className="p-1 text-start">{t("reprice.proposed")}</th>
                <th className="p-1 text-start">{t("alerts.replacement")}</th>
              </tr>
            </thead>
            <tbody>
              {alerts.stale.map((s) => (
                <tr key={s.productId} data-testid="alerts-stale-item" className={s.loss ? "text-danger" : ""}>
                  <td className="p-1">
                    {name(s)}
                    {s.loss ? <span className="ms-2 text-xs font-semibold">{t("alerts.at_a_loss")}</span> : null}
                  </td>
                  <td className="p-1">
                    <Money value={s.price} currency={s.currency} />
                  </td>
                  <td className="p-1">
                    <bdi dir="ltr">{s.shift}%</bdi>
                  </td>
                  <td className="p-1">
                    <Money value={s.proposed} currency={s.currency} />
                  </td>
                  <td className="p-1">{s.replacement ? <Money value={s.replacement} currency={s.currency} /> : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="flex flex-wrap items-center gap-3">
            <Button variant="primary" onClick={() => setRepricing(true)} data-testid="alerts-reprice">
              {t("alerts.review_reprice")}
            </Button>
            <p className="text-xs text-text-muted">{t("alerts.reprice_optional")}</p>
          </div>
        </Section>
      ) : null}

      {alerts.hasCapital ? (
        <Section title={t("alerts.section_capital")} badge={isNew("capital")} testId="alerts-capital">
          <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 text-sm">
            <dt className="text-text-muted">{t("alerts.capital_then", { date: alerts.capital.thenDate })}</dt>
            <dd>
              <Money value={alerts.capital.thenUsd} currency="USD" />
              <span className="ms-2 text-xs text-text-muted">
                {t("alerts.capital_at_rate", { rate: alerts.capital.thenRate, currency: tDynamic(`currency.short.${alerts.localCurrency}`) })}
              </span>
            </dd>
            <dt className="text-text-muted">{t("alerts.capital_now", { date: alerts.capital.nowDate })}</dt>
            <dd>
              <Money value={alerts.capital.nowUsd} currency="USD" />
              <span className="ms-2 text-xs text-text-muted">
                {t("alerts.capital_at_rate", { rate: alerts.capital.nowRate, currency: tDynamic(`currency.short.${alerts.localCurrency}`) })}
              </span>
            </dd>
            <dt className="text-text-muted">{t("alerts.capital_change")}</dt>
            <dd className={alerts.capital.rising ? "text-success" : "text-danger"} data-testid="alerts-capital-change">
              <bdi dir="ltr">{alerts.capital.change}%</bdi>
            </dd>
            <dt className="text-text-muted">{t("alerts.capital_rate")}</dt>
            <dd>
              <bdi dir="ltr">{alerts.capital.rateShift}%</bdi>
            </dd>
            <dt className="text-text-muted">{t("alerts.capital_depreciation")}</dt>
            <dd data-testid="alerts-depreciation">
              <Money value={alerts.capital.depreciation} currency="USD" />
            </dd>
          </dl>
          <p className="text-xs text-text-muted">{t("alerts.capital_explain")}</p>
        </Section>
      ) : collecting ? (
        <Section title={t("alerts.section_capital")} testId="alerts-capital-collecting">
          <p className="text-sm text-text-muted">
            {t("alerts.capital_collecting", { days: String(alerts.historyDays), need: String(alerts.historyNeeded) })}
          </p>
        </Section>
      ) : null}

      {alerts.lowStock.length > 0 ? (
        <Section title={t("alerts.section_low")} testId="alerts-low">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-xs text-text-muted">
                <th className="p-1 text-start">{t("alerts.product")}</th>
                <th className="p-1 text-start">{t("alerts.on_hand")}</th>
                <th className="p-1 text-start">{t("alerts.level")}</th>
              </tr>
            </thead>
            <tbody>
              {alerts.lowStock.map((l) => (
                <tr key={l.productId} data-testid="alerts-low-item">
                  <td className="p-1">
                    {name(l)}
                    {isNew(`low_stock:${l.productId}`)}
                  </td>
                  <td className="p-1">
                    <bdi dir="ltr">{l.onHand}</bdi> {tDynamic(`uom.${l.unitCode}`)}
                  </td>
                  <td className="p-1">
                    <bdi dir="ltr">{l.level}</bdi> {tDynamic(`uom.${l.unitCode}`)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <Link to="/stock" className="text-sm underline">
            {t("alerts.open_stock")}
          </Link>
        </Section>
      ) : null}

      {repricing ? (
        <RepriceDialog
          onDone={(count) => {
            setRepricing(false);
            setRepriced(count);
            void refresh();
          }}
          onClose={() => setRepricing(false)}
        />
      ) : null}
    </section>
  );
}

function Section({ title, badge, testId, children }: { title: string; badge?: ReactNode; testId: string; children: ReactNode }) {
  return (
    <div className="space-y-2 rounded-lg border border-border bg-surface-raised p-5" data-testid={testId}>
      <h3 className="font-semibold">
        {title}
        {badge}
      </h3>
      {children}
    </div>
  );
}

function NewMark() {
  const { t } = useLocale();
  return (
    <span className="ms-2 rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary" data-testid="alerts-new">
      {t("alerts.new")}
    </span>
  );
}
