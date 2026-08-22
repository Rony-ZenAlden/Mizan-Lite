import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import {
  confirmGoodsReceipt,
  draftGoodsReceipt,
  purchaseOrder,
  purchaseOrders,
  receiveLine,
  type PurchaseOrder,
} from "@/lib/wails";
import { Alert, Button, Card, EmptyState, Input, PageHeader, Select, StepList } from "@/shared/ui";
import { useErrorText } from "@/modules/admin/useAdminError";

/**
 * Receiving a delivery (Step 10.13).
 *
 * # This is the screen purchasing could not be operated without
 *
 * `DraftReceipt` and `ReceiveLine` were Go bindings with no caller. Deliveries could be LISTED
 * and never recorded, so the only way to take goods into stock was the CSV importer or the
 * database — for a module Phase 6 built in full.
 *
 * # Why it is guided rather than a form
 *
 * Receiving is three acts, and the order matters: pick the order, say what actually turned up,
 * then confirm — which is the irreversible one, because it moves stock and accrues what is owed.
 * A flat form with all three on screen invites confirming before counting.
 */
export function ReceiveDeliveryScreen() {
  const { t } = useTranslation();
  const errorText = useErrorText();
  const queryClient = useQueryClient();

  const [orderId, setOrderId] = useState("");
  const [receiptId, setReceiptId] = useState("");
  const [note, setNote] = useState("");
  const [quantities, setQuantities] = useState<Record<string, string>>({});

  // Only PLACED orders can receive: a draft has not been sent, and a closed one expects nothing.
  const orders = useQuery({
    queryKey: ["purchasing", "orders", "placed"],
    queryFn: () => purchaseOrders("placed"),
  });
  const order = useQuery({
    queryKey: ["purchasing", "order", orderId],
    queryFn: () => purchaseOrder(orderId),
    enabled: orderId !== "",
  });

  const start = useMutation({
    mutationFn: () =>
      draftGoodsReceipt({
        orderId,
        warehouseId: "",
        receiptDate: new Date().toISOString().slice(0, 10),
        deliveryNoteReference: note,
      }),
    onSuccess: (receipt) => setReceiptId(receipt.id),
  });

  const record = useMutation({
    mutationFn: (line: { orderLineId: string; quantityMicro: string }) =>
      receiveLine({ receiptId, ...line, notes: "" }),
  });

  const confirm = useMutation({
    mutationFn: () => confirmGoodsReceipt(receiptId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["purchasing"] });
      void queryClient.invalidateQueries({ queryKey: ["inventory"] });
      setOrderId("");
      setReceiptId("");
      setQuantities({});
      setNote("");
    },
  });

  const step = receiptId === "" ? (orderId === "" ? "order" : "start") : "count";
  const lines = order.data?.lines ?? [];

  return (
    <section className="flex flex-col gap-6">
      <PageHeader title={t("receive.title")} description={t("receive.help")} />

      <StepList
        current={step}
        steps={[
          { id: "order", label: t("receive.step.order"), hint: t("receive.step.orderHint"), done: orderId !== "" },
          { id: "start", label: t("receive.step.start"), hint: t("receive.step.startHint"), done: receiptId !== "" },
          { id: "count", label: t("receive.step.count"), hint: t("receive.step.countHint") },
        ]}
      />

      {[start, record, confirm].map((m, i) =>
        m.isError ? (
          <Alert key={i} tone="danger" title={t("receive.failed")}>{errorText(m.error)}</Alert>
        ) : null,
      )}
      {confirm.isSuccess && (
        <Alert tone="info" title={t("receive.done")}>{t("receive.doneHelp")}</Alert>
      )}

      {/* ── 1. which order ─────────────────────────────────────────────────── */}
      {step === "order" && (
        <Card title={t("receive.step.order")} description={t("receive.pickOrderHelp")}>
          {orders.data && orders.data.length === 0 ? (
            <EmptyState title={t("receive.noOrders")} />
          ) : (
            <Select
              label={t("receive.order")}
              value={orderId}
              onValueChange={setOrderId}
              options={[
                { value: "", label: t("receive.choose") },
                ...(orders.data ?? []).map((o: PurchaseOrder) => ({
                  value: o.id,
                  label: `${o.number} — ${o.supplierName}`,
                })),
              ]}
            />
          )}
        </Card>
      )}

      {/* ── 2. open the delivery ───────────────────────────────────────────── */}
      {step === "start" && (
        <Card title={t("receive.step.start")} description={t("receive.startHelp")}>
          <div className="flex flex-col gap-3">
            <Input
              label={t("receive.deliveryNote")}
              value={note}
              onChange={(event) => setNote(event.target.value)}
              placeholder={t("receive.deliveryNoteHint")}
            />
            <div className="flex justify-between gap-2">
              <Button variant="ghost" onClick={() => setOrderId("")}>
                {t("receive.back")}
              </Button>
              <Button onClick={() => start.mutate()} disabled={start.isPending}>
                {t("receive.startButton")}
              </Button>
            </div>
          </div>
        </Card>
      )}

      {/* ── 3. what actually arrived ───────────────────────────────────────── */}
      {step === "count" && (
        <Card title={t("receive.step.count")} description={t("receive.countHelp")}>
          <div className="flex flex-col gap-4">
            {lines.length === 0 ? (
              <EmptyState title={t("receive.noLines")} />
            ) : (
              <ul className="flex flex-col gap-3">
                {lines.map((line) => (
                  <li
                    key={line.id}
                    className="flex flex-wrap items-end justify-between gap-3 rounded-lg border border-border p-3"
                  >
                    <div className="flex min-w-0 flex-col">
                      <span className="text-sm text-text">{line.productName}</span>
                      <span className="text-xs text-text-muted">
                        {t("receive.ordered", { quantity: line.quantityMicro })}
                      </span>
                    </div>
                    <div className="flex items-end gap-2">
                      <Input
                        label={t("receive.arrived")}
                        value={quantities[line.id] ?? ""}
                        onChange={(event) =>
                          setQuantities({ ...quantities, [line.id]: event.target.value })
                        }
                      />
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={!quantities[line.id] || record.isPending}
                        onClick={() =>
                          record.mutate({
                            orderLineId: line.id,
                            quantityMicro: `${quantities[line.id]}000000`,
                          })
                        }
                      >
                        {t("receive.record")}
                      </Button>
                    </div>
                  </li>
                ))}
              </ul>
            )}

            {/*
             * Confirming is the irreversible act: it moves stock and accrues what is owed. It is
             * separated from recording the lines, and labelled with what it does rather than
             * "Save" — 6.4's rule, that a screen must not let somebody confirm before counting.
             */}
            <div className="flex items-center justify-between gap-2 border-t border-border pt-3">
              <span className="text-xs text-text-muted">{t("receive.confirmWarning")}</span>
              <Button
                variant="danger"
                onClick={() => confirm.mutate()}
                disabled={confirm.isPending}
              >
                {t("receive.confirm")}
              </Button>
            </div>
          </div>
        </Card>
      )}
    </section>
  );
}
