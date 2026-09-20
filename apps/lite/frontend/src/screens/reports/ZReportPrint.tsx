import { useState } from "react";
import { useClient } from "@/api/ClientContext";
import { useLocale } from "@/i18n/LocaleProvider";
import { usePrinterSettings } from "@/printing/PrintPanel";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";

/**
 * Printing the end of the day on receipt paper (the owner's request, 2026-09-20).
 *
 * The statement above is the whole report and always readable, because most shops running this have no printer. This
 * is the extra for the ones that do: the same figures, on 80 mm, to go in the till drawer at close.
 */
export function ZReportPrint({ date }: { date: string }) {
  const client = useClient();
  const { t, errorText } = useLocale();
  const printer = usePrinterSettings();
  const [sent, setSent] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  if (!printer || printer.printer === "") {
    return (
      <p className="text-xs text-text-muted" data-testid="zreport-screen-only">
        {t("zreport.screen_only")}
      </p>
    );
  }

  const print = async () => {
    setBusy(true);
    setError(null);
    try {
      await client.print.zReport(date);
      setSent(true);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-2" data-testid="zreport-print">
      <Button onClick={() => void print()} disabled={busy}>
        {t("zreport.print")}
      </Button>
      {sent ? <Alert tone="success" title={t("zreport.printed")} /> : null}
      {error ? <Alert tone="danger" title={errorText(error)} /> : null}
    </div>
  );
}
