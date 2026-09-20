import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach, expect, vi } from "vitest";

// Any console.error or console.warn FAILS the test that produced it.
//
// React reports real defects through the console — an update after unmount, a missing key, a state
// change outside act() that means a test ended before the code under it finished. Left as noise, the
// one warning that matters scrolls past with the rest. A test that expects the console (an error
// boundary catching a render error) says so with expectConsoleErrors().
let consoleCalls: string[] = [];
let consoleExpected = false;

export function expectConsoleErrors(): void {
  consoleExpected = true;
}

beforeEach(() => {
  consoleCalls = [];
  consoleExpected = false;
  const record = (kind: string) => (...args: unknown[]) => {
    consoleCalls.push(`${kind}: ${args.map(String).join(" ").split("\n")[0]}`);
  };
  vi.spyOn(console, "error").mockImplementation(record("console.error"));
  vi.spyOn(console, "warn").mockImplementation(record("console.warn"));
});

afterEach(() => {
  cleanup();
  document.documentElement.removeAttribute("dir");
  document.documentElement.removeAttribute("lang");
  vi.restoreAllMocks();
  if (!consoleExpected && consoleCalls.length > 0) {
    throw new Error(`the test wrote to the console:\n  ${consoleCalls.join("\n  ")}`);
  }
});

// Arabic sentences carry invisible isolates around the values in them (i18n/messages.inSentence). A reader does not see them,
// so a test compares what a reader sees.
const readable = (text: string | null | undefined) => (text ?? "").replace(/[⁦-⁩]/g, "").replace(/\s+/g, " ").trim();

expect.extend({
  toHaveReadableText(element: Element | null, expected: string) {
    const actual = readable(element?.textContent);
    const pass = actual.includes(readable(expected));
    return { pass, message: () => `expected the text a reader sees${pass ? " not" : ""} to contain\n  ${expected}\nbut it was\n  ${actual}` };
  },
});

declare module "vitest" {
  interface Assertion<T> {
    toHaveReadableText(expected: string): T;
  }
}

// The till keeps its open cart in localStorage so a restart recovers it (2026-09-20). jsdom keeps that storage for the
// whole file, so without this one test's half-finished cart turns up in the next one's till.
beforeEach(() => {
  try {
    window.localStorage.clear();
  } catch {
    // A test environment without storage is fine: the code under test tolerates it too.
  }
});
