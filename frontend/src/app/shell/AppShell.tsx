import { NavLink, Route, Routes } from "react-router-dom";
import { usePreferences, useTranslation } from "@/app/providers/PreferencesProvider";
import { RequirePermission } from "@/app/session/Can";
import { useSession, hasPermission } from "@/app/session/session";
import { useSignOut } from "@/app/session/useSignOut";
import { ROUTES } from "@/app/shell/routes";
import { type ThemePreference } from "@/lib/wails";
import { Button, Select, Tooltip } from "@/shared/ui";
import type { Locale } from "@/i18n/messages";

/**
 * The application shell: sidebar, header, content.
 *
 * The navigation region was deliberately empty through Phase 0 — there were no modules to
 * navigate to, and placeholder routes would have been deleted. Step 1.11 fills it from ROUTES,
 * so the menu and the router are generated from ONE declaration and cannot disagree about what
 * exists.
 */
export function AppShell() {
  return (
    <div className="flex h-full">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <Header />
        <main className="flex-1 overflow-y-auto p-6">
          <Routes>
            {ROUTES.map((route) => (
              <Route
                key={route.path}
                path={route.path}
                element={
                  route.permission ? (
                    // Belt AND braces, both cosmetic: the menu item is hidden, and the route
                    // still explains itself if reached another way. Neither is enforcement —
                    // every binding re-checks on the Go side (§14.3).
                    <RequirePermission permission={route.permission}>
                      {route.element}
                    </RequirePermission>
                  ) : (
                    route.element
                  )
                }
              />
            ))}
          </Routes>
        </main>
      </div>
    </div>
  );
}

/**
 * The navigation menu, filtered by permission (§FE.3).
 *
 * COSMETIC. It hides what would fail, so a cashier does not learn to ignore errors by clicking
 * a menu item that always refuses. It protects nothing.
 */
function Sidebar() {
  const { t } = useTranslation();
  const session = useSession();
  const permissions = session?.permissions ?? [];

  const visible = ROUTES.filter(
    (route) => !route.permission || hasPermission(permissions, route.permission),
  );

  return (
    <aside className="hidden w-56 shrink-0 flex-col border-e border-border bg-surface-raised p-4 md:flex">
      <p className="text-lg font-semibold text-text">{t("app.title")}</p>
      <p className="mt-1 text-xs text-text-muted">{t("app.tagline")}</p>
      <nav className="mt-6 flex flex-col gap-1" aria-label={t("shell.nav.label")}>
        {visible.map((route) => (
          <NavLink
            key={route.path}
            to={route.path}
            end={route.path === "/"}
            className={({ isActive }) =>
              "rounded px-3 py-2 text-sm " +
              (isActive ? "bg-surface-sunken font-medium text-text" : "text-text-muted")
            }
          >
            {t(route.labelKey)}
          </NavLink>
        ))}
      </nav>
    </aside>
  );
}

function Header() {
  const { t, locale, availableLocales, setLocale, theme, setTheme } = usePreferences();
  const session = useSession();
  const signOut = useSignOut();

  return (
    <header className="flex items-center justify-between gap-4 border-b border-border px-6 py-3">
      <h1 className="truncate text-base font-medium text-text">{t("shell.title")}</h1>
      <div className="flex items-center gap-2">
        {session ? (
          <>
            <span className="hidden truncate text-sm text-text-muted sm:inline">
              {session.displayName || session.username}
            </span>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => signOut.mutate()}
              loading={signOut.isPending}
            >
              {t("auth.signOut")}
            </Button>
          </>
        ) : null}
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
