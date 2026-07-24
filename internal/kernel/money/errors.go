package money

import "errors"

// Sentinel errors returned by this package. Callers branch on them with errors.Is.
// They are plain stdlib errors so that money remains a standalone, dependency-free
// package; higher layers map them to categorised domain errors (kernel/errs).
var (
	// ErrNoCurrency is returned when an operation is attempted on a zero-value
	// (no-currency) Money. Construct amounts via FromMinor, Zero, or Parse.
	ErrNoCurrency = errors.New("money: operation on a zero-value amount (no currency)")

	// ErrCurrencyMismatch is returned when two amounts of different currencies are
	// combined. There is no implicit conversion; use MulRate to convert explicitly.
	ErrCurrencyMismatch = errors.New("money: currency mismatch")

	// ErrOverflow is returned when a result would exceed the int64 minor-unit range.
	ErrOverflow = errors.New("money: int64 overflow")

	// ErrInvalidCurrency is returned when constructing an invalid Currency.
	ErrInvalidCurrency = errors.New("money: invalid currency")

	// ErrInvalidRate is returned when a rate is not strictly positive.
	ErrInvalidRate = errors.New("money: rate must be positive")

	// ErrParse is returned when a decimal string cannot be parsed for a currency.
	ErrParse = errors.New("money: invalid amount")

	// ErrInvalidWeights is returned when allocation weights are negative or do not
	// sum to a positive total.
	ErrInvalidWeights = errors.New("money: allocation weights must sum to a positive total")
)
