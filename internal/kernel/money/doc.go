// Package money provides exact monetary and numeric value objects.
//
// Money is stored as int64 minor units plus a Currency; it is NEVER a float.
// The five numeric types (Money, UnitAmount, Quantity, Rate, Percent) and the
// single-rounding LineExtension rule are implemented here in Phase 0 Step 0.2.
//
// See docs/architecture/PHASE_0_FOUNDATION.md §KRN.
package money
