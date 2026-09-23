import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useLocation } from "react-router-dom";
import { useClient } from "@/api/ClientContext";
import type { AlertsState, Notification } from "@/api/client";
import { useRate } from "@/rates/RateProvider";

/**
 * The notification engine as the screen holds it (the owner's request, 2026-09-23): one list from Go, which the bell
 * counts, the toasts announce and the notification centre shows — so the three can never disagree.
 *
 * # Read and unread
 *
 * A notification is read when its key AND fingerprint have been seen. Go changes the fingerprint when the condition
 * changes materially — a new exchange rate, a capital shift into the next band, another day without a backup — so a
 * notification that gets worse is unread again, and one that is merely still true is not announced twice. What has been
 * seen lives in this computer's webview, like the open cart: it is a person's convenience, not a fact about the shop.
 *
 * # Toasts
 *
 * Only what is NEW and UNREAD is announced — a notification that appeared, or changed, since the last look. The first
 * look of a session announces one summary rather than one toast per item, so opening the application on a morning with
 * twelve low products is one line, not twelve.
 *
 * # When Go is asked
 *
 * Every minute, on every change of screen, when the exchange rate changes, and whenever a screen that just changed
 * something asks (the till, after a sale). That is what makes a toast arrive while it is still news.
 */
export interface Toast {
  id: number;
  /** The notification, or null for a summary. */
  notification: Notification | null;
  /** For a summary toast, how many notifications it stands for. */
  count: number;
}

interface NotificationsState {
  alerts: AlertsState | null;
  unread: number;
  isUnread: (n: Notification) => boolean;
  markAllRead: () => void;
  refresh: () => Promise<void>;
  toasts: Toast[];
  dismiss: (id: number) => void;
}

const Context = createContext<NotificationsState | null>(null);

export const SEEN_KEY = "mizan.lite.alerts.seen";
/** How often the engine is asked, when nothing else asks sooner. */
export const POLL_MS = 60_000;
/** How long a toast stays: long enough to read at a busy counter, short enough never to need dismissing. */
export const TOAST_MS = 8_000;
/** At most this many toasts at once; more new notifications than this are folded into one summary. */
export const MAX_TOASTS = 3;

type Seen = Record<string, string>;

function readSeen(): Seen {
  try {
    const raw = window.localStorage.getItem(SEEN_KEY);
    const parsed = raw ? (JSON.parse(raw) as unknown) : {};
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Seen) : {};
  } catch {
    return {};
  }
}

function writeSeen(seen: Seen): void {
  try {
    window.localStorage.setItem(SEEN_KEY, JSON.stringify(seen));
  } catch {
    // A bell that cannot remember what was read still rings: unread is the safe side to fail on.
  }
}

let toastCounter = 0;

export function NotificationProvider({ children }: { children: ReactNode }) {
  const client = useClient();
  const { pathname } = useLocation();
  const { rate } = useRate();
  const [alerts, setAlerts] = useState<AlertsState | null>(null);
  // What has been read: the ref is the truth the callbacks read, the state is what renders. Kept apart so marking read,
  // and reading, never make a callback new — a new markAllRead would re-run the centre's effect, which marks read again.
  const seenRef = useRef<Seen | null>(null);
  if (seenRef.current === null) seenRef.current = readSeen();
  const [seen, setSeen] = useState<Seen>(seenRef.current);
  const [toasts, setToasts] = useState<Toast[]>([]);
  // What this session has already looked at, key → fingerprint; null before the first look.
  const lastLook = useRef<Map<string, string> | null>(null);
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());
  const mounted = useRef(true);

  const dismiss = useCallback((id: number) => setToasts((list) => list.filter((x) => x.id !== id)), []);

  const saveSeen = useCallback((next: Seen) => {
    seenRef.current = next;
    writeSeen(next);
    setSeen(next);
  }, []);

  // Each toast leaves on its own, TOAST_MS after it arrived, whatever arrives after it.
  const push = useCallback(
    (items: Omit<Toast, "id">[]) => {
      const added = items.map((item) => ({ ...item, id: ++toastCounter }));
      setToasts((list) => [...list, ...added].slice(-MAX_TOASTS));
      for (const toast of added) {
        const timer = setTimeout(() => {
          timers.current.delete(timer);
          dismiss(toast.id);
        }, TOAST_MS);
        timers.current.add(timer);
      }
    },
    [dismiss],
  );

  const announce = useCallback(
    (list: Notification[], seenNow: Seen) => {
      const previous = lastLook.current;
      lastLook.current = new Map(list.map((n) => [n.key, n.fingerprint]));
      const unread = list.filter((n) => seenNow[n.key] !== n.fingerprint);
      // The first look of the session: what is unread, summarised. After it: what is new since the last look and not
      // already read — owner mode showing the capital again is not news to an owner who has read it.
      const fresh = previous === null ? unread : unread.filter((n) => previous.get(n.key) !== n.fingerprint);
      if (fresh.length === 0) return;
      if (fresh.length === 1) push([{ notification: fresh[0]!, count: 1 }]);
      else if (previous === null || fresh.length > MAX_TOASTS) push([{ notification: null, count: fresh.length }]);
      else push(fresh.map((n) => ({ notification: n, count: 1 })));
    },
    [push],
  );

  const refresh = useCallback(async () => {
    try {
      const next = await client.alerts.current();
      if (!mounted.current) return;
      // A notification that went away and comes back is new again: forget what was seen of anything not present. Not
      // while owner figures are held back, though — the capital is absent then because it is hidden, not because it
      // went away, and forgetting it would announce it again at the next unlock.
      let current = seenRef.current ?? {};
      if (!next.ownerHidden) {
        const present = new Set(next.notifications.map((n) => n.key));
        const kept = Object.entries(current).filter(([k]) => present.has(k));
        if (kept.length !== Object.keys(current).length) {
          current = Object.fromEntries(kept);
          saveSeen(current);
        }
      }
      setAlerts(next);
      announce(next.notifications, current);
    } catch {
      // A failed look keeps the last list: the bell is not worth interrupting a sale over.
    }
  }, [client, announce, saveSeen]);

  useEffect(() => {
    mounted.current = true;
    const timer = setInterval(() => void refresh(), POLL_MS);
    const pending = timers.current;
    return () => {
      mounted.current = false;
      clearInterval(timer);
      for (const t of pending) clearTimeout(t);
      pending.clear();
    };
  }, [refresh]);

  // A change of screen is a moment something may have changed.
  useEffect(() => {
    void refresh();
  }, [refresh, pathname]);

  // So is a new exchange rate: it is what makes prices stale. The first read of the rate is not a new one.
  const rateText = rate?.set ? rate.rate : "";
  const lastRate = useRef<string | null>(null);
  useEffect(() => {
    if (rateText === "") return;
    if (lastRate.current !== null && lastRate.current !== rateText) void refresh();
    lastRate.current = rateText;
  }, [refresh, rateText]);

  const isUnread = useCallback((n: Notification) => seen[n.key] !== n.fingerprint, [seen]);

  // Idempotent: with nothing unread it changes nothing, so a screen may call it on every render of new alerts.
  const markAllRead = useCallback(() => {
    if (!alerts) return;
    const current = seenRef.current ?? {};
    if (alerts.notifications.every((n) => current[n.key] === n.fingerprint)) return;
    const next = { ...current };
    for (const n of alerts.notifications) next[n.key] = n.fingerprint;
    saveSeen(next);
  }, [alerts, saveSeen]);

  const value = useMemo<NotificationsState>(
    () => ({
      alerts,
      unread: alerts ? alerts.notifications.filter((n) => seen[n.key] !== n.fingerprint).length : 0,
      isUnread,
      markAllRead,
      refresh,
      toasts,
      dismiss,
    }),
    [alerts, seen, isUnread, markAllRead, refresh, toasts, dismiss],
  );
  return <Context.Provider value={value}>{children}</Context.Provider>;
}

/** The notifications, from inside the provider. */
export function useNotifications(): NotificationsState {
  const v = useContext(Context);
  if (!v) throw new Error("useNotifications outside NotificationProvider");
  return v;
}

/** The notifications if there is a provider, or null — for screens that render without one (their own tests). */
export function useOptionalNotifications(): NotificationsState | null {
  return useContext(Context);
}
