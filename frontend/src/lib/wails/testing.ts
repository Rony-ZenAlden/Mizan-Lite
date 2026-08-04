// Test helpers for faking the Wails bridge.
//
// They live here, inside the one directory allowed to touch window.go, so that tests
// elsewhere never need to — which keeps the ESLint boundary tight rather than riddled with
// per-test exemptions. Not imported by production code.

type Invoker = (...args: unknown[]) => Promise<unknown>;

/** Installs a fake bridge exposing the given struct/method implementations. */
export function installBridge(structs: Record<string, Record<string, Invoker>>): void {
  window.go = { main: structs };
}

/** Installs a single method, replacing any existing bridge. */
export function installMethod(struct: string, method: string, impl: Invoker): void {
  installBridge({ [struct]: { [method]: impl } });
}

/** Removes the fake bridge and runtime, restoring the browser-dev fallback. */
export function clearBridge(): void {
  delete window.go;
  delete window.runtime;
}

/** Installs a fake Wails runtime whose emit() drives every registered listener. */
export function installRuntime(): { emit: (event: string) => void } {
  const listeners = new Map<string, Set<(...data: unknown[]) => void>>();

  window.runtime = {
    EventsOn: (event, callback) => {
      const set = listeners.get(event) ?? new Set();
      set.add(callback);
      listeners.set(event, set);
      return () => set.delete(callback);
    },
    EventsOff: (event) => listeners.delete(event),
  };

  return {
    emit: (event) => {
      for (const callback of listeners.get(event) ?? []) callback();
    },
  };
}
