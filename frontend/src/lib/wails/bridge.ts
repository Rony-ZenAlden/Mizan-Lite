// The ONLY module that touches the Wails globals.
//
// Step 0.11 §1.2: the previous wrapper let a binding's return type be declared inline at the
// call site, and when Step 0.10 wrapped Health() in an envelope the declaration silently went
// stale. An ESLint rule (see eslint.config.js) forbids `window.go` and `window.runtime`
// anywhere but this directory, so there is exactly one place that can drift, and it is tested.

type Invoker = (...args: unknown[]) => Promise<unknown>;

type BoundStruct = Record<string, Invoker | undefined>;

/**
 * Wails namespaces bound structs by their GO PACKAGE NAME.
 *
 * The bindings live in `internal/api/bindings` (package `bindings`), so the runtime publishes
 * them at `window.go.bindings.<Struct>.<Method>` — exactly what the generated files under
 * `frontend/wailsjs/go/bindings/` call. `main` was a guess made when the only bound struct was a
 * placeholder in main.go, and it was never re-checked after the bindings moved.
 *
 * `main` is still read as a fallback rather than deleted: a future refactor that moves a struct
 * back into package main should degrade to working, not to silence.
 */
interface WailsBridge {
  bindings?: Record<string, BoundStruct | undefined>;
  main?: Record<string, BoundStruct | undefined>;
}

/**
 * Every namespace a bound struct might appear under, in priority order.
 *
 * Exported so the namespace test can assert this list against the files Wails actually
 * generates — the one source of truth neither side can quietly drift from.
 */
export const BRIDGE_NAMESPACES = ["bindings", "main"] as const;

/** The subset of the Wails runtime the shell uses: the boot event stream. */
interface WailsRuntime {
  EventsOn?: (event: string, callback: (...data: unknown[]) => void) => () => void;
  EventsOff?: (event: string) => void;
}

declare global {
  interface Window {
    go?: WailsBridge;
    runtime?: WailsRuntime;
  }
}

/**
 * Looks up a bound Go method, or undefined when running outside the desktop webview.
 *
 * # The defect this shape exists to prevent
 *
 * Until 10.17 this read `window.go.main` alone. Wails publishes under the bound struct's Go
 * PACKAGE, which is `bindings` — so in the packaged desktop application `hasBridge()` was false,
 * every call fell through to the browser-dev mock, and the shipped app never once spoke to its
 * own backend. It looked like it worked, because the mock answered.
 *
 * Reading a LIST rather than one name is what stops the next move from doing it again silently.
 */
export function bridgeMethod(struct: string, method: string): Invoker | undefined {
  const bridge = window.go;
  if (!bridge) return undefined;
  for (const namespace of BRIDGE_NAMESPACES) {
    const bound = bridge[namespace]?.[struct]?.[method];
    if (bound) return bound;
  }
  return undefined;
}

/** Whether the Wails bridge is present — false under `npm run dev` in a plain browser. */
export function hasBridge(): boolean {
  if (typeof window === "undefined") return false;
  const bridge = window.go;
  if (!bridge) return false;
  return BRIDGE_NAMESPACES.some((namespace) => bridge[namespace] !== undefined);
}

/**
 * Subscribes to a Wails runtime event, returning an unsubscribe function.
 *
 * Outside the desktop webview there is no runtime, so this is a no-op returning a no-op. The
 * caller does not branch: a browser-dev session simply never receives boot events, and the
 * boot screen's mock path (mock.ts) drives the same state machine instead.
 */
export function onEvent(
  event: string,
  callback: (...data: unknown[]) => void,
): () => void {
  const on = window.runtime?.EventsOn;
  if (!on) return () => undefined;
  return on(event, callback);
}
