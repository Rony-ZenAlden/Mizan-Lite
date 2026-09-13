package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

func TestParseLocaleAcceptsOnlyShippedLocales(t *testing.T) {
	accepted := []struct {
		raw  string
		want domain.Locale
	}{
		{"ar", domain.Arabic}, {"en", domain.English},
		{" AR ", domain.Arabic}, // surrounding space and case are forgiven
		{"En", domain.English},
	}
	for _, tc := range accepted {
		got, err := domain.ParseLocale(tc.raw)
		if err != nil || got != tc.want {
			t.Errorf("ParseLocale(%q) = %q, %v; want %q", tc.raw, got, err, tc.want)
		}
	}
	// A region, another language, and empty are all refused rather than guessed at.
	for _, raw := range []string{"ar-SY", "fr", "", "arabic", "ع"} {
		_, err := domain.ParseLocale(raw)
		if errs.CodeOf(err) != domain.CodeInvalidLocale {
			t.Errorf("ParseLocale(%q) code = %q, want %q", raw, errs.CodeOf(err), domain.CodeInvalidLocale)
		}
		if !errs.IsCategory(err, errs.CategoryValidation) {
			t.Errorf("ParseLocale(%q) is not a validation error: %v", raw, err)
		}
	}
}

func TestArabicIsRightToLeftAndEnglishIsLeftToRight(t *testing.T) {
	if domain.Arabic.Direction() != domain.RTL {
		t.Errorf("Arabic direction = %q", domain.Arabic.Direction())
	}
	if domain.English.Direction() != domain.LTR {
		t.Errorf("English direction = %q", domain.English.Direction())
	}
}

func TestAFreshInstallationOpensInArabic(t *testing.T) {
	if domain.Defaults().Locale != domain.Arabic {
		t.Fatalf("default locale = %q, want Arabic (design D3)", domain.Defaults().Locale)
	}
}

func TestFromStoredReportsEveryUnusableRowAndKeepsTheDefault(t *testing.T) {
	t.Run("a valid stored locale is used", func(t *testing.T) {
		got, problems := domain.FromStored(map[string]string{domain.KeyLocale: "en"})
		if got.Locale != domain.English || len(problems) != 0 {
			t.Fatalf("got %+v, problems %+v", got, problems)
		}
	})
	t.Run("an invalid value keeps the default and is reported", func(t *testing.T) {
		got, problems := domain.FromStored(map[string]string{domain.KeyLocale: "klingon"})
		if got.Locale != domain.Arabic {
			t.Fatalf("locale = %q, want the default", got.Locale)
		}
		if len(problems) != 1 || problems[0].Kind != domain.InvalidValue || problems[0].Key != domain.KeyLocale {
			t.Fatalf("problems = %+v", problems)
		}
	})
	t.Run("an unknown key is reported and ignored", func(t *testing.T) {
		got, problems := domain.FromStored(map[string]string{"from.a.newer.build": "x", domain.KeyLocale: "en"})
		if got.Locale != domain.English {
			t.Fatalf("an unknown key disturbed a known one: %+v", got)
		}
		if len(problems) != 1 || problems[0].Kind != domain.UnknownKey {
			t.Fatalf("problems = %+v", problems)
		}
	})
	t.Run("nothing stored is the defaults with no problems", func(t *testing.T) {
		got, problems := domain.FromStored(nil)
		if got != domain.Defaults() || len(problems) != 0 {
			t.Fatalf("got %+v, problems %+v", got, problems)
		}
	})
}

func TestApply(t *testing.T) {
	en, ar, bad := "en", "ar", "xx"
	start := domain.Defaults() // Arabic

	t.Run("a nil field changes nothing and writes nothing", func(t *testing.T) {
		next, changes, err := start.Apply(domain.Update{})
		if err != nil || next != start || len(changes) != 0 {
			t.Fatalf("next %+v, changes %+v, err %v", next, changes, err)
		}
	})
	t.Run("setting the current value writes nothing", func(t *testing.T) {
		_, changes, err := start.Apply(domain.Update{Locale: &ar})
		if err != nil || len(changes) != 0 {
			t.Fatalf("changes %+v, err %v", changes, err)
		}
	})
	t.Run("a change is returned with the row to write", func(t *testing.T) {
		next, changes, err := start.Apply(domain.Update{Locale: &en})
		if err != nil {
			t.Fatal(err)
		}
		if next.Locale != domain.English {
			t.Fatalf("next = %+v", next)
		}
		want := domain.Change{Key: domain.KeyLocale, Value: "en"}
		if len(changes) != 1 || changes[0] != want {
			t.Fatalf("changes = %+v, want [%+v]", changes, want)
		}
	})
	t.Run("an invalid value fails and leaves the settings as they were", func(t *testing.T) {
		next, changes, err := start.Apply(domain.Update{Locale: &bad})
		if errs.CodeOf(err) != domain.CodeInvalidLocale {
			t.Fatalf("err = %v", err)
		}
		if next != start || changes != nil {
			t.Fatalf("a failed apply changed state: next %+v, changes %+v", next, changes)
		}
	})
}
