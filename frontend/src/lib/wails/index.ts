// The typed binding surface. Every Go call in the application goes through here.
//
// Each function is a thin, typed wrapper over `call`, which performs the single envelope
// unwrap (§3.1). Components never see a Result<T> and never touch window.go.

import { onEvent } from "./bridge";
import { call } from "./call";

export { BindingError, isBindingError } from "./call";
export type { APIError, FieldError, Result } from "./types";

// ── Boot ────────────────────────────────────────────────────────────────────────
//
// The one binding callable before the object graph exists — it is what reports whether the
// graph exists. Everything else returns a typed not-ready error until boot succeeds.

export type BootState = "starting" | "ready" | "failed";

export interface BootStatus {
  state: BootState;
  /** migrate.Phase: checking | backup | migrating | restoring | done */
  phase: string;
  current: number;
  total: number;
  name: string;
  /** Present only when state is "failed". Rendered from code + params, never as prose. */
  error: { code: string; messageKey: string; params?: Record<string, string> } | null;
  /** Where the pre-migration snapshot was written, when a migration failed. */
  backupPath: string;
}

export function bootStatus(): Promise<BootStatus> {
  return call<BootStatus>("Boot", "Status");
}

/**
 * Subscribes to boot progress.
 *
 * Events are an optimisation, not the source of truth: a window that finishes loading after
 * boot completed would never receive one. The boot screen therefore polls `bootStatus()` on
 * mount and treats events as a way to update sooner.
 */
export function onBootEvent(callback: () => void): () => void {
  const offProgress = onEvent("boot:progress", callback);
  const offReady = onEvent("boot:ready", callback);
  const offFailed = onEvent("boot:failed", callback);
  return () => {
    offProgress();
    offReady();
    offFailed();
  };
}

// ── Auth ────────────────────────────────────────────────────────────────────────

export interface SessionInfo {
  signedIn: boolean;
  userId: string;
  username: string;
  displayName: string;
  mustChange: boolean;
  /** Cosmetic only — the backend re-checks every call (§14.3). */
  permissions: string[];
}

/** Signs in. The session token never crosses this boundary; it lives in the Go process. */
export function login(username: string, password: string, remember: boolean): Promise<SessionInfo> {
  return call<SessionInfo>("Auth", "Login", username, password, remember);
}

export function logout(): Promise<boolean> {
  return call<boolean>("Auth", "Logout");
}

/** Asks "am I signed in, and as whom?" — resolves with signedIn:false rather than throwing. */
export function me(): Promise<SessionInfo> {
  return call<SessionInfo>("Auth", "Me");
}

// ── System ──────────────────────────────────────────────────────────────────────

export interface HealthInfo {
  version: string;
  commit: string;
  buildTime: string;
  goVersion: string;
  platform: string;
}

export function health(): Promise<HealthInfo> {
  return call<HealthInfo>("System", "Health");
}

// ── Config ──────────────────────────────────────────────────────────────────────

export type ThemePreference = "light" | "dark" | "system";

export interface Preferences {
  locale: string;
  theme: ThemePreference;
  availableLocales: string[];
}

export function preferences(): Promise<Preferences> {
  return call<Preferences>("Config", "Preferences");
}

export function setLocale(locale: string): Promise<Preferences> {
  return call<Preferences>("Config", "SetLocale", locale);
}

export function setTheme(theme: ThemePreference): Promise<Preferences> {
  return call<Preferences>("Config", "SetTheme", theme);
}

// ── Ops ─────────────────────────────────────────────────────────────────────────

export interface JobState {
  key: string;
  enabled: boolean;
  schedule: string;
  lastRunAt: string;
  nextRunAt: string;
  lastStatus: string;
}

export interface RunRecord {
  runId: string;
  jobKey: string;
  startedAt: string;
  finishedAt: string;
  status: string;
  attempt: number;
  error: string;
  output: string;
  triggeredBy: string;
}

export function jobs(): Promise<JobState[]> {
  return call<JobState[]>("Ops", "Jobs");
}

export function runs(jobKey: string, limit: number): Promise<RunRecord[]> {
  return call<RunRecord[]>("Ops", "Runs", jobKey, limit);
}

// ── Audit ───────────────────────────────────────────────────────────────────────

export interface AuditEntry {
  id: string;
  occurredAt: string;
  actorUserId: string;
  /** A snapshot taken when the entry was written, so old records stay readable. */
  actorName: string;
  correlationId: string;
  action: string;
  entityType: string;
  entityId: string;
  entityLabel: string;
  source: string;

  /**
   * The before/after payload, gated by `audit.entry.view_payload`.
   *
   * OPTIONAL, and that is the contract: the key is ABSENT — not blank — when the signed-in user
   * may not see it. `undefined` therefore means "you may not see this", which is a different
   * fact from an entry that genuinely had no before-state, and the UI must render them
   * differently.
   */
  beforeJson?: string;
  afterJson?: string;
  changedFields?: string;
}

export interface AuditFilter {
  entityType?: string;
  entityId?: string;
  actorId?: string;
  limit?: number;
}

export function auditEntries(filter: AuditFilter = {}): Promise<AuditEntry[]> {
  return call<AuditEntry[]>("Audit", "Entries", {
    entityType: filter.entityType ?? "",
    entityId: filter.entityId ?? "",
    actorId: filter.actorId ?? "",
    limit: filter.limit ?? 0,
  });
}

/**
 * Whether the payload was withheld rather than empty.
 *
 * A helper rather than an inline `=== undefined` at each call site, so every screen renders the
 * distinction the same way — and so the reasoning lives in one place.
 */
export function payloadHidden(entry: AuditEntry): boolean {
  return entry.beforeJson === undefined && entry.afterJson === undefined;
}

// ── Money ───────────────────────────────────────────────────────────────────────

export interface Currency {
  code: string;
  name: string;
  symbol: string;
  decimalPlaces: number;
  symbolPosition: string;
}

export function currencies(): Promise<Currency[]> {
  return call<Currency[]>("Money", "Currencies");
}
