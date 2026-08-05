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
