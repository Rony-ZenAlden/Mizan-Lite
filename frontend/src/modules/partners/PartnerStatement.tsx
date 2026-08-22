import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { partnerStatement, type OpenItem } from "@/lib/wails";
import { Alert, Badge, Button, EmptyState, Input, PageHeader, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor, isZeroMinor } from "@/modules/accounting/money";

/**
 * What one partner owes and is owed.
 *
 * # Receivable and payable stay apart
 *
 * A statement sent to a customer shows what they owe; one sent to a supplier shows what is owed
 * to them. Merging them into a single signed list would produce a document nobody can send to
 * either — and the same partner is frequently both.
 */
export function PartnerStatement({
  partnerId,
  onBack,
}: {
  partnerId: string;
  onBack: () => void;
}) {
  const { t } = useTranslation();
  const errorText = useErrorText();

  // The ageing date is the OPERATOR'S, not the clock's. A statement printed for a month end must
  // say what it said at that month end, and one that re-ages itself on reopening is a report
  // nobody can file.
  const [asAt, setAsAt] = useState("");

  const statement = useQuery({
    queryKey: ["partners", "statement", partnerId, asAt],
    queryFn: () => partnerStatement(partnerId, asAt),
  });

  if (statement.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (statement.isError) {
    return (
      <Alert tone="danger" title={t("statement.failed")}>{errorText(statement.error)}</Alert>
    );
  }

  const { balance, receivable, payable, receivableAgeing } = statement.data;
  const owesUs = !balance.netMinor.startsWith("-") && !isZeroMinor(balance.netMinor);

  return (
    <section className="flex flex-col gap-5">
      <div className="flex items-center justify-between">
        <Button variant="ghost" onClick={onBack}>
          {t("common.back")}
        </Button>
        <Input
          label={t("statement.asAt")}
          value={asAt}
          placeholder={t("statement.today")}
          onChange={(event) => setAsAt(event.target.value)}
        />
      </div>

      <PageHeader title={balance.partnerName} description={t("statement.help")} />

      {/* ── where they stand ─────────────────────────────────────────────────── */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <Figure label={t("statement.receivable")} value={balance.receivableMinor} />
        <Figure label={t("statement.payable")} value={balance.payableMinor} />
        <div className="flex flex-col gap-1 rounded-lg border border-border bg-surface p-4">
          <span className="text-xs uppercase text-text-muted">{t("statement.net")}</span>
          <span className="font-mono text-2xl tabular-nums text-text">
            {formatMinor(balance.netMinor)}
          </span>
          <span className="text-xs text-text-muted">
            {/* Both halves are shown above, because netting them away hides the case that
                matters most: owing 5,000 and being owed 4,900 is not the same risk as owing
                100. */}
            {t(owesUs ? "statement.owesUs" : "statement.weOwe")}
          </span>
        </div>
      </div>

      {/* ── how overdue ──────────────────────────────────────────────────────── */}
      {!isZeroMinor(balance.receivableMinor) && (
        <div className="flex flex-wrap gap-2">
          {[
            ["statement.current", receivableAgeing.currentMinor, "success"],
            ["statement.days30", receivableAgeing.days30Minor, "neutral"],
            ["statement.days60", receivableAgeing.days60Minor, "warning"],
            ["statement.days90", receivableAgeing.days90Minor, "warning"],
            ["statement.older", receivableAgeing.olderMinor, "danger"],
          ].map(([key, amount, tone]) =>
            isZeroMinor(String(amount)) ? null : (
              <Badge key={String(key)} tone={tone as "success" | "neutral" | "warning" | "danger"}>
                {t(String(key))}: {formatMinor(String(amount))}
              </Badge>
            ),
          )}
        </div>
      )}

      <OpenItems title={t("statement.theyOwe")} items={receivable} />
      <OpenItems title={t("statement.weOweThem")} items={payable} />
    </section>
  );
}

function OpenItems({ title, items }: { title: string; items: OpenItem[] }) {
  const { t } = useTranslation();
  return (
    <Table<OpenItem>
      caption={title}
      rowKey={(item) => item.documentId}
      rows={items}
      empty={<EmptyState title={t("statement.nothingOpen")} />}
      columns={[
        {
          key: "document",
          header: t("statement.document"),
          cell: (item) => (
            <span className="flex flex-col">
              <span className="font-mono text-xs">{item.documentNumber || "—"}</span>
              {/* WHICH kind of document. A statement row a customer can check against their own
                  records needs to say what it was, not only what it came to. */}
              <span className="text-xs text-text-muted">
                {t(`statement.type.${item.documentType}`)}
              </span>
            </span>
          ),
        },
        { key: "date", header: t("statement.date"), cell: (item) => item.date },
        { key: "due", header: t("statement.due"), cell: (item) => item.dueDate || "—" },
        {
          key: "total",
          header: t("statement.total"),
          cell: (item) => formatMinor(item.totalMinor),
        },
        {
          key: "settled",
          header: t("statement.settled"),
          cell: (item) =>
            isZeroMinor(item.settledMinor) ? "—" : formatMinor(item.settledMinor),
        },
        {
          key: "outstanding",
          header: t("statement.outstanding"),
          cell: (item) => (
            <span className="font-mono tabular-nums text-warning">
              {formatMinor(item.outstandingMinor)}
            </span>
          ),
        },
      ]}
    />
  );
}

function Figure({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1 rounded-lg border border-border bg-surface p-4">
      <span className="text-xs uppercase text-text-muted">{label}</span>
      <span className="font-mono text-2xl tabular-nums text-text">{formatMinor(value)}</span>
    </div>
  );
}
