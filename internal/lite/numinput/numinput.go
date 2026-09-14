// Package numinput turns a number a person typed into the plain decimal string the kernel parses.
//
// # Why this is its own package
//
// DESIGN §8.3 named this the most likely bug in the product. The keyboard decides which digits arrive:
// some Arabic layouts type ١٢٣ on the number row, a Persian layout types ۱۲۳, and the Arabic decimal
// separator is ٫. A USB barcode scanner emulates a keyboard, so with an Arabic layout active it can
// deliver Arabic-Indic digits for a Latin barcode. An input that parses only 0–9 rejects the price; a
// lenient one reads 1 from ١٫٧٥٠ and drops the rest.
//
// # One contract, two languages
//
// The frontend has its own normaliseNumber, so an input can say "not a number" as it is typed. Go is
// authoritative. Both implementations are tested against ONE fixture, testdata/cases.json, so a case
// added for one is enforced on the other (L1 §6, D-L1.4).
package numinput

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable error codes. They double as i18n keys.
const (
	// CodeInvalid is anything that is not a plain non-negative decimal.
	CodeInvalid = "lite.number.invalid"
	// CodeGrouping is a thousands separator. Refused rather than guessed, because guessing turns
	// 1,750 kg into 1.75 kg — and the person who typed it deserves to be told why.
	CodeGrouping = "lite.number.grouping"
)

// maxLength bounds the input. Nothing a shop types is longer; anything longer is a paste gone wrong.
const maxLength = 32

// Normalise returns raw as a plain decimal string of Latin digits with at most one '.', or a typed error.
//
// Accepted: Latin, Arabic-Indic and Extended Arabic-Indic digits; '.' or '٫' as the decimal point;
// surrounding whitespace. A leading point gains a zero (".5" → "0.5"); a trailing point is dropped
// ("5." → "5"). Refused: empty input, grouping separators, signs, exponents, inner spaces, and more than
// one decimal point.
func Normalise(raw string) (string, error) {
	s := strings.TrimFunc(raw, unicode.IsSpace)
	if s == "" || utf8.RuneCountInString(s) > maxLength {
		return "", invalid(raw)
	}

	var b strings.Builder
	b.Grow(len(s))
	points := 0
	digits := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			digits++
		case r >= '٠' && r <= '٩':
			b.WriteRune('0' + (r - '٠'))
			digits++
		case r >= '۰' && r <= '۹':
			b.WriteRune('0' + (r - '۰'))
			digits++
		case r == '.' || r == '٫':
			points++
			if points > 1 {
				return "", invalid(raw)
			}
			b.WriteByte('.')
		case r == ',' || r == '٬' || r == '،':
			return "", errs.Validation(CodeGrouping, "thousands separators are not accepted").
				WithParam("value", raw)
		default:
			return "", invalid(raw)
		}
	}
	if digits == 0 {
		return "", invalid(raw)
	}

	out := b.String()
	if strings.HasPrefix(out, ".") {
		out = "0" + out
	}
	out = strings.TrimSuffix(out, ".")
	return out, nil
}

// Decimals reports how many digits follow the point in a string Normalise returned.
func Decimals(normalised string) int {
	if i := strings.IndexByte(normalised, '.'); i >= 0 {
		return len(normalised) - i - 1
	}
	return 0
}

// LatinDigits converts Arabic-Indic and Extended Arabic-Indic digits to Latin, leaving every other
// character as it is, and trims surrounding whitespace. For barcodes and PINs, which are not decimals.
func LatinDigits(raw string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '٠' && r <= '٩':
			return '0' + (r - '٠')
		case r >= '۰' && r <= '۹':
			return '0' + (r - '۰')
		}
		return r
	}, strings.TrimFunc(raw, unicode.IsSpace))
}

func invalid(raw string) error {
	return errs.Validation(CodeInvalid, "not a number").WithParam("value", raw)
}

// FormatFixed is the other direction: a signed integer scaled by 10^scale, written with at least minDecimals decimals and
// every significant digit beyond them — never rounded for display. "12.500" kg (scale 6, 3), "97500" pounds (0, 0),
// "5.9375" dollars (6, 2), "13007.5355" (9, 0). Latin digits and a '.' always: display localisation is the screen's.
func FormatFixed(v int64, scale, minDecimals int) string {
	sign := ""
	magnitude := uint64(v) //nolint:gosec // reinterpreted below for negative values
	if v < 0 {
		sign = "-"
		magnitude = -magnitude // two's complement: the magnitude of every int64, MinInt64 included
	}
	text := strconv.FormatUint(magnitude, 10)
	if len(text) <= scale {
		text = strings.Repeat("0", scale-len(text)+1) + text
	}
	whole, frac := text[:len(text)-scale], strings.TrimRight(text[len(text)-scale:], "0")
	if len(frac) < minDecimals {
		frac += strings.Repeat("0", minDecimals-len(frac))
	}
	if frac == "" {
		return sign + whole
	}
	return sign + whole + "." + frac
}
