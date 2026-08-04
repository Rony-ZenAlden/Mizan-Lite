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
  const invoke = hasBridge() ? bridgeMethod(struct, method) : mockInvoke(struct, method);
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
