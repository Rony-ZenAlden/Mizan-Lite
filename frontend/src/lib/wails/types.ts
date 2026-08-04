// The wire types, mirroring internal/api/envelope.
//
// The authoritative shape is the Go type; this file is its TypeScript reading. The pairing is
// pinned by __fixtures__/envelope.contract.json, which the Go test generates and the tests in
// this directory parse — so neither side can change the shape alone (Step 0.11 §3.3).

/** A per-field validation failure. */
export interface FieldError {
  field: string;
  code: string;
  messageKey: string;
  params?: Record<string, string>;
}

/**
 * A failure as it crosses the boundary.
 *
 * Note what is absent: any message text. The backend never returns prose (§5.4, §22.2) —
 * it returns a stable code plus parameters, and the UI renders them in the active locale.
 * That is what makes switching language complete rather than leaving stale errors behind.
 */
export interface APIError {
  code: string;
  messageKey: string;
  params?: Record<string, string>;
  fields?: FieldError[];
}

/**
 * The envelope every binding returns.
 *
 * `data` is always present on the wire — the Go field carries no omitempty, precisely so an
 * empty list arrives as [] rather than vanishing. See the comment on envelope.Result.
 */
export interface Result<T> {
  ok: boolean;
  data: T;
  error?: APIError;
}
