import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { useSession } from "@/app/session/session";
import { availableActions, destinationFor, navigableRoutes } from "@/app/shell/commands";
import { globalSearch } from "@/lib/wails";
import { Alert, Dialog } from "@/shared/ui";
import { cn } from "@/shared/ui/cn";

/**
 * One thing the palette can do.
 *
 * Flattened across sections deliberately: the arrow keys move through a single list, and a
 * per-section index would have to know where each section ends — which is the kind of
 * bookkeeping that goes wrong the moment a section is empty.
 */
interface Item {
  id: string;
  label: string;
  hint: string;
  run: () => void;
}

interface Section {
  titleKey: string;
  items: Item[];
}

/**
 * The command palette: everywhere you can go and everything you can do, from the keyboard.
 *
 * # Why this replaced the search box rather than sitting beside it
 *
 * The 10.12 audit found a search component that was mounted nowhere. Adding it to the header
 * would have given the shell two overlapping affordances — a box that finds records and a menu
 * that finds screens — and a user hunting for "suppliers" would have had to know which of the
 * two knows about it. One surface answers both questions.
 *
 * # What it does NOT do
 *
 * It navigates. It does not create, post, or delete anything. A palette that can post an invoice
 * from a fuzzy match is a palette that will eventually post the wrong one, and the "did you
 * mean" moment arrives after the ledger has been written. Every action here lands the user on the
 * screen that does the work, with the work still to confirm.
 *
 * # Permissions are cosmetic here, as everywhere in the shell
 *
 * Filtering hides what would refuse (§FE.3). It protects nothing — every binding re-checks on
 * the Go side (§14.3) — and the reason to do it is that a shortcut that always fails teaches
 * people to ignore errors.
 */
export function CommandPalette({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const session = useSession();
  const permissions = useMemo(() => session?.permissions ?? [], [session]);

  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const listRef = useRef<HTMLDivElement>(null);

  // A fresh palette every time. Reopening onto the last search is the behaviour people complain
  // about in every application that has it: the box is already full, and the first keystroke
  // appends to a query they have forgotten they typed.
  useEffect(() => {
    if (open) {
      setQuery("");
      setActive(0);
    }
  }, [open]);

  const trimmed = query.trim();
  const needle = trimmed.toLowerCase();

  // Two characters is the backend's minimum. The check is duplicated here only to avoid a round
  // trip that would be refused; the backend's is the one that matters.
  const searching = trimmed.length >= 2;
  const found = useQuery({
    queryKey: ["insight", "search", trimmed],
    queryFn: () => globalSearch(trimmed),
    enabled: open && searching,
  });

  // Memoised because the section list depends on it, and a fresh closure every render would
  // rebuild every item on every keystroke — which is also what the exhaustive-deps rule is
  // pointing at when it asks for `go`.
  const go = useCallback(
    (to: string) => {
      onOpenChange(false);
      navigate(to);
    },
    [navigate, onOpenChange],
  );

  const sections = useMemo<Section[]>(() => {
    const matches = (label: string) => needle === "" || label.toLowerCase().includes(needle);

    const actions: Item[] = availableActions(permissions)
      .map((action) => ({
        id: `action:${action.key}`,
        label: t(`command.action.${action.key}`),
        hint: t("command.hint.action"),
        run: () => go(action.to),
      }))
      .filter((item) => matches(item.label));

    const places: Item[] = navigableRoutes(permissions)
      .map((route) => ({
        id: `route:${route.path}`,
        label: t(route.labelKey),
        hint: t(`nav.group.${route.group}`),
        run: () => go(route.path),
      }))
      .filter((item) => matches(item.label));

    // Records come only from the backend. Filtering them here would be filtering a list that is
    // already the answer to this exact query.
    const records: Item[] = (found.data?.results ?? [])
      .map((result) => {
        const to = destinationFor(result.kind);
        if (!to) return undefined;
        return {
          id: `record:${result.kind}:${result.id}`,
          label: result.label,
          hint: result.subtitle
            ? `${t(`search.kind.${result.kind}`)} · ${result.subtitle}`
            : t(`search.kind.${result.kind}`),
          run: () => go(to),
        };
      })
      .filter((item): item is Item => item !== undefined);

    return [
      { titleKey: "command.section.actions", items: actions },
      { titleKey: "command.section.places", items: places },
      { titleKey: "command.section.records", items: records },
    ].filter((section) => section.items.length > 0);
  }, [needle, permissions, t, found.data, go]);

  const flat = useMemo(() => sections.flatMap((section) => section.items), [sections]);

  // The highlight must never point past the end of a list that just got shorter, or Enter runs
  // nothing and the palette looks frozen.
  useEffect(() => {
    setActive((current) => (current >= flat.length ? 0 : current));
  }, [flat.length]);

  // Keep the highlighted row on screen. Without this, arrowing down a thirty-item list moves a
  // highlight the user cannot see.
  useEffect(() => {
    listRef.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block: "nearest" });
  }, [active]);

  function onKeyDown(event: ReactKeyboardEvent) {
    if (flat.length === 0) return;
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActive((current) => (current + 1) % flat.length);
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive((current) => (current - 1 + flat.length) % flat.length);
    } else if (event.key === "Enter") {
      event.preventDefault();
      flat[active]?.run();
    }
  }

  let index = -1;

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("command.title")}
      description={t("command.help")}
    >
      {/*
       * The key handler sits on the WRAPPER rather than on the input, so the arrow keys still
       * move the highlight after focus has moved to a row by hovering. Keydown bubbles, so one
       * handler covers the whole panel.
       */}
      <div className="flex flex-col gap-3" onKeyDown={onKeyDown}>
        <input
          type="search"
          autoComplete="off"
          value={query}
          aria-label={t("command.title")}
          placeholder={t("command.placeholder")}
          onChange={(event) => {
            setQuery(event.target.value);
            setActive(0);
          }}
          className={cn(
            "w-full rounded-lg border border-border bg-surface-raised px-3 py-2",
            "text-base text-text placeholder:text-text-muted",
          )}
        />

        {/*
         * "Nothing found" and "search is broken" are different answers, and the backend
         * distinguishes them. A palette that showed the first when the second is true would send
         * somebody looking for a record that is there.
         */}
        {found.data && found.data.failed.length > 0 ? (
          <Alert tone="warning" title={t("search.partial")}>
            {t("search.partialHelp", { sources: found.data.failed.join(", ") })}
          </Alert>
        ) : null}

        <div ref={listRef} className="max-h-80 overflow-y-auto" role="listbox" tabIndex={-1}>
          {flat.length === 0 ? (
            <p className="px-1 py-6 text-center text-sm text-text-muted">
              {searching && found.isPending ? t("gate.checking") : t("command.none")}
            </p>
          ) : (
            sections.map((section) => (
              <div key={section.titleKey} className="mb-2">
                <p className="px-2 py-1 text-[0.7rem] font-semibold uppercase tracking-wider text-text-muted">
                  {t(section.titleKey)}
                </p>
                {section.items.map((item) => {
                  index += 1;
                  const isActive = index === active;
                  const position = index;
                  return (
                    <button
                      key={item.id}
                      type="button"
                      role="option"
                      aria-selected={isActive}
                      data-active={isActive}
                      onMouseEnter={() => setActive(position)}
                      onClick={item.run}
                      className={cn(
                        "flex w-full items-baseline justify-between gap-3 rounded-lg px-2 py-2 text-start",
                        isActive ? "bg-primary-subtle" : "hover:bg-surface-sunken",
                      )}
                    >
                      <span className="truncate text-sm text-text">{item.label}</span>
                      <span className="shrink-0 text-xs text-text-muted">{item.hint}</span>
                    </button>
                  );
                })}
              </div>
            ))
          )}
        </div>
      </div>
    </Dialog>
  );
}

/**
 * Cmd+K on macOS, Ctrl+K everywhere else.
 *
 * `metaKey || ctrlKey` rather than platform detection: the shortcut people reach for is the one
 * their other applications use, and sniffing the platform to REFUSE the other one gains nothing.
 *
 * preventDefault matters — Ctrl+K is a browser shortcut, and in a WebView that inherits it the
 * palette would open behind whatever the host decided to do.
 */
export function useCommandPaletteShortcut(onOpen: () => void) {
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key.toLowerCase() !== "k") return;
      if (!event.metaKey && !event.ctrlKey) return;
      event.preventDefault();
      onOpen();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onOpen]);
}
