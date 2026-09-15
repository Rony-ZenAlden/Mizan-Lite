import { useCallback, useEffect, useRef, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { Preview, PrintResult, PrinterSettings } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";

/** What a printout is of: a sale's receipt, or a debt payment's or refund's voucher. */
export type Printable = "sale" | "entry";

/** What was just recorded, for automatic printing (Q-L7.2). */
export type Recorded = "cash_sale" | "credit_sale" | "voucher";

/**
 * Whether a document prints itself as soon as it is recorded (Q-L7.2): never with no printer chosen (A-L7.6); with "credit",
 * credit sales, payments and refunds do and a cash sale waits for its button; with "all", everything does.
 */
export function printsItself(settings: Pick<PrinterSettings, "printer" | "autoPrint"> | null, recorded: Recorded): boolean {
  if (!settings || settings.printer === "") return false;
  switch (settings.autoPrint) {
    case "all":
      return true;
    case "credit":
      return recorded !== "cash_sale";
    default:
      return false;
  }
}

type State = { kind: "idle" } | { kind: "busy" } | { kind: "sent"; result: PrintResult } | { kind: "failed"; error: unknown };

/**
 * Printing a receipt or voucher (L7 §5): the bitmap Go will send, when asked for (D-L7.11 — what you see is what prints), a
 * *Print* button that becomes *Print a copy* once one was sent, and the outcome. A failure never touches what was recorded
 * (D-L7.10); it says why and offers *Print again*. A refund's voucher is the owner's, so every call goes through withOwner.
 */
export function PrintPanel({ kind, id, auto = false, preview = false }: { kind: Printable; id: string; auto?: boolean; preview?: boolean }) {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, errorText } = useLocale();
  const [image, setImage] = useState<Preview | null>(null);
  const [previewError, setPreviewError] = useState<unknown>(null);
  const [state, setState] = useState<State>({ kind: "idle" });
  const [sent, setSent] = useState(0);
  const started = useRef(false);

  useEffect(() => {
    if (!preview) return;
    let cancelled = false;
    withOwner(() => client.print.preview(kind, id))
      .then((p) => {
        if (cancelled) return;
        setImage(p);
        setPreviewError(null);
      })
      .catch((e) => {
        if (!cancelled && !(e instanceof OwnerCancelled)) setPreviewError(e);
      });
    return () => {
      cancelled = true;
    };
    // A sent copy changes the stamp the next one carries: read the preview again after each.
  }, [client, withOwner, kind, id, preview, sent]);

  const print = useCallback(async () => {
    setState({ kind: "busy" });
    try {
      const result = await withOwner(() => (kind === "sale" ? client.print.sale(id) : client.print.entry(id)));
      setState({ kind: "sent", result });
      setSent((n) => n + 1);
    } catch (e) {
      setState(e instanceof OwnerCancelled ? { kind: "idle" } : { kind: "failed", error: e });
    }
  }, [client, withOwner, kind, id]);

  // Once, however often React runs effects: a second automatic print would be a second copy on paper.
  useEffect(() => {
    if (!auto || started.current) return;
    started.current = true;
    void print();
  }, [auto, print]);

  const copy = sent > 0 || (image !== null && image.copyNo > 1);

  return (
    <div className="space-y-2" data-testid="print-panel">
      {preview && image ? (
        <img
          src={`data:image/png;base64,${image.png}`}
          alt={t("print.preview_alt")}
          data-testid="print-preview"
          width={image.width}
          height={image.height}
          className="mx-auto h-auto w-full max-w-[80mm] border border-dashed border-border bg-surface-raised"
        />
      ) : null}
      {previewError ? <Alert tone="danger" title={errorText(previewError)} /> : null}

      {state.kind === "busy" ? (
        <p role="status" className="text-sm text-text-muted">
          {t("print.printing")}
        </p>
      ) : null}
      {state.kind === "sent" ? (
        <Alert
          tone="success"
          title={
            state.result.copyNo > 1
              ? t("print.sent_copy", { printer: state.result.printer, number: String(state.result.copyNo) })
              : t("print.sent", { printer: state.result.printer })
          }
        />
      ) : null}
      {state.kind === "failed" ? (
        <Alert tone="danger" title={t("print.failed")}>
          <p>{errorText(state.error)}</p>
          <Button className="mt-2" onClick={() => void print()}>
            {t("print.again")}
          </Button>
        </Alert>
      ) : null}

      {state.kind !== "failed" ? (
        <Button onClick={() => void print()} disabled={state.kind === "busy"}>
          {copy ? t("print.copy") : kind === "sale" ? t("print.receipt") : t("print.voucher")}
        </Button>
      ) : null}
    </div>
  );
}

/** The printer settings, read once for automatic printing; null until read or when the read fails (then nothing prints itself). */
export function usePrinterSettings(): PrinterSettings | null {
  const client = useClient();
  const [settings, setSettings] = useState<PrinterSettings | null>(null);
  useEffect(() => {
    let cancelled = false;
    client.printers
      .settings()
      .then((s) => {
        if (!cancelled) setSettings(s);
      })
      .catch(() => {
        // Not printing automatically is the safe answer; the Print button still works and reports its own failure.
      });
    return () => {
      cancelled = true;
    };
  }, [client]);
  return settings;
}
