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
