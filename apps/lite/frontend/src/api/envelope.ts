// The envelope every binding returns, and the ONE place it is unwrapped.
//
// The backend never sends human-readable text: a failure is a code and parameters, translated here
// in whichever language the reader has chosen (Mizan §22.2). This file turns every way a call can go
// wrong into one typed error carrying such a code.

/** A failure as the frontend receives it. Mirrors internal/api/envelope.APIError. */
export interface APIError {
  code: string;
  messageKey: string;
  params?: Record<string, string>;
}

/** The frontend's own codes — the only error codes not declared in Go. Each has a catalog entry. */
export const CODE_BRIDGE_UNAVAILABLE = "lite.client.bridge_unavailable";
export const CODE_CALL_FAILED = "lite.client.call_failed";
export const CODE_MALFORMED_RESPONSE = "lite.client.malformed_response";
export const CODE_UNKNOWN = "lite.client.unknown";

export const CLIENT_CODES = [
  CODE_BRIDGE_UNAVAILABLE,
  CODE_CALL_FAILED,
  CODE_MALFORMED_RESPONSE,
  CODE_UNKNOWN,
] as const;

/** Thrown for every failed call. Carries a code, never prose. */
export class BindingError extends Error {
  readonly apiError: APIError;

  constructor(apiError: APIError) {
    super(apiError.code);
    this.name = "BindingError";
    this.apiError = apiError;
  }

  get code(): string {
    return this.apiError.code;
  }
}

function clientError(code: string): BindingError {
  return new BindingError({ code, messageKey: code });
}

/**
 * The generated model classes carry a `convertValues` method, but the IPC bridge delivers plain JSON.
 * Plain<T> is the generated type with its methods removed — every field still comes from Go.
 */
export type Plain<T> = T extends readonly (infer U)[]
  ? Plain<U>[]
  : T extends object
    ? { [K in keyof T as T[K] extends (...args: never[]) => unknown ? never : K]: Plain<T[K]> }
    : T;

interface WireResult<T> {
  ok: boolean;
  data: T;
  error?: APIError;
}

function isWireResult(value: unknown): value is WireResult<unknown> {
  return typeof value === "object" && value !== null && typeof (value as { ok?: unknown }).ok === "boolean";
}

/**
 * Invokes a generated binding and unwraps its envelope, returning the data or THROWING.
 *
 * Throwing rather than returning a union is deliberate: a caller that forgets to check a union
 * renders `undefined`; a thrown error reaches the nearest handler and cannot be ignored silently.
 *
 * # How a missing bridge is told apart from a failed call
 *
 * Without the Wails runtime, a generated function throws SYNCHRONOUSLY — it dereferences
 * `window.go`, which does not exist. A call that reached the bridge and then failed REJECTS instead.
 * So the distinction needs no reference to `window.go` in this codebase at all (gate G1).
 */
export async function unwrap<T>(invoke: () => Promise<WireResult<T>>): Promise<Plain<T>> {
  let pending: Promise<WireResult<T>>;
  try {
    pending = invoke();
  } catch {
    throw clientError(CODE_BRIDGE_UNAVAILABLE);
  }

  let raw: unknown;
  try {
    raw = await pending;
  } catch {
    // An IPC-level failure. Its text is a developer string, so it is deliberately not surfaced.
    throw clientError(CODE_CALL_FAILED);
  }

  if (!isWireResult(raw)) {
    throw clientError(CODE_MALFORMED_RESPONSE);
  }
  if (!raw.ok) {
    throw new BindingError(raw.error ?? { code: CODE_UNKNOWN, messageKey: CODE_UNKNOWN });
  }
  return raw.data as Plain<T>;
}
