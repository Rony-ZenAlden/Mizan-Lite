import { useState } from "react";
import { useClient } from "@/api/ClientContext";
import type { Product } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Dialog } from "@/ui/Dialog";
import { TextField } from "@/ui/Field";

/**
 * Printing price tags for a product (the owner's request, 2026-09-20).
 *
 * The tag goes to the receipt printer the shop already owns — a roll cut short — not to a dedicated label printer,
 * because a pantry shop has the one and not the other. A product with no barcode still gets a tag with its price on
 * it: the symbol is simply absent, rather than invented, because an invented symbol scans as a product the shop does
 * not have.
 */
export function LabelDialog({ product, onClose }: { product: Product; onClose: () => void }) {
  const client = useClient();
  const { t, locale, errorText } = useLocale();
  const [copies, setCopies] = useState("1");
  const [error, setError] = useState<unknown>(null);
  const [sent, setSent] = useState<{ count: number; printer: string } | null>(null);
  const [busy, setBusy] = useState(false);

  const count = Number.parseInt(copies, 10);
  const valid = Number.isFinite(count) && count >= 1 && count <= 40;

  const print = async () => {
    setBusy(true);
    setError(null);
    try {
      const result = await client.print.label(product.id, count);
      setSent({ count, printer: result.printer });
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title={t("label.print")} onClose={onClose}>
      <p className="font-semibold">{locale === "en" && product.nameEn ? product.nameEn : product.nameAr}</p>
      {product.barcode === "" ? <p className="mt-1 text-xs text-text-muted">{t("label.no_barcode")}</p> : null}
      <div className="mt-3 space-y-3">
        <TextField
          label={t("label.copies")}
          value={copies}
          onChange={(e) => setCopies(e.target.value)}
          inputMode="numeric"
          dir="ltr"
        />
        {sent ? <Alert tone="success" title={t("label.printed", { count: String(sent.count), printer: sent.printer })} /> : null}
        {error ? <Alert tone="danger" title={errorText(error)} /> : null}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>{t("action.close")}</Button>
          <Button variant="primary" onClick={() => void print()} disabled={busy || !valid}>
            {t("label.print")}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
