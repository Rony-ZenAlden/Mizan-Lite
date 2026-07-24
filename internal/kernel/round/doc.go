// Package round is the foundational rounding primitive shared by the money and
// quantity kernels.
//
// # Why it exists
//
// Both monetary settlement and unit-of-measure conversion must round an exact
// rational result to an integer, and both must do so with an explicitly named
// policy — never implicitly. Placing RoundingMode and the rounding engine in one
// tiny package lets money and quantity share identical, well-tested rounding
// behaviour without importing each other (which would create a cycle).
//
// # What it prevents
//
//   - Inconsistent rounding between prices, taxes, and conversions.
//   - Accidental double rounding (this engine rounds exactly once, on demand).
//   - Floating-point rounding error (it operates on math/big integers only).
//
// The engine never allocates a decision to chance: every tie is resolved by the
// selected RoundingMode's documented rule (see mode.go).
package round
