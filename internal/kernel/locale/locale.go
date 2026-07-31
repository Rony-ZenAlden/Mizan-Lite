package locale

import (
	"context"
	"strings"
)

// Locale is a BCP-47-style language tag, e.g. "en" or "ar".
//
// Deliberately a string type rather than an enum of two values: adding Kurdish must be
// dropping a directory into locales/, not editing a constant list and recompiling. `en` and
// `ar` are the seed set, not the definition (ARCHITECTURE_v1 §22, and the "configuration, not
// code" mandate).
type Locale string

// Direction is the writing direction a locale is laid out in.
type Direction string

const (
	LTR Direction = "ltr"
	RTL Direction = "rtl"
)

// Default is the locale used when none is set, and the last link in every fallback chain.
// English because it is the language the catalogs are authored in, so it is the one locale
// guaranteed to define every key.
const Default = Locale("en")

// rtlLanguages lists the primary language subtags written right-to-left.
//
// Keyed on the language subtag alone, so "ar-SY" and "ar-EG" both resolve correctly without
// enumerating regions.
var rtlLanguages = map[string]bool{
	"ar":  true, // Arabic
	"fa":  true, // Persian
	"he":  true, // Hebrew
	"ur":  true, // Urdu
	"ckb": true, // Central Kurdish (Sorani)
	"syr": true, // Syriac
}

// Parse validates and normalises a locale tag.
//
// It accepts any well-formed tag, not merely the ones shipped today: the catalog loader
// discovers locales from the filesystem, so a build with locales/ku/ must be able to parse
// "ku" without a code change.
func Parse(s string) (Locale, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", false
	}
	// Normalise separators and case: "AR_sy" and "ar-SY" are the same locale.
	normalised := strings.ReplaceAll(trimmed, "_", "-")
	parts := strings.Split(normalised, "-")
	for i, p := range parts {
		if p == "" || !isAlphaNumeric(p) {
			return "", false
		}
		if i == 0 {
			parts[i] = strings.ToLower(p)
			continue
		}
		parts[i] = strings.ToUpper(p)
	}
	lang := parts[0]
	if len(lang) < 2 || len(lang) > 3 || !isAlpha(lang) {
		return "", false
	}
	return Locale(strings.Join(parts, "-")), true
}

// Language returns the primary subtag: "ar" for "ar-SY".
func (l Locale) Language() string {
	if i := strings.Index(string(l), "-"); i > 0 {
		return string(l)[:i]
	}
	return string(l)
}

// Direction reports how the locale is laid out.
func (l Locale) Direction() Direction {
	if l.IsRTL() {
		return RTL
	}
	return LTR
}

// IsRTL reports whether the locale is written right-to-left.
func (l Locale) IsRTL() bool { return rtlLanguages[l.Language()] }

// FallbackChain returns the locales to try, most specific first, always ending at Default.
//
// "ar-SY" yields [ar-SY, ar, en]: a Syrian-specific override falls back to general Arabic
// before English, so a partial regional catalog is useful rather than all-or-nothing.
func (l Locale) FallbackChain() []Locale {
	chain := []Locale{}
	if l != "" {
		chain = append(chain, l)
	}
	if lang := Locale(l.Language()); lang != l && lang != "" {
		chain = append(chain, lang)
	}
	if l != Default && Locale(l.Language()) != Default {
		chain = append(chain, Default)
	}
	if len(chain) == 0 {
		chain = append(chain, Default)
	}
	return chain
}

// IsZero reports whether the locale is unset.
func (l Locale) IsZero() bool { return l == "" }

// String returns the tag.
func (l Locale) String() string { return string(l) }

// ── context ─────────────────────────────────────────────────────────────────────

type ctxKey struct{}

// WithLocale attaches the active locale to ctx. The API layer sets it per request from the
// user's setting; everything downstream inherits it.
func WithLocale(ctx context.Context, l Locale) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the active locale, or Default when none is set.
//
// It cannot fail: a missing locale resolves to English rather than an error, because no
// caller should have to handle "the user has no language" on the way to rendering a label.
func FromContext(ctx context.Context) Locale {
	if l, ok := ctx.Value(ctxKey{}).(Locale); ok && !l.IsZero() {
		return l
	}
	return Default
}

func isAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

func isAlphaNumeric(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}
