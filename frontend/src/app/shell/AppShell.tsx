import { useEffect, useState } from "react";
import { usePreferences, useTranslation } from "@/app/providers/PreferencesProvider";
import { health, type HealthInfo, type ThemePreference } from "@/lib/wails";
import { JobStatusPanel } from "@/modules/ops/JobStatusPanel";
import { Button, Select, Tabs, Tooltip } from "@/shared/ui";
import type { Locale } from "@/i18n/messages";

/**
 * The application shell: sidebar, header, content.
 *
 * A real layout rather than a centred card, because the point of the shell is to be the frame
 * every Phase 1 module drops into. The navigation region is deliberately empty — there are no
 * modules to navigate to yet, and inventing placeholder routes would mean deleting them.
 */
export function AppShell() {
  const { t } = useTranslation();
  const [tab, setTab] = useState("system");

  return (
    <div className="flex h-full">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <Header />
        <main className="flex-1 overflow-y-auto p-6">
          <Tabs
            value={tab}
            onValueChange={setTab}
            items={[
              { value: "system", label: t("shell.nav.system"), content: <SystemPanel /> },
              { value: "jobs", label: t("shell.nav.jobs"), content: <JobStatusPanel /> },
            ]}
          />
        </main>
      </div>
    </div>
  );
}

function Sidebar() {
  const { t } = useTranslation();
  return (
    <aside className="hidden w-56 shrink-0 flex-col border-e border-border bg-surface-raised p-4 md:flex">
      <p className="text-lg font-semibold text-text">{t("app.title")}</p>
      <p className="mt-1 text-xs text-text-muted">{t("app.tagline")}</p>
      {/* Navigation lands with the first module that has a screen (Phase 1). */}
      <nav className="mt-6 flex flex-col gap-1" aria-label={t("shell.nav.label")} />
    </aside>
  );
}

function Header() {
  const { t, locale, availableLocales, setLocale, theme, setTheme } = usePreferences();

  return (
    <header className="flex items-center justify-between gap-4 border-b border-border px-6 py-3">
      <h1 className="truncate text-base font-medium text-text">{t("shell.title")}</h1>
      <div className="flex items-center gap-2">
        <Select
          value={locale}
          onValueChange={(next) => void setLocale(next as Locale)}
          ariaLabel={t("action.changeLanguage")}
          options={availableLocales.map((code) => ({
            value: code,
            // Languages are named in their own language — a user looking for Arabic is
            // looking for "العربية", not for "Arabic" spelled in a script they may not read.
            label: t(`locale.name.${code}`),
          }))}
        />
        <ThemeToggle theme={theme} onChange={setTheme} label={t("action.toggleTheme")} />
      </div>
    </header>
  );
}

const THEME_ORDER: ThemePreference[] = ["light", "dark", "system"];
const THEME_ICON: Record<ThemePreference, string> = { light: "☀", dark: "☾", system: "◐" };

function ThemeToggle({
  theme,
  onChange,
  label,
}: {
  theme: ThemePreference;
  onChange: (theme: ThemePreference) => Promise<void>;
  label: string;
}) {
  const next =
    THEME_ORDER[(THEME_ORDER.indexOf(theme) + 1) % THEME_ORDER.length] ?? "system";
  return (
    <Tooltip label={label}>
      <Button
        variant="ghost"
        size="sm"
        aria-label={label}
        onClick={() => void onChange(next)}
      >
        <span aria-hidden="true">{THEME_ICON[theme]}</span>
      </Button>
    </Tooltip>
  );
}

/** The health screen — the envelope's first consumer, now reading through the wrapper. */
function SystemPanel() {
  const { t } = useTranslation();
  const [info, setInfo] = useState<HealthInfo | null>(null);

  useEffect(() => {
    health()
      .then(setInfo)
      .catch(() => setInfo(null));
  }, []);

  return (
    <dl className="grid max-w-md grid-cols-2 gap-2 text-sm">
      <Row label={t("shell.version")} value={info?.version} />
      <Row label={t("shell.platform")} value={info?.platform} />
      <Row label={t("shell.backend")} value={info?.goVersion} />
      <Row label={t("shell.commit")} value={info?.commit} />
    </dl>
  );
}

function Row({ label, value }: { label: string; value?: string }) {
  return (
    <>
      <dt className="text-text-muted">{label}</dt>
      <dd className="font-mono text-text">{value ?? "—"}</dd>
    </>
  );
}
