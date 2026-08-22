import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { salesDocuments, type SalesDocument } from "@/lib/wails";
import { Alert, Badge, EmptyState, Input, PageHeader, Select, Sheet, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor, isZeroMinor } from "@/modules/accounting/money";
import { InvoiceDetail } from "./InvoiceDetail";
import { KpiStrip } from "@/modules/insight/KpiStrip";

/** The statuses a document can be filtered to. Empty is "any". */
const STATUSES = ["", "draft", "posted", "cancelled"] as const;

/**
 * The sales ledger: what was sold, to whom, and what is still owed.
 *
 * # Outstanding is the column this screen exists for
 *
 * A list of invoices is a record; a list of invoices with what each still owes is a working
 * document — it is the one somebody opens on a Monday morning to decide who to telephone. The
 * figure is derived on read from the allocations rather than stored, so it cannot drift from
 * the payments that justify it.
 */
export function InvoicesScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const [status, setStatus] = useState<string>("");
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<string | null>(null);

  const documents = useQuery({
    queryKey: ["sales", "documents", status],
    queryFn: () => salesDocuments("", status),
  });

  if (documents.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (documents.isError) {
    return (
      <Alert tone="danger" title={t("sales.failed")}>{errorText(documents.error)}</Alert>
    );
  }

  const term = search.trim().toLowerCase();
  const visible = documents.data.filter(
    (row) =>
      !term ||
      row.number.toLowerCase().includes(term) ||
      row.partnerName.toLowerCase().includes(term),
  );

  /*
   * A DRAWER, not a replacement.
   *
   * Until 10.16 picking a row returned the detail view in place of this whole screen, so the
   * filter, the search term and the scroll position were all discarded — and came back empty on
   * the way out. Checking six records against a delivery note meant re-typing the search six
   * times.
   */
  const openInvoice = visible.find((row) => row.id === selected);

  return (
    <section className="flex flex-col gap-4">
      <Sheet
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null);
        }}
        title={openInvoice ? openInvoice.number : ""}
        description={openInvoice ? openInvoice.partnerName : undefined}
      >
        {selected ? (
          <InvoiceDetail documentId={selected} onBack={() => setSelected(null)} embedded />
        ) : null}
      </Sheet>

      <PageHeader title={t("sales.title")} description={t("sales.help")} />

      {/* The figures somebody opens this screen to check, above the list they would otherwise
          have to add up by eye. Same source as the home screen, so the two cannot disagree. */}
      <KpiStrip source="sales" />

      <div className="flex flex-wrap items-end gap-3">
        <Input
          label={t("sales.search")}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
        <Select
          label={t("sales.status")}
          value={status}
          onValueChange={setStatus}
          options={STATUSES.map((code) => ({
            value: code,
            label: code === "" ? t("sales.anyStatus") : t(`sales.status.${code}`),
          }))}
        />
      </div>

      <Table<SalesDocument>
        caption={t("sales.title")}
        rowKey={(row) => row.id}
        rows={visible}
        empty={<EmptyState title={t("sales.none")} />}
        columns={[
          {
            key: "number",
            header: t("sales.number"),
            cell: (row) => (
              <button
                type="button"
                className="font-mono text-xs text-accent underline-offset-2 hover:underline"
                onClick={() => setSelected(row.id)}
              >
                {/* A draft has no number: §9.4 allocates one at POSTING, so that a cancelled
                    draft does not consume one and leave a hole an auditor asks about. */}
                {row.number || t("sales.unnumbered")}
              </button>
            ),
          },
          { key: "date", header: t("sales.date"), cell: (row) => row.date },
          { key: "partner", header: t("sales.customer"), cell: (row) => row.partnerName || "—" },
          {
            key: "status",
            header: t("sales.status"),
            cell: (row) => (
              <span className="flex items-center gap-2">
                <Badge tone={statusTone(row.status)}>{t(`sales.status.${row.status}`)}</Badge>
                {row.isHeld && <Badge tone="neutral">{t("sales.held")}</Badge>}
              </span>
            ),
          },
          {
            key: "total",
            header: t("sales.total"),
            cell: (row) => formatMinor(row.totalMinor),
          },
          {
            key: "outstanding",
            header: t("sales.outstanding"),
            cell: (row) => {
              // Absent rather than zero on anything not posted: a draft owes nothing because it
              // has not been agreed, and showing "0.00" would read as settled.
              if (row.status !== "posted") return "—";
              return isZeroMinor(row.outstandingMinor) ? (
                <Badge tone="success">{t("sales.settled")}</Badge>
              ) : (
                <span className="font-mono tabular-nums text-warning">
                  {formatMinor(row.outstandingMinor)}
                </span>
              );
            },
          },
        ]}
      />
    </section>
  );
}

function statusTone(status: string): "success" | "danger" | "neutral" {
  if (status === "posted") return "success";
  return status === "cancelled" ? "danger" : "neutral";
}
