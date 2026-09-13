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
}

// Defaults is what a fresh installation uses.
func Defaults() Settings {
	return Settings{Locale: Arabic}
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
	return next, changes, nil
}
