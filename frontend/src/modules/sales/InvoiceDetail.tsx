import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { printSalesDocument, salesDocument, type SalesLine } from "@/lib/wails";
import { Can } from "@/app/session/Can";
import { PERMISSIONS } from "@/lib/wails";
import { printHTML } from "./print";
import { Alert, Badge, Button, EmptyState, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor, isZeroMinor } from "@/modules/accounting/money";
import { formatQuantity } from "@/modules/inventory/quantity";

/**
 * One invoice, as it was issued.
 *
 * # Everything here is a SNAPSHOT
 *
 * The product names, unit codes, prices, and tax figures are what they were when the document
 * was posted, not what they are now (§9.3). A product renamed last month still appears here
 * under the name the customer's copy carries — because the document a customer holds and the
 * document the system shows must be the same document, and a screen that "helpfully" updates
 * names is showing them something that was never sent.
 */
export function InvoiceDetail({
  documentId,
  onBack,
  embedded = false,
}: {
  documentId: string;
  onBack: () => void;
  /**
   * Rendered inside a drawer, which supplies the title and the way out.
   *
   * A back button and a close button side by side are two controls that do the same thing, and a
   * user who tries the wrong one learns the screen is unpredictable.
   */
  embedded?: boolean;
}) {
  const { t } = useTranslation();
  const errorText = useErrorText();

  const [failure, setFailure] = useState<unknown>(null);

  const detail = useQuery({
    queryKey: ["sales", "document", documentId],
    queryFn: () => salesDocument(documentId),
  });

  // Rendered by Go and printed by the browser. Not cached: a document reprinted after a payment
  // must show what is owed NOW, and a stale receipt is worse than a slow one.
  const print = useMutation({
    mutationFn: (template: string) =>
      printSalesDocument(documentId, template, template === "receipt" ? "80mm" : "A4"),
    onSuccess: (rendered) => {
      setFailure(null);
      printHTML(rendered.html);
    },
    onError: setFailure,
  });

  if (detail.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (detail.isError) {
    return <Alert tone="danger" title={t("sales.failed")}>{errorText(detail.error)}</Alert>;
  }

  const { document, lines } = detail.data;
  const settled = document.status === "posted" && isZeroMinor(document.outstandingMinor);

  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        {embedded ? (
          <span />
        ) : (
          <Button variant="ghost" onClick={onBack}>
            {t("common.back")}
          </Button>
        )}
        {/* Printing is gated separately from viewing: a printed invoice leaves the building.
            A draft offers nothing to print, because a draft is not a document yet. */}
        {detail.data?.document.status === "posted" && (
          <Can permission={PERMISSIONS.salePrint}>
            <span className="flex gap-2">
              <Button
                variant="secondary"
                disabled={print.isPending}
                onClick={() => print.mutate("invoice")}
              >
                {print.isPending ? t("sales.printing") : t("sales.print")}
              </Button>
              <Button
                variant="ghost"
                disabled={print.isPending}
                onClick={() => print.mutate("receipt")}
              >
                {t("sales.printReceipt")}
              </Button>
            </span>
          </Can>
        )}
      </div>

      {failure !== null && (
        <Alert tone="danger" title={t("sales.printFailed")}>{errorText(failure)}</Alert>
      )}

      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h2 className="font-mono text-base font-medium text-text">
            {document.number || t("sales.unnumbered")}
          </h2>
          <p className="text-sm text-text-muted">
            {document.partnerName || t("sales.walkIn")} · {document.date}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Badge tone={document.status === "posted" ? "success" : "neutral"}>
            {t(`sales.status.${document.status}`)}
          </Badge>
          {settled && <Badge tone="success">{t("sales.settled")}</Badge>}
        </div>
      </header>

      <Table<SalesLine>
        caption={t("sales.lines")}
        rowKey={(line) => line.id}
        rows={lines}
        empty={<EmptyState title={t("sales.noLines")} />}
        columns={[
          { key: "no", header: "#", cell: (line) => String(line.lineNumber) },
          { key: "sku", header: t("sales.sku"), cell: (line) => line.sku },
          { key: "item", header: t("sales.item"), cell: (line) => line.productName },
          {
            key: "qty",
            header: t("sales.quantity"),
            cell: (line) => `${formatQuantity(line.quantityMicro)} ${line.uomCode}`,
          },
          {
            key: "price",
            header: t("sales.unitPrice"),
            cell: (line) => (
              <span className="flex flex-col">
                <span className="font-mono tabular-nums">{formatMinor(line.unitPriceMinor)}</span>
                {/* WHY this price. §2.6 records which list answered, and a salesperson who
                    cannot explain a price will override it by hand. */}
                {line.priceListCode && (
                  <span className="text-xs text-text-muted">{line.priceListCode}</span>
                )}
              </span>
            ),
          },
          {
            key: "tax",
            header: t("sales.tax"),
            cell: (line) => formatMinor(line.taxAmountMinor),
          },
          {
            key: "total",
            header: t("sales.lineTotal"),
            cell: (line) => formatMinor(line.totalMinor),
          },
        ]}
      />

      <dl className="ms-auto flex w-full max-w-xs flex-col gap-1 text-sm">
        <Figure label={t("sales.net")} value={formatMinor(document.netMinor)} />
        {!isZeroMinor(document.discountMinor) && (
          <Figure label={t("sales.discount")} value={formatMinor(document.discountMinor)} />
        )}
        <Figure label={t("sales.tax")} value={formatMinor(document.taxMinor)} />
        <div className="mt-1 flex items-baseline justify-between border-t border-border pt-2">
          <dt className="font-medium text-text">{t("sales.total")}</dt>
          <dd className="font-mono text-lg tabular-nums text-text">
            {formatMinor(document.totalMinor)}
          </dd>
        </div>
        {document.status === "posted" && !settled && (
          <Figure
            label={t("sales.outstanding")}
            value={formatMinor(document.outstandingMinor)}
            tone="warning"
          />
        )}
      </dl>
    </section>
  );
}

function Figure({
  label,
  value,
  tone,
}: {
  label: string;
  value: string;
  tone?: "warning";
}) {
  return (
    <div className="flex items-baseline justify-between">
      <dt className="text-text-muted">{label}</dt>
      <dd className={`font-mono tabular-nums ${tone === "warning" ? "text-warning" : "text-text"}`}>
        {value}
      </dd>
    </div>
  );
}
