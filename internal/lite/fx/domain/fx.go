// Package domain holds Mizan Lite's exchange rates as values: what a rate is, how one is typed, what a fetched quote is
// allowed to do, and when a rate is out of date (L3). No I/O, no clock of its own.
package domain

import (
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// Stable error codes. They double as i18n keys.
const (
	CodeRateDecimals     = "lite.fx.rate_decimals"
	CodeRateRequired     = "lite.fx.rate_required"
	CodeRateTooLarge     = "lite.fx.rate_too_large"
	CodeLargeChange      = "lite.fx.large_change"
	CodeNoteTooLong      = "lite.fx.note_too_long"
	CodeAlreadyHasRate   = "lite.fx.already_has_rate"
	CodeNotAProposal     = "lite.fx.not_a_proposal"
	CodeProposalStale    = "lite.fx.proposal_stale"
	CodeFetchNotFound    = "lite.fx.fetch_not_found"
	CodeFetchUnavailable = "lite.fx.fetch_unavailable"
	CodeFetchOffline     = "lite.fx.fetch_offline"
	CodeFetchFailed      = "lite.fx.fetch_failed"
	CodeInvalidMode      = "lite.fx.invalid_mode"
)

// Fields named in validation errors.
const (
	FieldRate = "rate"
	FieldNote = "note"
)

const (
	// MaxDecimals is how many decimals a rate may have, typed or fetched (L3 §4.1). It is also what refuses a rate typed
	// in the wrong direction: 0.0000667 dollars per pound has seven.
	MaxDecimals = 4
	// LargeChangePercent is the change beyond which a rate needs a second look (Q-L3.2).
	LargeChangePercent = 20
	// MaxNoteRunes bounds a note, as the schema does.
	MaxNoteRunes = 200
	// USD is the currency every rate is quoted against.
	USD = "USD"
)

const nano = 1_000_000_000

// Mode is how the rate is kept (L3 §14.4).
type Mode string

// The modes.
const (
	ModeAutomatic Mode = "automatic"
	ModeManual    Mode = "manual"
)

// ParseMode accepts exactly a mode.
func ParseMode(raw string) (Mode, error) {
	switch Mode(raw) {
	case ModeAutomatic, ModeManual:
		return Mode(raw), nil
	}
	return "", errs.Validation(CodeInvalidMode, "unknown rate mode").WithParam("value", raw)
}

// Source is where a rate came from.
type Source string

// The sources.
const (
	SourceFirstRun Source = "first_run"
	SourceManual   Source = "manual"
	SourceFetched  Source = "fetched"
)

// Rate is one recorded exchange rate: local currency per one US dollar.
type Rate struct {
	ID            id.ID
	LocalCurrency string
	// Seq is the rate's place; the highest place is the rate in force (A-L3.1).
	Seq          int64
	Nano         int64
	Source       Source
	FetchID      id.ID // set exactly when Source is fetched
	BusinessDate string
	RecordedAt   time.Time
	Note         string
}

// Outcome is what a fetch decided (L3 §14.5).
type Outcome string

// The outcomes.
const (
	OutcomeApplied   Outcome = "applied"
	OutcomeUnchanged Outcome = "unchanged"
	OutcomeProposed  Outcome = "proposed"
	OutcomeHeldMode  Outcome = "held_mode"
	OutcomeHeldToday Outcome = "held_today"
	OutcomeFailed    Outcome = "failed"
)

// Fetch is one attempt to fetch a rate.
type Fetch struct {
	ID            id.ID
	LocalCurrency string
	Seq           int64
	AttemptedAt   time.Time
	BusinessDate  string
	Outcome       Outcome
	Provider      string // empty when the attempt failed
	Nano          int64  // zero when the attempt failed
	AgainstRateID id.ID  // the rate in force when it was decided; zero when there was none
	ErrorCode     string // set exactly when the attempt failed
	// AdjustPercentMicro and EffectiveNano are DERIVED, not stored: the shop's margin on the internet's rate, and what
	// that margin makes of Nano (2026-09-17). The row keeps the published figure so the log always says what the provider
	// actually answered; these two are filled in from the setting in force when the fetch is read.
	AdjustPercentMicro int64
	EffectiveNano      int64
}

// Adjust puts a shop's margin on a published rate: nano × (1 + percent/100), rounded half away from zero, never below one.
//
// The arithmetic is in big.Int because a rate in nano on an old-pound figure is already in the tens of trillions, and
// multiplying by a hundred million before dividing overflows int64 and comes back a plausible wrong number.
func Adjust(nano, percentMicro int64) int64 {
	if percentMicro == 0 || nano <= 0 {
		return nano
	}
	base := big.NewInt(nano)
	delta := new(big.Int).Mul(base, big.NewInt(percentMicro))
	divisor := big.NewInt(100_000_000)
	quotient, remainder := new(big.Int).QuoRem(delta, divisor, new(big.Int))
	if new(big.Int).Abs(new(big.Int).Lsh(remainder, 1)).Cmp(divisor) >= 0 {
		if delta.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	out := new(big.Int).Add(base, quotient)
	if !out.IsInt64() || out.Sign() <= 0 {
		return 1 // a rate of nothing cannot price anything; the guards above keep this unreachable in practice
	}
	return out.Int64()
}

// Quote is what a provider answered: already scaled to the local currency and rounded to MaxDecimals.
type Quote struct {
	Provider string
	Nano     int64
}

// ParseRate reads a rate as typed: positive, at most MaxDecimals decimals, Arabic-Indic digits accepted.
func ParseRate(raw string) (int64, error) {
	normalised, err := numinput.Normalise(raw)
	if err != nil {
		if typed, ok := errs.AsError(err); ok {
			return 0, typed.WithField(FieldRate, typed.Code, "invalid")
		}
		return 0, err
	}
	if numinput.Decimals(normalised) > MaxDecimals {
		return 0, errs.Validation(CodeRateDecimals, "a rate takes at most four decimals").
			WithField(FieldRate, CodeRateDecimals, "too many decimals").WithParam("decimals", strconv.Itoa(MaxDecimals))
	}
	value, ok := new(big.Rat).SetString(normalised)
	if !ok {
		return 0, errs.Validation(numinput.CodeInvalid, "not a number").WithField(FieldRate, numinput.CodeInvalid, "invalid")
	}
	scaled := new(big.Rat).Mul(value, big.NewRat(nano, 1))
	if !scaled.IsInt() || !scaled.Num().IsInt64() {
		return 0, errs.Validation(CodeRateTooLarge, "rate out of range").WithField(FieldRate, CodeRateTooLarge, "too large")
	}
	v := scaled.Num().Int64()
	if v <= 0 {
		return 0, errs.Validation(CodeRateRequired, "a rate must be above zero").WithField(FieldRate, CodeRateRequired, "required")
	}
	return v, nil
}

// QuoteFromDecimal turns a provider's decimal text into a rate: exact, multiplied by scale, rounded half-up to
// MaxDecimals once (L3 §14.6). No float holds it at any point.
func QuoteFromDecimal(text string, scale int64) (int64, bool) {
	value, ok := new(big.Rat).SetString(strings.TrimSpace(text))
	if !ok || value.Sign() <= 0 || scale <= 0 {
		return 0, false
	}
	const perDecimalUnit = 10_000 // 10^MaxDecimals
	step := int64(nano / perDecimalUnit)
	// Units of 10⁻⁴: value × scale × 10⁴, rounded half up once, then back to 10⁻⁹.
	units := new(big.Rat).Mul(value, new(big.Rat).SetInt64(scale*perDecimalUnit))
	num, den := units.Num(), units.Denom()
	twice := new(big.Int).Add(new(big.Int).Mul(num, big.NewInt(2)), den)
	rounded := new(big.Int).Quo(twice, new(big.Int).Mul(den, big.NewInt(2)))
	result := new(big.Int).Mul(rounded, big.NewInt(step))
	if !result.IsInt64() || result.Sign() <= 0 {
		return 0, false
	}
	return result.Int64(), true
}

// ChangeExceedsLimit reports whether moving from one rate to another changes it by more than LargeChangePercent.
func ChangeExceedsLimit(from, to int64) bool {
	diff := new(big.Int).Abs(new(big.Int).Sub(big.NewInt(to), big.NewInt(from)))
	return new(big.Int).Mul(diff, big.NewInt(100)).Cmp(new(big.Int).Mul(big.NewInt(from), big.NewInt(LargeChangePercent))) > 0
}

// ChangePercent is the signed change from one rate to another in percent, to one decimal, rounded half away from zero
// once: "-13.3", "1.3", "0.0".
func ChangePercent(from, to int64) string {
	if from <= 0 {
		return ""
	}
	diff := new(big.Int).Sub(big.NewInt(to), big.NewInt(from))
	neg := diff.Sign() < 0
	diff.Abs(diff)
	// tenths of a percent: diff × 1000 ÷ from, half up on the magnitude.
	num := new(big.Int).Mul(diff, big.NewInt(1000))
	den := big.NewInt(from)
	tenths := new(big.Int).Quo(new(big.Int).Add(new(big.Int).Mul(num, big.NewInt(2)), den), new(big.Int).Mul(den, big.NewInt(2)))
	text := tenths.String()
	if len(text) < 2 {
		text = "0" + text
	}
	out := text[:len(text)-1] + "." + text[len(text)-1:]
	if neg && tenths.Sign() > 0 {
		out = "-" + out
	}
	return out
}

// Decide is what a fetched quote does (L3 §14.5), in that order. inForce is nil when no rate has been recorded.
func Decide(mode Mode, inForce *Rate, today string, q Quote) Outcome {
	switch {
	case mode == ModeManual:
		return OutcomeHeldMode
	case inForce == nil:
		return OutcomeProposed // the first rate is always a person's (D-L3.17)
	case inForce.Source == SourceManual && inForce.BusinessDate == today:
		return OutcomeHeldToday // the owner's rate holds for the rest of its day (D-L3.18)
	case ChangeExceedsLimit(inForce.Nano, q.Nano):
		return OutcomeProposed
	case q.Nano == inForce.Nano && inForce.BusinessDate == today:
		return OutcomeUnchanged
	default:
		return OutcomeApplied
	}
}

// Stale reports whether the rate was recorded before today's business date (Q-L3.1).
func (r Rate) Stale(today string) bool { return r.BusinessDate != today }

// Age is how long ago the rate was recorded — never negative, if the clock went back.
func (r Rate) Age(now time.Time) time.Duration {
	if d := now.Sub(r.RecordedAt); d > 0 {
		return d
	}
	return 0
}

// CleanNote trims a note and bounds it.
func CleanNote(note string) (string, error) {
	trimmed := strings.TrimSpace(note)
	if utf8.RuneCountInString(trimmed) > MaxNoteRunes {
		return "", errs.Validation(CodeNoteTooLong, "note too long").
			WithField(FieldNote, CodeNoteTooLong, "too long").WithParam("max", strconv.Itoa(MaxNoteRunes))
	}
	return trimmed, nil
}

// LargeChange is the refusal a large change gets until it is confirmed, carrying both figures for the dialog.
func LargeChange(from, to int64) error {
	return errs.Conflict(CodeLargeChange, "the rate changes by more than 20%").
		WithField(FieldRate, CodeLargeChange, "large change").
		WithParam("inForce", FormatRate(from)).WithParam("typed", FormatRate(to)).WithParam("percent", ChangePercent(from, to))
}

// FormatRate formats a rate at 10⁻⁹ with every significant decimal and none more: "15000", "13007.5355".
func FormatRate(n int64) string { return formatFixed(n, 9, 0) }

// FormatMinor formats a signed amount in minor units of a currency with `decimals` decimals: "97500", "1.20".
func FormatMinor(minor int64, decimals int) string { return formatFixed(minor, decimals, decimals) }

func formatFixed(v int64, scale, decimals int) string {
	return numinput.FormatFixed(v, scale, decimals)
}
