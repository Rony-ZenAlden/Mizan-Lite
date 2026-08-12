import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import {
  addSaleLine,
  cancelSale,
  currentShift,
  draftSale,
  heldSales,
  holdSale,
  removeSaleLine,
  resumeSale,
  salesDocument,
  scanBarcode,
  type SalesDetail,
} from "@/lib/wails";
import { Alert, Badge, Button, EmptyState, Input, Table } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";
import { formatMinor } from "@/modules/accounting/money";
import { formatQuantity } from "@/modules/inventory/quantity";
import { PaymentPanel } from "./PaymentPanel";
import { ShiftBar } from "./ShiftBar";

/**
 * The till.
 *
 * # This screen is the product
 *
 * §32 calls the point of sale the core value, and for a shop it is the only screen that runs all
 * day. Everything else in this application is used occasionally by somebody at a desk; this is
 * used continuously by somebody standing up, with a queue in front of them.
 *
 * That drives every decision here:
 *
 *   - **The scanner is the primary input.** A barcode scanner is a keyboard that types fast and
 *     ends with Enter. The scan field therefore holds focus at all times and takes it back after
 *     every action, because a scan that lands in no field is a beep the operator trusts and an
 *     item nobody charged for.
 *   - **The total is enormous.** It is read across a counter, sometimes by the customer.
 *   - **Nothing needs a mouse.** Serving is scan, scan, scan, tender.
 *   - **Every figure comes from Go.** After each change the whole document is re-read rather than
 *     patched locally, so what the screen shows is what will be charged.
 */
export function POSTerminal() {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  // One terminal per window. A fixed name rather than a picker, because a till is a physical
  // place: the machine by the door is always the machine by the door, and asking every morning
  // is a question with the same answer every day.
  const terminal = "main";

  const [sale, setSale] = useState<SalesDetail | null>(null);
  const [code, setCode] = useState("");
  const [failure, setFailure] = useState<unknown>(null);
  const [tendering, setTendering] = useState(false);

  const scanField = useRef<HTMLInputElement>(null);
  // Focus returns to the scan field after every render that could have taken it — posting,
  // holding, resuming, an error. A till that needs a click before the next scan works is a till
  // that loses items.
  //
  // On EVERY render, not on selected actions. The paths that steal focus are open-ended —
  // voiding a line, opening the tender panel, resuming a held sale, an error appearing — and a
  // list of them is a list somebody will add to without noticing. One effect covers all of them,
  // including the ones nobody has thought of yet.
  useEffect(() => {
    scanField.current?.focus();
  });

  const shift = useQuery({
    queryKey: ["pos", "shift", terminal],
    queryFn: () => currentShift(terminal),
    retry: false,
  });
  const held = useQuery({ queryKey: ["pos", "held"], queryFn: heldSales });

  const refreshHeld = () => queryClient.invalidateQueries({ queryKey: ["pos", "held"] });

  const scan = useMutation({
    mutationFn: async (barcode: string) => {
      const found = await scanBarcode(barcode);
      // The sale is opened on the FIRST scan, not when the screen loads. Opening one per visit
      // would litter the day with empty drafts nobody cancels.
      const target =
        sale ??
        (await salesDocument(
          (
            await draftSale({
              warehouseId: "",
              partnerId: "",
              partnerName: "",
              date: "",
              currency: "",
            })
          ).id,
        ));
      return addSaleLine({
        documentId: target.document.id,
        variantId: found.variantId,
        uomId: "",
        // A case barcode is worth its whole case: one scan, twelve units (§A.5).
        quantityMicro: found.quantityMicro,
      });
    },
    onSuccess: (detail) => {
      setSale(detail);
      setFailure(null);
      setCode("");
    },
    // The scanned code is CLEARED on failure too. Leaving it would make the next scan land on
    // the end of the rejected one and fail differently, which is a worse thing to explain.
    onError: (error) => {
      setFailure(error);
      setCode("");
    },
  });

  const drop = useMutation({
    mutationFn: (lineId: string) => removeSaleLine(sale!.document.id, lineId),
    onSuccess: setSale,
    onError: setFailure,
  });

  const park = useMutation({
    mutationFn: (label: string) => holdSale(sale!.document.id, label),
    onSuccess: () => {
      setSale(null);
      setTendering(false);
      void refreshHeld();
    },
    onError: setFailure,
  });

  const unpark = useMutation({
    mutationFn: (documentId: string) => resumeSale(documentId),
    onSuccess: (detail) => {
      setSale(detail);
      void refreshHeld();
    },
    onError: setFailure,
  });

  const abandon = useMutation({
    mutationFn: () => cancelSale(sale!.document.id),
    onSuccess: () => {
      setSale(null);
      setTendering(false);
    },
    onError: setFailure,
  });

  // ── a till cannot trade without an open shift ────────────────────────────────
  //
  // Not a warning: the screen is REPLACED. Cash taken outside a shift belongs to no
  // reconciliation, so at close the drawer is short and there is no record of why.
  //
  // The PENDING case is handled first and separately, and that ordering is the point. Treating
  // "not yet known" as "open" renders a scan field and a cart for as long as the query takes —
  // and a scanner does not wait to be told the screen was provisional. An operator who scans
  // into that window has items on a sale that is about to be replaced.
  //
  // An error is not pending: a till that has never opened a shift answers with one, and that
  // must land on the gate rather than on a spinner nobody can leave.
  if (shift.isPending) {
    return <p className="p-4 text-sm text-text-muted">{t("gate.checking")}</p>;
  }
  if (shift.data?.status !== "open") {
    return <ShiftBar terminal={terminal} shift={shift.data} />;
  }

  const lines = sale?.lines ?? [];
  const total = sale?.document.totalMinor ?? "0";

  return (
    <section className="flex h-full flex-col gap-4">
      <ShiftBar terminal={terminal} shift={shift.data} compact />

      <div className="grid flex-1 grid-cols-1 gap-4 lg:grid-cols-[1fr_22rem]">
        {/* ── the cart ─────────────────────────────────────────────────────── */}
        <div className="flex flex-col gap-3">
          <Input
            ref={scanField}
            label={t("pos.scan")}
            value={code}
            autoFocus
            // A scanner types and presses Enter. Nothing else is needed to sell.
            onChange={(event) => setCode(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && code.trim()) {
                event.preventDefault();
                scan.mutate(code.trim());
              }
            }}
          />

          {failure !== null && (
            <Alert tone="danger" title={t("pos.scanFailed")}>
              {errorText(failure)}
            </Alert>
          )}

          <Table<SalesDetail["lines"][number]>
            caption={t("pos.cart")}
            rowKey={(line) => line.id}
            rows={lines}
            empty={<EmptyState title={t("pos.empty")} />}
            columns={[
              { key: "sku", header: t("pos.sku"), cell: (line) => line.sku },
              { key: "name", header: t("pos.item"), cell: (line) => line.productName },
              {
                key: "qty",
                header: t("pos.quantity"),
                cell: (line) => `${formatQuantity(line.quantityMicro)} ${line.uomCode}`,
              },
              {
                key: "price",
                header: t("pos.price"),
                cell: (line) => formatMinor(line.unitPriceMinor),
              },
              {
                key: "total",
                header: t("pos.lineTotal"),
                cell: (line) => formatMinor(line.totalMinor),
              },
              {
                key: "void",
                header: t("pos.void"),
                cell: (line) => (
                  <Button
                    variant="ghost"
                    onClick={() => drop.mutate(line.id)}
                  >
                    {t("pos.void")}
                  </Button>
                ),
              },
            ]}
          />
        </div>

        {/* ── the totals and the tender ────────────────────────────────────── */}
        <aside className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4">
          <dl className="flex flex-col gap-1 text-sm">
            <Figure label={t("pos.net")} value={formatMinor(sale?.document.netMinor ?? "0")} />
            <Figure label={t("pos.tax")} value={formatMinor(sale?.document.taxMinor ?? "0")} />
            <div className="mt-2 flex items-baseline justify-between border-t border-border pt-3">
              <dt className="text-sm font-medium text-text">{t("pos.total")}</dt>
              {/* Read across a counter, sometimes by the customer. */}
              <dd className="font-mono text-3xl font-semibold tabular-nums text-text">
                {formatMinor(total)}
              </dd>
            </div>
          </dl>

          {tendering && sale ? (
            <PaymentPanel
              sale={sale}
              shiftId={shift.data?.id ?? ""}
              onSettled={() => {
                setSale(null);
                setTendering(false);
              }}
              onCancel={() => setTendering(false)}
            />
          ) : (
            <div className="flex flex-col gap-2">
              <Button
                variant="primary"
                disabled={lines.length === 0}
                onClick={() => setTendering(true)}
              >
                {t("pos.tender")}
              </Button>
              <Button
                variant="secondary"
                disabled={lines.length === 0}
                onClick={() => park.mutate(sale?.document.partnerName || t("pos.heldSale"))}
              >
                {t("pos.hold")}
              </Button>
              <Button variant="ghost" disabled={!sale} onClick={() => abandon.mutate()}>
                {t("pos.abandon")}
              </Button>
            </div>
          )}

          {/* Parked sales, offered back. A held sale nobody can find is a sale that is lost. */}
          {(held.data?.length ?? 0) > 0 && (
            <div className="flex flex-col gap-2 border-t border-border pt-3">
              <h3 className="text-xs font-medium uppercase text-text-muted">{t("pos.held")}</h3>
              {held.data?.map((parked) => (
                <button
                  key={parked.id}
                  type="button"
                  className="flex items-center justify-between rounded border border-border px-2 py-1 text-sm hover:bg-surface-hover"
                  onClick={() => unpark.mutate(parked.id)}
                >
                  <span>{parked.holdLabel || t("pos.heldSale")}</span>
                  <Badge tone="neutral">{formatMinor(parked.totalMinor)}</Badge>
                </button>
              ))}
            </div>
          )}
        </aside>
      </div>
    </section>
  );
}

function Figure({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between">
      <dt className="text-text-muted">{label}</dt>
      <dd className="font-mono tabular-nums text-text">{value}</dd>
    </div>
  );
}
