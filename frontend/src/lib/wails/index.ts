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

// ── Setup ───────────────────────────────────────────────────────────────────────
//
// The wizard. Both methods are Public on the Go side (§WIZ.1's bootstrap paradox): no user
// exists on a fresh install, so nothing about first-run setup can be permissioned. `Apply`
// carries a second gate there — it refuses once a company exists, forever.

export interface SetupCountry {
  code: string;
  /** A translation key, not a name: the country list is translated like everything else. */
  nameKey: string;
  defaultLocale: string;
  supportedLocales: string[];
  functionalCurrency: string;
  pricingCurrency: string;
  fiscalYearStart: number;
  dateFormat: string;
  firstDayOfWeek: number;
  phoneCode: string;
}

export interface SetupBusinessProfile {
  code: string;
  nameKey: string;
  name: string;
  description: string;
}

export interface SetupCurrency {
  code: string;
  name: string;
  symbol: string;
}

export interface SetupOptions {
  countries: SetupCountry[];
  businessProfiles: SetupBusinessProfile[];
  currencies: SetupCurrency[];
  locales: string[];
}

export interface SetupStatus {
  required: boolean;
  /** Present ONLY while setup is required — the backend stops volunteering it afterwards. */
  options?: SetupOptions;
}

export interface SetupInput {
  locale: string;
  countryCode: string;
  companyCode: string;
  companyName: string;
  legalName: string;
  taxNumber: string;
  functionalCurrency: string;
  pricingCurrency: string;
  businessProfile: string;
  fiscalYearStartMonth: number;
  fiscalYearStartYear: number;
  branchCode: string;
  branchName: string;
  warehouseCode: string;
  warehouseName: string;
  adminUsername: string;
  adminDisplayName: string;
  adminPassword: string;
}

export interface SetupResult {
  companyId: string;
  branchId: string;
  warehouseId: string;
  adminUserId: string;
}

export function setupStatus(): Promise<SetupStatus> {
  return call<SetupStatus>("Setup", "Status");
}

export function applySetup(input: SetupInput): Promise<SetupResult> {
  return call<SetupResult>("Setup", "Apply", input);
}

// ── Identity (administration) ───────────────────────────────────────────────────

export interface User {
  id: string;
  username: string;
  displayName: string;
  email: string;
  isActive: boolean;
  /** The setup administrator: protected from deletion, not from deactivation. */
  isSystem: boolean;
}

export interface Role {
  id: string;
  code: string;
  name: string;
  description: string;
  isSystem: boolean;
  isActive: boolean;
}

export interface PermissionEntry {
  code: string;
  module: string;
  /** Declared by an older build and no longer by this one. Shown, not hidden. */
  obsolete: boolean;
}

export interface AdminSession {
  id: string;
  userId: string;
  username: string;
  displayName: string;
  startedAt: string;
  lastSeen: string;
  expiresAt: string;
  deviceInfo: string;
  /** The session making the call. Ending it signs you out. */
  current: boolean;
}

export interface NewUser {
  username: string;
  displayName: string;
  email: string;
  password: string;
  roleCode: string;
}

export function users(): Promise<User[]> {
  return call<User[]>("Identity", "Users");
}

export function createUser(input: NewUser): Promise<User> {
  return call<User>("Identity", "CreateUser", input);
}

export function setUserActive(userId: string, active: boolean): Promise<boolean> {
  return call<boolean>("Identity", "SetActive", userId, active);
}

export function resetPasswordFor(userId: string, password: string): Promise<boolean> {
  return call<boolean>("Identity", "ResetPasswordFor", userId, password);
}

/** Changes the CALLER'S OWN password. No user id: the backend reads it from the session. */
export function changeMyPassword(current: string, next: string): Promise<boolean> {
  return call<boolean>("Identity", "ChangeMyPassword", current, next);
}

export function roles(): Promise<Role[]> {
  return call<Role[]>("Identity", "Roles");
}

export function userRoles(userId: string): Promise<Role[]> {
  return call<Role[]>("Identity", "UserRoles", userId);
}

export function permissionCatalogue(): Promise<PermissionEntry[]> {
  return call<PermissionEntry[]>("Identity", "Permissions");
}

export function roleGrants(roleId: string): Promise<string[]> {
  return call<string[]>("Identity", "RoleGrants", roleId);
}

export function grantToRole(roleId: string, permission: string): Promise<boolean> {
  return call<boolean>("Identity", "GrantToRole", roleId, permission);
}

export function revokeFromRole(roleId: string, permission: string): Promise<boolean> {
  return call<boolean>("Identity", "RevokeFromRole", roleId, permission);
}

export function assignRole(userId: string, roleId: string): Promise<boolean> {
  return call<boolean>("Identity", "AssignRole", userId, roleId);
}

export function unassignRole(userId: string, roleId: string): Promise<boolean> {
  return call<boolean>("Identity", "UnassignRole", userId, roleId);
}

export function adminSessions(): Promise<AdminSession[]> {
  return call<AdminSession[]>("Identity", "Sessions");
}

export function revokeSession(sessionId: string): Promise<boolean> {
  return call<boolean>("Identity", "RevokeSession", sessionId);
}

/** The permission codes the administration screens gate on. */
export const PERMISSIONS = {
  userView: "identity.user.view",
  userManage: "identity.user.manage",
  roleView: "identity.role.view",
  roleManage: "identity.role.manage",
  sessionView: "identity.session.view",
  sessionRevoke: "identity.session.revoke",
  auditView: "audit.entry.view",
  auditPayload: "audit.entry.view_payload",
} as const;

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
