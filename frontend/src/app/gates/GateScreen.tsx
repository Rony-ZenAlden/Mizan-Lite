import { useTranslation } from "@/app/providers/PreferencesProvider";
import { isBindingError } from "@/lib/wails";
import { Alert, Spinner } from "@/shared/ui";

/**
 * The full-window states a gate shows while it decides, or when it cannot.
 *
 * Shared by every gate so that "we are still asking" and "we could not ask" look the same
 * wherever they happen. Three gates each inventing their own spinner would drift, and the
 * differences would read to a user as three different kinds of problem.
 */
export function GateScreen({
  kind,
  error,
}: {
  kind: "loading" | "error";
  error?: unknown;
}) {
  const { t } = useTranslation();

  if (kind === "loading") {
    return (
      // Labelled rather than given its own role="status": Spinner already carries one, and two
      // nested status regions announce the same thing twice to a screen reader.
      <div
        className="flex h-full flex-col items-center justify-center gap-3 p-8"
        aria-busy="true"
      >
        <Spinner size="lg" label={t("gate.checking")} />
        <p className="text-sm text-text-muted">{t("gate.checking")}</p>
      </div>
    );
  }

  return (
    <div className="flex h-full items-center justify-center p-8">
      <div className="w-full max-w-lg">
        <Alert tone="danger" title={t("gate.failed.title")}>
          {/* Rendered from the CODE, never from prose (§22.2) — so the message a customer reads
              under stress is in their own language. */}
          {isBindingError(error) ? t(error.messageKey, error.params) : t("app.unknown_error")}
        </Alert>
      </div>
    </div>
  );
}
