// The browser-dev fallback, for `npm run dev` without Wails.
//
// Step 0.11 §1.2, lesson 1: the previous mock returned the PRE-envelope flat shape, so the
// browser looked correct while the packaged app was broken. A mock that does not match the
// contract is worse than no mock — it actively certifies the failing path.
//
// So every mock here returns a genuine Result<T>, and mock.test.ts asserts the Health mock
// against the fixture the Go side generates. If the envelope changes, this file fails a test.

import type { Result } from "./types";

type Invoker = (...args: unknown[]) => Promise<unknown>;

function ok<T>(data: T): Promise<Result<T>> {
  return Promise.resolve({ ok: true, data });
}

/**
 * Boot is instant in the browser: there is no database to migrate. The boot screen therefore
 * flashes through to ready, which is the honest simulation — not a fake delay that would give
 * a developer a false impression of how a real first launch feels.
 */
const MOCKS: Record<string, Record<string, Invoker>> = {
  Boot: {
    Status: () =>
      ok({
        state: "ready",
        phase: "done",
        current: 0,
        total: 0,
        name: "",
        error: null,
        backupPath: "",
      }),
  },
  Auth: {
    // Browser dev runs as a signed-in administrator: there is no Go process to hold a session,
    // and a mock that could not sign in would make every guarded screen unreachable.
    Login: () =>
      ok({
        signedIn: true, userId: "dev", username: "admin", displayName: "Developer",
        mustChange: false, permissions: ["*"],
      }),
    Logout: () => ok(true),
    Me: () =>
      ok({
        signedIn: true, userId: "dev", username: "admin", displayName: "Developer",
        mustChange: false, permissions: ["*"],
      }),
  },
  Setup: {
    // Browser dev runs against an already-configured installation: the wizard is reachable in
    // dev by flipping this to true, and leaving it false means every OTHER screen is reachable
    // without walking seven steps first.
    Status: () => ok({ required: false }),
    Apply: () =>
      ok({ companyId: "dev", branchId: "dev", warehouseId: "dev", adminUserId: "dev" }),
  },
  Identity: {
    Users: () =>
      ok([
        { id: "dev", username: "admin", displayName: "Developer", email: "", isActive: true, isSystem: true },
      ]),
    Roles: () =>
      ok([
        { id: "r1", code: "administrator", name: "Administrator", description: "", isSystem: true, isActive: true },
      ]),
    Permissions: () => ok([{ code: "identity.user.view", module: "identity", obsolete: false }]),
    RoleGrants: () => ok(["*"]),
    UserRoles: () =>
      ok([
        { id: "r1", code: "administrator", name: "Administrator", description: "", isSystem: true, isActive: true },
      ]),
    Sessions: () =>
      ok([
        {
          id: "s1", userId: "dev", username: "admin", displayName: "Developer",
          startedAt: "", lastSeen: "", expiresAt: "", deviceInfo: "browser", current: true,
        },
      ]),
    CreateUser: () =>
      ok({ id: "new", username: "new", displayName: "", email: "", isActive: true, isSystem: false }),
    SetActive: () => ok(true),
    ResetPasswordFor: () => ok(true),
    ChangeMyPassword: () => ok(true),
    GrantToRole: () => ok(true),
    RevokeFromRole: () => ok(true),
    AssignRole: () => ok(true),
    UnassignRole: () => ok(true),
    RevokeSession: () => ok(true),
  },
  System: {
    Health: () =>
      ok({
        version: "dev (browser)",
        commit: "unknown",
        buildTime: "unknown",
        goVersion: "unknown",
        platform: "browser",
      }),
  },
  Config: {
    Preferences: () => ok({ locale: "en", theme: "system", availableLocales: ["en", "ar"] }),
    SetLocale: (locale) =>
      ok({ locale: String(locale), theme: "system", availableLocales: ["en", "ar"] }),
    SetTheme: (theme) =>
      ok({ locale: "en", theme: String(theme), availableLocales: ["en", "ar"] }),
  },
  Ops: {
    Jobs: () =>
      ok([
        {
          key: "outbox.dispatch",
          enabled: true,
          schedule: "@every 5s",
          lastRunAt: "2026-08-04T09:00:00.000Z",
          nextRunAt: "2026-08-04T09:00:05.000Z",
          lastStatus: "succeeded",
        },
        {
          key: "platform.heartbeat",
          enabled: true,
          schedule: "@every 1m",
          lastRunAt: "2026-08-04T09:00:00.000Z",
          nextRunAt: "2026-08-04T09:01:00.000Z",
          lastStatus: "succeeded",
        },
      ]),
    Runs: () =>
      ok([
        {
          runId: "0192f2c0-0000-7000-8000-000000000001",
          jobKey: "platform.heartbeat",
          startedAt: "2026-08-04T09:00:00.000Z",
          finishedAt: "2026-08-04T09:00:00.010Z",
          status: "succeeded",
          attempt: 1,
          error: "",
          output: "",
          triggeredBy: "schedule",
        },
      ]),
  },
  Audit: {
    Entries: () =>
      ok([
        {
          id: "audit-1", occurredAt: "2026-08-06T09:00:00.000Z",
          actorUserId: "dev", actorName: "Developer", correlationId: "corr-1",
          action: "identity.user.password_changed", entityType: "user",
          entityId: "user-1", entityLabel: "alice", source: "ui",
          // Present here because the browser-dev mock runs as an administrator. A user without
          // audit.entry.view_payload receives these keys ABSENT, not blank.
          beforeJson: '{"example":"before"}',
          afterJson: '{"example":"after"}',
          changedFields: '["example"]',
        },
      ]),
  },
  Money: {
    Currencies: () =>
      ok([
        { code: "SYP", name: "Syrian Pound", symbol: "ل.س", decimalPlaces: 0, symbolPosition: "after" },
        { code: "USD", name: "US Dollar", symbol: "$", decimalPlaces: 2, symbolPosition: "before" },
      ]),
  },
};

/** Returns the mock for a bound method, or undefined if there is none. */
export function mockInvoke(struct: string, method: string): Invoker | undefined {
  return MOCKS[struct]?.[method];
}
