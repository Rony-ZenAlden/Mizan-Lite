import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { useClient } from "@/api/ClientContext";
import type { PrintResult, PrinterInfo, PrinterSettings } from "@/api/client";
import { useLocale } from "@/i18n/LocaleProvider";
import { OwnerCancelled, useOwner } from "@/owner/OwnerProvider";
import { formErrors } from "@/screens/stock/forms";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Checkbox } from "@/ui/Checkbox";
import { SelectField, TextField } from "@/ui/Field";
import { Spinner } from "@/ui/Spinner";

// The limit Go holds (settings/domain MaxLineRunes); the field stops typing where Go would refuse.
const MAX_LINE = 120;

/**
 * The receipt printer (L7 §5, §9.2): the printers the operating system knows, the paper, how the receipt reaches the printer —
 * through its driver (the default, Q-L7.1) or straight to it as ESC/POS — what prints itself (Q-L7.2), the cash drawer
 * (Q-L7.11), and the line at the foot of every receipt. Changing them is the owner's; the test page is anyone's.
 *
 * The A4 printer an invoice goes to is here too (0.10.0) — an office printer, beside the receipt roll. What heads every
 * document — the shop's name, logo, phone, city and address — is under Settings > Store information, set once for all.
 */
export function PrinterScreen() {
  const client = useClient();
  const { withOwner } = useOwner();
  const { t, tDynamic, errorText } = useLocale();
  const [form, setForm] = useState<PrinterSettings | null>(null);
  const [printers, setPrinters] = useState<PrinterInfo[]>([]);
  const [listError, setListError] = useState<unknown>(null);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [error, setError] = useState<unknown>(null);
  const [saved, setSaved] = useState(false);
  const [busy, setBusy] = useState(false);
  const [test, setTest] = useState<{ kind: "sent"; result: PrintResult } | { kind: "failed"; error: unknown } | null>(null);

  const loadPrinters = useCallback(async () => {
    try {
      setPrinters(await client.printers.list());
      setListError(null);
    } catch (e) {
      setListError(e);
    }
  }, [client]);

  useEffect(() => {
    client.printers
      .settings()
      .then(setForm)
      .catch(setLoadError);
    void loadPrinters();
  }, [client, loadPrinters]);

  if (loadError) return <Alert tone="danger" title={errorText(loadError)} />;
  if (!form) return <Spinner label={t("state.loading")} />;

  const set = <K extends keyof PrinterSettings>(key: K, value: PrinterSettings[K]) => {
    setSaved(false);
    setForm({ ...form, [key]: value });
  };
  const errors = formErrors(error, errorText);
  const known = printers.some((p) => p.name === form.printer);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      setForm(await withOwner(() => client.printers.save({ ...form, paperMm: String(form.paperMm) })));
      setSaved(true);
    } catch (e) {
      if (!(e instanceof OwnerCancelled)) setError(e);
    } finally {
      setBusy(false);
    }
  };

  const printTest = async () => {
    setTest(null);
    try {
      setTest({ kind: "sent", result: await client.printers.test() });
    } catch (e) {
      setTest({ kind: "failed", error: e });
    }
  };

  return (
    <section className="mx-auto max-w-2xl space-y-4">
      <h2 className="text-xl font-semibold">{t("printer.title")}</h2>

      <form className="space-y-3 rounded-md border border-border bg-surface-raised p-4" onSubmit={(e) => void submit(e)}>
        <div className="flex flex-wrap items-end gap-2">
          <div className="min-w-64 flex-1">
            <SelectField label={t("printer.printer")} value={form.printer} onChange={(e) => set("printer", e.target.value)} error={errors.field("printer")}>
              <option value="">{t("printer.none")}</option>
              {printers.map((p) => (
                <option key={p.name} value={p.name}>
                  {p.default ? t("printer.default_name", { name: p.name }) : p.name}
                </option>
              ))}
              {form.printer && !known ? <option value={form.printer}>{t("printer.missing_name", { name: form.printer })}</option> : null}
            </SelectField>
          </div>
          <Button onClick={() => void loadPrinters()}>{t("printer.refresh")}</Button>
        </div>
        {listError ? <Alert tone="danger" title={errorText(listError)} /> : null}
        {!listError && printers.length === 0 ? <p className="text-sm text-text-muted">{t("printer.no_printers")}</p> : null}

        <SelectField label={t("printer.paper")} value={String(form.paperMm)} onChange={(e) => set("paperMm", Number(e.target.value))} error={errors.field("paperMm")}>
          <option value="80">{t("printer.paper_80")}</option>
          <option value="58">{t("printer.paper_58")}</option>
        </SelectField>
        <SelectField label={t("printer.path")} value={form.path} onChange={(e) => set("path", e.target.value)} error={errors.field("path")} hint={t("printer.path_hint")}>
          {["driver", "raw"].map((p) => (
            <option key={p} value={p}>
              {tDynamic(`printers.path.${p}`)}
            </option>
          ))}
        </SelectField>
        <SelectField label={t("printer.auto")} value={form.autoPrint} onChange={(e) => set("autoPrint", e.target.value)} error={errors.field("autoPrint")}>
          {["credit", "all", "none"].map((a) => (
            <option key={a} value={a}>
              {tDynamic(`printer.auto.${a}`)}
            </option>
          ))}
        </SelectField>
        <Checkbox label={t("printer.drawer")} checked={form.drawer} onChange={(e) => set("drawer", e.target.checked)} />
        {form.drawer && form.path !== "raw" ? <p className="text-xs text-text-muted">{t("printer.drawer_hint")}</p> : null}

        <h3 className="pt-2 font-semibold">{t("printer.invoice")}</h3>
        <SelectField
          label={t("printer.invoice_printer")}
          value={form.invoicePrinter}
          onChange={(e) => set("invoicePrinter", e.target.value)}
          error={errors.field("invoicePrinter")}
          hint={t("printer.invoice_printer_hint")}
          data-testid="invoice-printer"
        >
          <option value="">{t("printer.invoice_none")}</option>
          {printers.map((p) => (
            <option key={p.name} value={p.name}>
              {p.default ? t("printer.default_name", { name: p.name }) : p.name}
            </option>
          ))}
          {form.invoicePrinter && !printers.some((p) => p.name === form.invoicePrinter) ? (
            <option value={form.invoicePrinter}>{t("printer.missing_name", { name: form.invoicePrinter })}</option>
          ) : null}
        </SelectField>

        <h3 className="pt-2 font-semibold">{t("printer.header")}</h3>
        <p className="text-sm text-text-muted" data-testid="printer-header-moved">
          {t("printer.header_moved")}{" "}
          <Link to="/settings" className="underline">
            {t("printer.header_open")}
          </Link>
        </p>
        <TextField label={t("printer.footer")} value={form.footer} onChange={(e) => set("footer", e.target.value)} error={errors.field("footer")} maxLength={MAX_LINE} hint={t("printer.footer_hint")} autoComplete="off" />

        {errors.form ? <Alert tone="danger" title={errors.form} /> : null}
        {saved ? <Alert tone="success" title={t("printer.saved")} /> : null}
        <div className="flex justify-end">
          <Button variant="primary" type="submit" disabled={busy}>
            {t("action.save")}
          </Button>
        </div>
      </form>

      <div className="space-y-2 rounded-md border border-border bg-surface-raised p-4">
        <h3 className="font-semibold">{t("printer.test")}</h3>
        <p className="text-sm text-text-muted">{t("printer.test_hint")}</p>
        <Button onClick={() => void printTest()}>{t("printer.test_action")}</Button>
        {test?.kind === "sent" ? <Alert tone="success" title={t("print.sent", { printer: test.result.printer })} /> : null}
        {test?.kind === "failed" ? <Alert tone="danger" title={t("print.failed")}>{errorText(test.error)}</Alert> : null}
      </div>
    </section>
  );
}
