package round

// RoundingMode names how an exact value is rounded to an integer number of the
// smallest representable units. A "tie" is a value exactly halfway between two
// integers (…, its fractional part is exactly 0.5).
//
// The rules, with examples on the boundary between 2 and 3 (and their negatives):
//
//	Mode              tie rule            2.5    -2.5   2.4    typical use
//	----------------  ------------------  -----  -----  -----  --------------------------
//	HalfAwayFromZero  away from zero        3     -3      2    commercial default
//	HalfToEven        to the nearest even   2     -2      2    unbiased/statistical
//	HalfUp            toward +infinity      3     -2      2    some tax jurisdictions
//	HalfDown          toward -infinity      2     -3      2    rare, jurisdiction-specific
//	Ceiling           always toward +inf    3     -2      3    round-up pricing
//	Floor             always toward -inf    2     -3      2    conservative valuation
//	TowardZero        drop the fraction     2     -2      2    explicit truncation
//
// HalfAwayFromZero is the seed commercial default; the effective mode is always
// configurable per currency and per business context above this package.
type RoundingMode uint8

const (
	// HalfAwayFromZero rounds ties away from zero (2.5→3, -2.5→-3). Default.
	HalfAwayFromZero RoundingMode = iota
	// HalfToEven rounds ties to the nearest even integer (2.5→2, 3.5→4). Banker's rounding.
	HalfToEven
	// HalfUp rounds ties toward positive infinity (2.5→3, -2.5→-2).
	HalfUp
	// HalfDown rounds ties toward negative infinity (2.5→2, -2.5→-3).
	HalfDown
	// Ceiling always rounds toward positive infinity (2.1→3, -2.9→-2).
	Ceiling
	// Floor always rounds toward negative infinity (2.9→2, -2.1→-3).
	Floor
	// TowardZero drops the fraction entirely (2.9→2, -2.9→-2).
	TowardZero

	maxMode = TowardZero
)

// Valid reports whether m is a defined rounding mode.
func (m RoundingMode) Valid() bool { return m <= maxMode }

// ParseMode is the inverse of String.
//
// String is documented as the storage form, so a parser is the other half of that
// contract: a rounding mode persisted in a settings row or a currency record has to come
// back as the same mode, on any engine, years later. Unknown names are rejected rather
// than silently defaulting — a typo in a seed file that quietly changed how money rounds
// would be almost impossible to trace from the symptom.
func ParseMode(s string) (RoundingMode, bool) {
	for m := RoundingMode(0); m <= maxMode; m++ {
		if m.String() == s {
			return m, true
		}
	}
	return 0, false
}

// String returns a stable, snake_case name suitable for storage and settings.
func (m RoundingMode) String() string {
	switch m {
	case HalfAwayFromZero:
		return "half_away_from_zero"
	case HalfToEven:
		return "half_to_even"
	case HalfUp:
		return "half_up"
	case HalfDown:
		return "half_down"
	case Ceiling:
		return "ceiling"
	case Floor:
		return "floor"
	case TowardZero:
		return "toward_zero"
	default:
		return "unknown"
	}
}
