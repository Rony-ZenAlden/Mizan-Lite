package locale_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/locale"
)

func TestParseAcceptsWellFormedTags(t *testing.T) {
	// Any well-formed tag, not only the two shipped today: the catalog loader discovers
	// locales from the filesystem, so a build with locales/ku/ must parse "ku".
	// A slice rather than a map because one case deliberately has surrounding whitespace,
	// which is exactly what Parse must trim.
	cases := []struct {
		input string
		want  locale.Locale
	}{
		{"en", "en"},
		{"ar", "ar"},
		{"ku", "ku"},
		{"ckb", "ckb"},
		{"ar-SY", "ar-SY"},
		{"ar_sy", "ar-SY"}, // separator and case normalised
		{"AR-sy", "ar-SY"},
		{" en ", "en"}, // trimmed
	}
	for _, tc := range cases {
		input, want := tc.input, tc.want
		t.Run(input, func(t *testing.T) {
			got, ok := locale.Parse(input)
			if !ok {
				t.Fatalf("Parse(%q) rejected a well-formed tag", input)
			}
			if got != want {
				t.Errorf("Parse(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "   ", "e", "toolongtag", "en--US", "en US", "1234", "a-"} {
		t.Run(bad, func(t *testing.T) {
			if got, ok := locale.Parse(bad); ok {
				t.Errorf("Parse(%q) = %q; want rejection", bad, got)
			}
		})
	}
}

func TestDirection(t *testing.T) {
	rtl := []locale.Locale{"ar", "ar-SY", "fa", "he", "ur", "ckb"}
	ltr := []locale.Locale{"en", "en-GB", "fr", "tr", "ku"}

	for _, l := range rtl {
		if !l.IsRTL() || l.Direction() != locale.RTL {
			t.Errorf("%q should be RTL", l)
		}
	}
	for _, l := range ltr {
		if l.IsRTL() || l.Direction() != locale.LTR {
			t.Errorf("%q should be LTR", l)
		}
	}
}

func TestLanguageSubtag(t *testing.T) {
	// Direction is keyed on the language alone, so ar-SY and ar-EG both work without
	// enumerating regions.
	if got := locale.Locale("ar-SY").Language(); got != "ar" {
		t.Errorf("Language = %q, want ar", got)
	}
	if got := locale.Locale("en").Language(); got != "en" {
		t.Errorf("Language = %q, want en", got)
	}
}

func TestFallbackChain(t *testing.T) {
	cases := map[locale.Locale][]locale.Locale{
		"ar":    {"ar", "en"},
		"ar-SY": {"ar-SY", "ar", "en"}, // regional → general → default
		"en":    {"en"},
		"en-GB": {"en-GB", "en"},
		"":      {"en"},
	}
	for input, want := range cases {
		t.Run(string(input), func(t *testing.T) {
			got := input.FallbackChain()
			if len(got) != len(want) {
				t.Fatalf("chain for %q = %v, want %v", input, got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("chain for %q = %v, want %v", input, got, want)
				}
			}
		})
	}
}

func TestFallbackChainAlwaysEndsAtDefault(t *testing.T) {
	// Every chain must terminate somewhere guaranteed to define every key.
	for _, l := range []locale.Locale{"ar", "ar-SY", "ku", "fr-CA", ""} {
		chain := l.FallbackChain()
		if last := chain[len(chain)-1]; last != locale.Default && last.Language() != string(locale.Default) {
			t.Errorf("chain for %q ends at %q, not the default locale", l, last)
		}
	}
}

func TestContextCarrier(t *testing.T) {
	ctx := context.Background()
	if got := locale.FromContext(ctx); got != locale.Default {
		t.Errorf("unset context = %q, want the default %q", got, locale.Default)
	}

	arabic := locale.WithLocale(ctx, "ar")
	if got := locale.FromContext(arabic); got != "ar" {
		t.Errorf("FromContext = %q, want ar", got)
	}
	// The parent is untouched.
	if got := locale.FromContext(ctx); got != locale.Default {
		t.Errorf("the parent context was mutated: %q", got)
	}
	// An empty locale in context still yields the default rather than "".
	if got := locale.FromContext(locale.WithLocale(ctx, "")); got != locale.Default {
		t.Errorf("empty locale = %q, want the default", got)
	}
}
