import { useCallback, useState } from "react";
import { NavLink, Route, Routes, useLocation } from "react-router-dom";
import { usePreferences, useTranslation } from "@/app/providers/PreferencesProvider";
import { RequirePermission } from "@/app/session/Can";
import { useSession, hasPermission } from "@/app/session/session";
import { useSignOut } from "@/app/session/useSignOut";
import { CommandPalette, useCommandPaletteShortcut } from "@/app/shell/CommandPalette";
import { NAV_GROUPS, ROUTES } from "@/app/shell/routes";
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
  // The palette's open state lives here, not in the Header, because the shortcut is global: it
  // must work while focus is in a table, a form, or nothing at all.
  const [paletteOpen, setPaletteOpen] = useState(false);
  const open = useCallback(() => setPaletteOpen(true), []);
  useCommandPaletteShortcut(open);

  return (
    <div className="flex h-full">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <Header onOpenPalette={open} />
        <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
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
  const location = useLocation();
  const permissions = session?.permissions ?? [];

  const visible = ROUTES.filter(
    (route) => !route.permission || hasPermission(permissions, route.permission),
  );

  const core = visible.filter((route) => !route.advanced);
  const advanced = visible.filter((route) => route.advanced);

  /*
   * The Advanced section opens itself when you are inside it.
   *
   * Without this, following a link to a supplier bill leaves the sidebar showing no trace of
   * where you are — the highlighted item is inside a section that is shut. A person navigating
   * by the menu would have no way back except remembering which fold it came from.
   */
  const insideAdvanced = advanced.some((route) => route.path === location.pathname);

  return (
    <aside className="hidden w-60 shrink-0 flex-col overflow-y-auto border-e border-border bg-surface-raised px-3 py-4 md:flex">
      <p className="px-2 text-lg font-semibold tracking-tight text-text">{t("app.title")}</p>
      <p className="mt-0.5 px-2 text-xs text-text-muted">{t("app.tagline")}</p>

      {/*
       * CORE first, ungrouped and unlabelled.
       *
       * The 10.13 grouping into six sections was an improvement on thirty items in one column,
       * and it was not enough: six headings over thirty items is still thirty items to read.
       *
       * A shop uses four or five screens all day. Those are here, in the order the day runs —
       * the till, what was sold, who owes, what is on the shelf — with no headings at all,
       * because nine items do not need to be told what they are.
       */}
      {/*
       * ONE navigation landmark for the whole sidebar, core and fold together.
       *
       * Two would make a screen-reader user choose between "navigation 1" and "navigation 2"
       * with nothing to tell them apart, and it would mean the menu's own contents depended on
       * whether a section happened to be open.
       */}
      <nav className="mt-6 flex flex-col" aria-label={t("shell.nav.label")}>
        <div className="flex flex-col gap-0.5">
          {core.map((route) => (
            <NavItem
              key={route.path}
              to={route.path}
              label={t(route.labelKey)}
              end={route.path === "/"}
            />
          ))}
        </div>

        {advanced.length > 0 ? (
        <details
          className="mt-4"
          // `key` forces the element to remount when you navigate into or out of the section,
          // which is what lets `open` follow the route. A controlled <details> would need a
          // click handler that fights the browser's own toggle.
          key={insideAdvanced ? "open" : "shut"}
          open={insideAdvanced}
        >
          <summary className="cursor-pointer rounded-lg px-3 py-1.5 text-xs font-semibold uppercase tracking-wider text-text-muted hover:bg-surface-sunken hover:text-text">
            {t("nav.advanced")}
          </summary>

          {/* Still GROUPED inside, because twenty-one items do need telling apart. The grouping
              is the route's own, so there is no second list to keep in step. */}
          <div className="mt-1 flex flex-col gap-4">
            {NAV_GROUPS.map((group) => {
              const items = advanced.filter((route) => route.group === group);
              if (items.length === 0) return null;
              return (
                <div key={group} className="flex flex-col gap-0.5">
                  <h3 className="px-3 pb-1 text-[0.7rem] font-semibold uppercase tracking-wider text-text-muted">
                    {t(`nav.group.${group}`)}
                  </h3>
                  {items.map((route) => (
                    <NavItem key={route.path} to={route.path} label={t(route.labelKey)} />
                  ))}
                </div>
              );
            })}
          </div>
        </details>
        ) : null}
      </nav>
    </aside>
  );
}

function NavItem({ to, label, end }: { to: string; label: string; end?: boolean }) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        "rounded-lg px-3 py-1.5 text-sm transition-colors " +
        (isActive
          ? "bg-primary-subtle font-medium text-text"
          : "text-text-muted hover:bg-surface-sunken hover:text-text")
      }
    >
      {label}
    </NavLink>
  );
}

function Header({ onOpenPalette }: { onOpenPalette: () => void }) {
  const { t, locale, availableLocales, setLocale, theme, setTheme } = usePreferences();
  const session = useSession();
  const signOut = useSignOut();

  return (
    <header className="flex items-center justify-between gap-4 border-b border-border px-6 py-3">
      <h1 className="truncate text-base font-medium text-text">{t("shell.title")}</h1>

      {/*
       * A search-shaped button, not a search box.
       *
       * It looks like a field because that is what people click when they want to find
       * something, and it opens the palette because a field in the header could only ever
       * search RECORDS — leaving screens and actions findable only by reading the sidebar.
       * The shortcut is printed on it so the keyboard route is discoverable without a manual.
       */}
      <button
        type="button"
        onClick={onOpenPalette}
        className={
          "hidden min-w-0 flex-1 items-center justify-between gap-3 rounded-lg border " +
          "border-border bg-surface-raised px-3 py-1.5 text-sm text-text-muted " +
          "hover:border-border-strong sm:flex sm:max-w-md"
        }
      >
        <span className="truncate">{t("command.placeholder")}</span>
        <kbd className="shrink-0 rounded border border-border px-1.5 py-0.5 text-xs">
          {t("command.shortcut")}
        </kbd>
      </button>

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
