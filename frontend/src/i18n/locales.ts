// Minimal i18n seed for Phase 0. The full catalog is shared with the Go backend
// (locales/{en,ar}/*.json) and loaded via a proper i18n layer in Step 0.11/Phase 1.
export type Locale = "en" | "ar";
export type Direction = "ltr" | "rtl";

export const DIRECTION: Record<Locale, Direction> = {
  en: "ltr",
  ar: "rtl",
};

type Dict = Record<string, string>;

export const MESSAGES: Record<Locale, Dict> = {
  en: {
    "app.title": "Mizan ERP",
    "app.tagline": "Where Precision Meets Simplicity.",
    "shell.status": "Foundation ready",
    "shell.backend": "Backend",
    "shell.version": "Version",
    "shell.platform": "Platform",
    "action.toggleTheme": "Toggle theme",
    "action.toggleLanguage": "العربية",
  },
  ar: {
    "app.title": "ميزان",
    "app.tagline": "حيث تلتقي الدقة بالبساطة.",
    "shell.status": "الأساس جاهز",
    "shell.backend": "الخادم",
    "shell.version": "الإصدار",
    "shell.platform": "المنصة",
    "action.toggleTheme": "تبديل السمة",
    "action.toggleLanguage": "English",
  },
};
