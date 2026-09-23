import { Link } from "react-router-dom";
import { useLocale } from "@/i18n/LocaleProvider";
import { useNotifications } from "./NotificationProvider";

/** The bell in the header, with how many notifications are unread (2026-09-23). It opens the notification centre. */
export function Bell() {
  const { unread } = useNotifications();
  const { t } = useLocale();
  return (
    <Link
      to="/notifications"
      className="relative rounded-md p-2 hover:bg-surface"
      aria-label={unread > 0 ? t("alerts.bell_unread", { count: String(unread) }) : t("alerts.bell")}
      data-testid="bell"
    >
      <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
        <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
        <path d="M13.73 21a2 2 0 0 1-3.46 0" />
      </svg>
      {unread > 0 ? (
        <span
          className="absolute -end-1 -top-1 min-w-5 rounded-full bg-danger px-1 text-center text-xs font-semibold text-white"
          data-testid="bell-count"
        >
          <bdi dir="ltr">{unread > 99 ? "99+" : unread}</bdi>
        </span>
      ) : null}
    </Link>
  );
}
