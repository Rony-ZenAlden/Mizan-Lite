import { Link } from "react-router-dom";
import { useLocale } from "@/i18n/LocaleProvider";
import { useNotifications } from "./NotificationProvider";
import { notificationText } from "./text";

/**
 * The toasts (2026-09-23): what just happened, for a few seconds, then gone. Polite, never modal, and in the corner a
 * cashier's eyes are not on — they announce, they do not interrupt. The owner removed a banner in 0.9.8 for standing
 * between the cashier and the customer; these are built not to.
 */
export function Toasts() {
  const { toasts, dismiss } = useNotifications();
  const { t, locale } = useLocale();
  if (toasts.length === 0) return null;
  return (
    <div className="pointer-events-none fixed bottom-4 end-4 z-50 flex w-80 flex-col gap-2" data-testid="toasts">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          role="status"
          className="pointer-events-auto flex items-start gap-2 rounded-md border border-border bg-surface-raised p-3 text-sm shadow-lg"
          data-testid="toast"
        >
          <p className="flex-1">
            {toast.notification ? notificationText(toast.notification, t, locale) : t("alerts.summary", { count: String(toast.count) })}{" "}
            <Link to="/notifications" className="underline" onClick={() => dismiss(toast.id)}>
              {t("alerts.open")}
            </Link>
          </p>
          <button type="button" aria-label={t("alerts.dismiss")} onClick={() => dismiss(toast.id)} className="text-text-muted">
            ×
          </button>
        </div>
      ))}
    </div>
  );
}
