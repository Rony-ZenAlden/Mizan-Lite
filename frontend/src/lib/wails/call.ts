import { bridgeMethod, hasBridge } from "./bridge";
import { mockInvoke } from "./mock";
import type { APIError, FieldError, Result } from "./types";

/**
 * Codes this layer itself can produce.
 *
 * The backend's codes come from Go and are covered by Step 0.8's coverage gate. These four
 * originate in the frontend, so they get their own gate — see call.test.ts, which asserts each
 * has a catalog entry in every locale. Without that, a transport failure would render as raw
 * `app.bridge_unavailable` text, which is exactly what the Go gate exists to prevent.
 */
export const CODE_BRIDGE_UNAVAILABLE = "app.bridge_unavailable";
export const CODE_CALL_FAILED = "app.call_failed";
export const CODE_MALFORMED_RESPONSE = "app.malformed_response";
export const CODE_UNKNOWN = "app.unknown_error";

/**
 * A failure returned by a Go binding, or by the transport beneath it.
 *
 * It deliberately carries no display text. `message` exists only because Error requires one
 * and is for developers reading a stack trace; the UI renders
 * `translate(locale, err.messageKey, err.params)`. Step 0.8 guarantees every code has an entry
 * in every locale, so that always produces real text.
 */
export class BindingError extends Error {
  readonly code: string;
  readonly messageKey: string;
  readonly params: Record<string, string>;
  readonly fields: FieldError[];

  constructor(error: APIError) {
    super(`binding failed: ${error.code}`);
    this.name = "BindingError";
    this.code = error.code;
    this.messageKey = error.messageKey || error.code;
    this.params = error.params ?? {};
    this.fields = error.fields ?? [];
  }
}

/** Narrows an unknown thrown value to a BindingError. */
export function isBindingError(err: unknown): err is BindingError {
  return err instanceof BindingError;
}

function bindingError(code: string): BindingError {
  return new BindingError({ code, messageKey: code });
}

/**
 * The browser-dev mock, and ONLY in a development build.
 *
 * # Why a production build must fail rather than fall back
 *
 * Until 10.17 a missing bridge fell through to the mock unconditionally. The bridge WAS missing
 * in the packaged application — it was looked up under the wrong namespace — so the shipped app
 * ran entirely on fixtures: a signed-in "Developer", a catalogue of sample products, a dashboard
 * of invented figures. Nothing looked broken, because the mock is good at its job.
 *
 * That is the worst failure a fallback can have. It did not degrade the application; it REPLACED
 * it with a convincing imitation, and the only thing that gave it away was an action the mock had
 * no fixture for.
 *
 * So the fallback is now confined to where it belongs. `import.meta.env.DEV` is true under
 * `npm run dev` and false in every packaged build, so a production binary that cannot find its
 * backend now says so — which is a bug report, where fake data is a silent corruption of every
 * number on screen.
 */
function devMock(struct: string, method: string) {
  if (!import.meta.env.DEV) return undefined;
  return mockInvoke(struct, method);
}

/**
 * Type guard for the envelope, so a malformed response becomes a typed error rather than an
 * undefined field three components away.
 */
function isResult(value: unknown): value is Result<unknown> {
  return typeof value === "object" && value !== null && "ok" in value;
}

/**
 * Invokes a bound Go method and unwraps its envelope.
 *
 * Returns `data` on success and **throws BindingError** on failure. Throwing rather than
 * returning a union is deliberate: a caller that forgets to check a returned union renders
 * `undefined`, which is precisely the defect §1.2 recorded. A thrown error reaches the nearest
 * error boundary and cannot be ignored silently.
 *
 * This is the single unwrap point in the application. Nothing else may read window.go.
 */
export async function call<T>(
  struct: string,
  method: string,
  ...args: unknown[]
): Promise<T> {
  const invoke = hasBridge() ? bridgeMethod(struct, method) : devMock(struct, method);
  if (!invoke) {
    throw bindingError(CODE_BRIDGE_UNAVAILABLE);
  }

  let raw: unknown;
  try {
    raw = await invoke(...args);
  } catch {
    // An IPC-level failure (the webview tore down mid-call, the method is not bound).
    // The underlying text is a developer string, so it is deliberately not surfaced.
    throw bindingError(CODE_CALL_FAILED);
  }

  if (!isResult(raw)) {
    throw bindingError(CODE_MALFORMED_RESPONSE);
  }
  if (!raw.ok) {
    throw new BindingError(raw.error ?? { code: CODE_UNKNOWN, messageKey: CODE_UNKNOWN });
  }
  return raw.data as T;
}
