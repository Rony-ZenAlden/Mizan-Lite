import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";

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
