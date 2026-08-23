import { useState } from "react";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { type ExportedFile, type ExportFormat } from "@/lib/wails";
import { Button } from "./Button";
import { cn } from "./cn";

/**
 * One control that offers a report in every format it can be had in.
 *
 * # Why the bytes come back rather than a path
 *
 * The backend hands over the FILE, base64 in the envelope, and this saves it through the
 * browser's own download. The alternative — Go writing to disk and returning where it went —
 * means this process choosing a directory on somebody's machine, which is a permission prompt on
 * macOS, a different folder on Windows, and a surprise on both.
 *
 * # Why a menu and not three buttons
 *
 * Three buttons in a page header is three things to read every time, to make a choice most people
 * make once. The default is CSV because it is what an accountant's own system reads; the other two
 * are one click away for the times somebody is sending the file to a person rather than a system.
 */
export function ExportMenu({
  onExport,
  disabled,
}: {
  /** Asks the backend for the file. The caller owns which report is being exported. */
  onExport: (format: ExportFormat) => Promise<ExportedFile>;
  disabled?: boolean;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState<ExportFormat | null>(null);
  const [failed, setFailed] = useState(false);

  async function run(format: ExportFormat) {
    setBusy(format);
    setFailed(false);
    try {
      save(await onExport(format));
      setOpen(false);
    } catch {
      // The reason belongs to the caller's own error surface; this control only reports that the
      // file did not arrive, because a menu is not a place to explain a backend failure.
      setFailed(true);
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="relative inline-flex flex-col items-end">
      <Button
        variant="secondary"
        size="sm"
        disabled={disabled}
        aria-expanded={open}
        aria-haspopup="menu"
        onClick={() => setOpen((current) => !current)}
      >
        {t("export.label")}
      </Button>

      {open ? (
        <div
          role="menu"
          className={cn(
            "absolute top-full z-20 mt-1 flex min-w-44 flex-col overflow-hidden",
            "rounded-lg border border-border bg-surface shadow-lg",
          )}
        >
          {FORMATS.map((format) => (
            <button
              key={format}
              type="button"
              role="menuitem"
              disabled={busy !== null}
              onClick={() => void run(format)}
              className={cn(
                "px-3 py-2 text-start text-sm text-text transition-colors",
                "hover:bg-surface-sunken disabled:opacity-60",
              )}
            >
              {busy === format ? t("export.working") : t(`export.format.${format}`)}
            </button>
          ))}
        </div>
      ) : null}

      {failed ? (
        <p role="alert" className="mt-1 text-xs text-danger">
          {t("export.failed")}
        </p>
      ) : null}
    </div>
  );
}

const FORMATS: ExportFormat[] = ["csv", "xlsx", "docx"];

/**
 * Saves a returned file through the browser's own download.
 *
 * A blob and an object URL rather than a `data:` href: a stock valuation for a real shop runs to
 * megabytes, and a data URL that long is refused outright by some webviews and silently truncated
 * by others — which produces a file that opens and is missing its last rows.
 *
 * The object URL is revoked on the next tick. Revoking it synchronously cancels the download in
 * WebKit, which is the sort of thing that works on the machine it was written on.
 */
function save(file: ExportedFile) {
  const binary = atob(file.contentBase64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }

  const url = URL.createObjectURL(new Blob([bytes], { type: file.mimeType }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = file.filename;
  document.body.appendChild(anchor);
  anchor.click();
  document.body.removeChild(anchor);
  setTimeout(() => URL.revokeObjectURL(url), 0);
}
