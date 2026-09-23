import type { Notification } from "@/api/client";
import type { MessageKey } from "@/i18n/messages";

type T = (key: MessageKey, params?: Record<string, string>) => string;

/** What a notification says, in the reader's language — one sentence, the same in a toast and in the centre. */
export function notificationText(n: Notification, t: T, locale: "ar" | "en"): string {
  switch (n.kind) {
    case "low_stock":
      return t("alerts.low_stock", { name: locale === "en" && n.nameEn ? n.nameEn : n.nameAr });
    case "stale_prices":
      return t("alerts.stale_prices", { count: String(n.count) });
    case "capital":
      return t("alerts.capital");
    case "backup_none":
      return t("backups.warning_none");
    case "backup_old":
      return t("backups.warning_old");
    default:
      return t("alerts.generic");
  }
}
