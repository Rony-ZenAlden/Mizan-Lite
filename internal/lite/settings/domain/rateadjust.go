package domain

import (
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
)

// FieldRateAdjust is the form field a bad margin is marked on.
const FieldRateAdjust = "rateAdjustPercent"

// ParseRateAdjustPercent reads the margin a shop puts on the internet's rate, as a signed number of percentage points at
// 10⁻⁶ ("5" → 5_000_000). An empty value is no margin at all, which is the default.
//
// It may be negative: a shop that prices below the published rate is doing something real, and refusing to express it would
// leave the setting unable to say what the shop is already doing.
func ParseRateAdjustPercent(raw string) (int64, error) {
	trimmed := strings.TrimSpace(numinput.LatinDigits(raw))
	if trimmed == "" {
		return 0, nil
	}
	negative := strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "−")
	unsigned := strings.TrimLeft(trimmed, "+-−")
	normalised, err := numinput.Normalise(unsigned)
	if err != nil {
		return 0, errs.Validation(CodeInvalidRateAdjust, "a margin is a number of percentage points").
			WithField(FieldRateAdjust, CodeInvalidRateAdjust, "not a number").WithParam("value", raw)
	}
	if numinput.Decimals(normalised) > 4 {
		return 0, errs.Validation(CodeInvalidRateAdjust, "a margin takes at most four decimals").
			WithField(FieldRateAdjust, CodeInvalidRateAdjust, "too many decimals").WithParam("value", raw)
	}
	whole, fraction, _ := strings.Cut(normalised, ".")
	fraction += strings.Repeat("0", 6-len(fraction))
	micro, convErr := strconv.ParseInt(whole+fraction, 10, 64)
	if convErr != nil {
		return 0, errs.Validation(CodeInvalidRateAdjust, "margin out of range").
			WithField(FieldRateAdjust, CodeInvalidRateAdjust, "out of range").WithParam("value", raw)
	}
	if negative {
		micro = -micro
	}
	if micro > MaxRateAdjustPercentMicro || micro < -MaxRateAdjustPercentMicro {
		return 0, errs.Validation(CodeInvalidRateAdjust, "a margin is at most 50 percentage points either way").
			WithField(FieldRateAdjust, CodeInvalidRateAdjust, "out of range").
			WithParam("max", strconv.FormatInt(MaxRateAdjustPercentMicro/1_000_000, 10))
	}
	return micro, nil
}

// FormatRateAdjustPercent writes the margin back as a row value, without trailing noughts.
func FormatRateAdjustPercent(micro int64) string {
	if micro == 0 {
		return "0"
	}
	sign := ""
	if micro < 0 {
		sign, micro = "-", -micro
	}
	whole, fraction := micro/1_000_000, micro%1_000_000
	if fraction == 0 {
		return sign + strconv.FormatInt(whole, 10)
	}
	text := strings.TrimRight(strconv.FormatInt(fraction+1_000_000, 10)[1:], "0")
	return sign + strconv.FormatInt(whole, 10) + "." + text
}
