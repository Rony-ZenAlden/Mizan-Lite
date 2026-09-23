import { useEffect, useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { Preview } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";

type State =
  | { kind: "idle" }
  | { kind: "busy" }
  | { kind: "printed"; printer: string }
  | { kind: "saved"; path: string }
  | { kind: "failed"; error: unknown };

/**
 * A sale's A4 invoice (the owner's request, 2026-09-24): printed on the A4 printer chosen on the Printer screen, or saved
 * as a PDF to send or to print anywhere. The preview is Go's own page — what prints is what is shown (D-L7.11). Anyone at
 * the counter may: the invoice goes to the customer.
 */
export function InvoicePanel({ saleId, preview }: { saleId: string; preview: boolean }) {
  const client = useClient();
  const { t, errorText } = useLocale();
  const [image, setImage] = useState<Preview | null>(null);
  const [previewError, setPreviewError] = useState<unknown>(null);
  const [state, setState] = useState<State>({ kind: "idle" });

  useEffect(() => {
    if (!preview) return;
    let cancelled = false;
    client.print
      .preview("invoice", saleId)
      .then((p) => {
        if (!cancelled) setImage(p);
      })
      .catch((e) => {
        if (!cancelled) setPreviewError(e);
      });
    return () => {
      cancelled = true;
    };
  }, [client, saleId, preview]);

  const print = async () => {
    setState({ kind: "busy" });
    try {
      setState({ kind: "printed", printer: (await client.print.invoice(saleId)).printer });
    } catch (e) {
      setState({ kind: "failed", error: e });
    }
  };

  const save = async () => {
    setState({ kind: "busy" });
    try {
      const saved = await client.exports.invoice(saleId);
      setState(saved.cancelled ? { kind: "idle" } : { kind: "saved", path: saved.path });
    } catch (e) {
      setState({ kind: "failed", error: e });
    }
  };

  return (
    <div className="space-y-2" data-testid="invoice-panel">
      {preview && image ? (
        <img
          src={`data:image/png;base64,${image.png}`}
          alt={t("invoice.preview_alt")}
          data-testid="invoice-preview"
          width={image.width}
          height={image.height}
          className="mx-auto h-auto w-full border border-border bg-white"
        />
      ) : null}
      {previewError ? <Alert tone="danger" title={errorText(previewError)} /> : null}
      {state.kind === "printed" ? <Alert tone="success" title={t("invoice.printed", { printer: state.printer })} testId="invoice-printed" /> : null}
      {state.kind === "saved" ? <Alert tone="success" title={t("invoice.saved", { path: state.path })} testId="invoice-saved" /> : null}
      {state.kind === "failed" ? <Alert tone="danger" title={errorText(state.error)} testId="invoice-failed" /> : null}
      <div className="flex flex-wrap gap-2">
        <Button onClick={() => void print()} disabled={state.kind === "busy"} data-testid="invoice-print">
          {t("invoice.print")}
        </Button>
        <Button onClick={() => void save()} disabled={state.kind === "busy"} data-testid="invoice-save">
          {t("invoice.save_pdf")}
        </Button>
      </div>
    </div>
  );
}
