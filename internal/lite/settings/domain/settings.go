// Package domain holds Mizan Lite's settings as values: what exists, what it defaults to, and what
// counts as valid. No I/O.
package domain

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// CodeInvalidLocale is returned when a locale is not one Lite ships.
const CodeInvalidLocale = "lite.settings.invalid_locale"

// KeyLocale is the stored key for the interface language. It is the only place the string exists:
// call sites use Settings.Locale, never the key.
const KeyLocale = "ui.locale"

// KeyShopName is the stored key for the shop's name (L1 §8): the header now, the receipt from L7.
const KeyShopName = "shop.name"

// KeyRateMode is the stored key for how the exchange rate is kept (L3 §14.4). Written only through the fx module,
// because changing it needs the owner.
const KeyRateMode = "fx.mode"

// KeyLocalCurrency is the stored key for the currency the exchange rate prices (L3 §3.2). Not editable in v1: nothing
// writes it, and a stored value must name a currency code.
const KeyLocalCurrency = "currency.local"

// CodeInvalidRateMode is returned when a rate mode is not one Lite has.
const CodeInvalidRateMode = "lite.settings.invalid_rate_mode"

// RateMode is how the exchange rate is kept.
type RateMode string

// The rate modes. Manual is the default, by the owner's decision at L3's approval: the internet's rate is official-style
// and below the market rate the shop trades at (L3 §14.3 F2), so a fresh installation prices from the owner's rate and
// shows the internet's for reference. Automatic is one PIN away.
const (
	RateAutomatic RateMode = "automatic"
	RateManual    RateMode = "manual"
)

// ParseRateMode accepts exactly a rate mode.
func ParseRateMode(raw string) (RateMode, error) {
	switch RateMode(strings.TrimSpace(raw)) {
	case RateAutomatic:
		return RateAutomatic, nil
	case RateManual:
		return RateManual, nil
	default:
		return "", errs.Validation(CodeInvalidRateMode, "unknown rate mode").WithParam("value", raw)
	}
}

// KeyCashNote is the stored key for the smallest local-currency note the shop hands over: totals in the local
// currency round to it (L4 §3.2, Q-L4.1). Written only through the sales module, because changing it needs the owner.
const KeyCashNote = "currency.local_cash_note"

// Cash note bounds and codes.
const (
	// DefaultCashNote is 500 pounds (Q-L4.1).
	DefaultCashNote     = int64(500)
	MaxCashNote         = int64(1_000_000)
	CodeInvalidCashNote = "lite.settings.invalid_cash_note"
)

// ParseCashNote reads a note in whole local minor units: a positive integer up to MaxCashNote.
func ParseCashNote(raw string) (int64, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || v < 1 || v > MaxCashNote {
		return 0, errs.Validation(CodeInvalidCashNote, "the cash note must be a whole amount from 1 to 1,000,000").
			WithField("cashNote", CodeInvalidCashNote, "invalid").WithParam("max", strconv.FormatInt(MaxCashNote, 10))
	}
	return v, nil
}

// DefaultLocalCurrency is the local currency of a fresh installation (Q-L3.4).
const DefaultLocalCurrency = "SYP"

// Shop name codes and bound.
const (
	CodeShopNameRequired = "lite.settings.shop_name_required"
	CodeShopNameTooLong  = "lite.settings.shop_name_too_long"
	MaxShopNameRunes     = 100
	FieldShopName        = "shopName"
)

// ParseShopName trims a shop name and refuses an empty or over-long one.
func ParseShopName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errs.Validation(CodeShopNameRequired, "the shop name is required").
			WithField(FieldShopName, CodeShopNameRequired, "required")
	}
	if utf8.RuneCountInString(name) > MaxShopNameRunes {
		return "", errs.Validation(CodeShopNameTooLong, "the shop name is too long").
			WithField(FieldShopName, CodeShopNameTooLong, "too long").WithParam("max", strconv.Itoa(MaxShopNameRunes))
	}
	return name, nil
}

// Locale is a language Lite ships a complete catalog for.
type Locale string

// The shipped locales. Arabic is the primary language of the product (design D3).
const (
	Arabic  Locale = "ar"
	English Locale = "en"
)

// Direction is the writing direction a locale lays out in.
type Direction string

// Writing directions.
const (
	RTL Direction = "rtl"
	LTR Direction = "ltr"
)

// ParseLocale accepts exactly a shipped locale code, case- and space-insensitively.
//
// It does not accept a region ("ar-SY") or fall back to a nearby language. A settings row is
// written only by this application, so anything else in it is damage, and damage is reported
// rather than guessed at.
func ParseLocale(raw string) (Locale, error) {
	switch Locale(strings.ToLower(strings.TrimSpace(raw))) {
	case Arabic:
		return Arabic, nil
	case English:
		return English, nil
	default:
		return "", errs.Validation(CodeInvalidLocale, "unsupported locale").WithParam("value", raw)
	}
}

// Direction reports how the locale lays out.
func (l Locale) Direction() Direction {
	if l == Arabic {
		return RTL
	}
	return LTR
}

// Settings is every setting Lite has, resolved.
//
// A field is added here only when something reads it. The design lists a dozen settings for later
// phases; declaring them now would be the "built and connected to nothing" defect in its smallest
// form.
type Settings struct {
	Locale Locale
	// ShopName is empty until first run sets it.
	ShopName string
	// RateMode is how the exchange rate is kept (L3).
	RateMode RateMode
	// LocalCurrency is the code the exchange rate prices (L3).
	LocalCurrency string
	// CashNote is the smallest local note, in local minor units, that local totals round to (L4).
	CashNote int64
}

// Defaults is what a fresh installation uses.
func Defaults() Settings {
	return Settings{Locale: Arabic, RateMode: RateManual, LocalCurrency: DefaultLocalCurrency, CashNote: DefaultCashNote}
}

// ProblemKind says what was wrong with a stored row.
type ProblemKind string

// Problem kinds.
const (
	// UnknownKey is a row this build does not declare — written by a newer build, or left over
	// from an older one. Ignored, so an older binary still opens a newer database.
	UnknownKey ProblemKind = "unknown_key"
	// InvalidValue is a declared key whose value does not parse. The default is used instead.
	InvalidValue ProblemKind = "invalid_value"
)

// Problem is a stored row that was not used, and why.
type Problem struct {
	Key   string
	Value string
	Kind  ProblemKind
}

// FromStored resolves settings from stored rows.
//
// It never fails. Every problem is RETURNED for the caller to log, and the affected setting keeps
// its default — a damaged preference must not be able to stop a shop from opening (Mizan 0.10 D3:
// data leftovers are reported and ignored; only code defects are fatal).
func FromStored(rows map[string]string) (Settings, []Problem) {
	out := Defaults()
	var problems []Problem
	for key, value := range rows {
		switch key {
		case KeyLocale:
			locale, err := ParseLocale(value)
			if err != nil {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.Locale = locale
		case KeyShopName:
			name, err := ParseShopName(value)
			if err != nil {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.ShopName = name
		case KeyRateMode:
			mode, err := ParseRateMode(value)
			if err != nil {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.RateMode = mode
		case KeyLocalCurrency:
			if !isCurrencyCode(value) {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.LocalCurrency = value
		case KeyCashNote:
			note, err := ParseCashNote(value)
			if err != nil {
				problems = append(problems, Problem{Key: key, Value: value, Kind: InvalidValue})
				continue
			}
			out.CashNote = note
		default:
			problems = append(problems, Problem{Key: key, Value: value, Kind: UnknownKey})
		}
	}
	return out, problems
}

// Change is one stored row to write.
type Change struct {
	Key   string
	Value string
}

// Update is a partial change to settings. A nil field is left as it is.
type Update struct {
	Locale   *string
	ShopName *string
	RateMode *string
	CashNote *string
}

// Apply validates an update against the current settings and returns the result and the rows that
// must be written. Nothing is returned to write for a field whose value does not change.
func (s Settings) Apply(u Update) (Settings, []Change, error) {
	next := s
	var changes []Change
	if u.Locale != nil {
		locale, err := ParseLocale(*u.Locale)
		if err != nil {
			return s, nil, err
		}
		if locale != s.Locale {
			next.Locale = locale
			changes = append(changes, Change{Key: KeyLocale, Value: string(locale)})
		}
	}
	if u.ShopName != nil {
		name, err := ParseShopName(*u.ShopName)
		if err != nil {
			return s, nil, err
		}
		if name != s.ShopName {
			next.ShopName = name
			changes = append(changes, Change{Key: KeyShopName, Value: name})
		}
	}
	if u.RateMode != nil {
		mode, err := ParseRateMode(*u.RateMode)
		if err != nil {
			return s, nil, err
		}
		if mode != s.RateMode {
			next.RateMode = mode
			changes = append(changes, Change{Key: KeyRateMode, Value: string(mode)})
		}
	}
	if u.CashNote != nil {
		note, err := ParseCashNote(*u.CashNote)
		if err != nil {
			return s, nil, err
		}
		if note != s.CashNote {
			next.CashNote = note
			changes = append(changes, Change{Key: KeyCashNote, Value: strconv.FormatInt(note, 10)})
		}
	}
	return next, changes, nil
}

// isCurrencyCode reports three upper-case ASCII letters — a code's shape; whether it exists is the database's to say.
func isCurrencyCode(v string) bool {
	if len(v) != 3 {
		return false
	}
	for _, r := range v {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
