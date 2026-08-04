// The ONLY module that touches the Wails globals.
//
// Step 0.11 §1.2: the previous wrapper let a binding's return type be declared inline at the
// call site, and when Step 0.10 wrapped Health() in an envelope the declaration silently went
// stale. An ESLint rule (see eslint.config.js) forbids `window.go` and `window.runtime`
// anywhere but this directory, so there is exactly one place that can drift, and it is tested.

type Invoker = (...args: unknown[]) => Promise<unknown>;

type BoundStruct = Record<string, Invoker | undefined>;

interface WailsBridge {
  main?: Record<string, BoundStruct | undefined>;
}

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
 * Wails binds each struct under window.go.main.<StructName>.<MethodName>.
 */
export function bridgeMethod(struct: string, method: string): Invoker | undefined {
  return window.go?.main?.[struct]?.[method];
}

/** Whether the Wails bridge is present — false under `npm run dev` in a plain browser. */
export function hasBridge(): boolean {
  return typeof window !== "undefined" && window.go?.main !== undefined;
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
