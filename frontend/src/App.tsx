import { useEffect, useState } from "react";
import { DIRECTION, translate, type Locale } from "@/i18n/messages";
import { getHealth, type HealthInfo } from "@/lib/health";

type Theme = "light" | "dark";

export default function App() {
  const [locale, setLocale] = useState<Locale>("en");
  const [theme, setTheme] = useState<Theme>("light");
  const [health, setHealth] = useState<HealthInfo | null>(null);

  const t = (key: string) => translate(locale, key);

  // Drive <html lang/dir/data-theme> — runtime locale + theme switching, no reload.
  useEffect(() => {
    const root = document.documentElement;
    root.lang = locale;
    root.dir = DIRECTION[locale];
    root.dataset.theme = theme;
  }, [locale, theme]);

  useEffect(() => {
    getHealth().then(setHealth).catch(() => setHealth(null));
  }, []);

  return (
    <div className="min-h-full flex flex-col items-center justify-center gap-8 p-8">
      <header className="text-center">
        <h1 className="text-4xl font-bold text-primary">{t("app.title")}</h1>
        <p className="mt-2 text-text-muted">{t("app.tagline")}</p>
      </header>

      <section className="w-full max-w-md rounded-lg border border-border bg-surface-raised p-6">
        <div className="flex items-center gap-2">
          <span className="inline-block h-2 w-2 rounded-full bg-success" />
          <span className="font-medium">{t("shell.status")}</span>
        </div>
        <dl className="mt-4 space-y-1 text-sm">
          <Row label={t("shell.version")} value={health?.version ?? "…"} />
          <Row label={t("shell.platform")} value={health?.platform ?? "…"} />
          <Row label={t("shell.backend")} value={health?.goVersion ?? "…"} />
        </dl>
      </section>

      <div className="flex gap-3">
        <button
          type="button"
          onClick={() => setLocale((l) => (l === "en" ? "ar" : "en"))}
          className="rounded border border-border px-4 py-2 text-sm hover:bg-surface-raised"
        >
          {t("action.toggleLanguage")}
        </button>
        <button
          type="button"
          onClick={() => setTheme((th) => (th === "light" ? "dark" : "light"))}
          className="rounded border border-border px-4 py-2 text-sm hover:bg-surface-raised"
        >
          {t("action.toggleTheme")}
        </button>
      </div>
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-4">
      <dt className="text-text-muted">{label}</dt>
      <dd className="font-mono">{value}</dd>
    </div>
  );
}
