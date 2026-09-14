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

func TestShopName(t *testing.T) {
	if got, err := domain.ParseShopName("  بقالية المونة  "); err != nil || got != "بقالية المونة" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := domain.ParseShopName("   "); errs.CodeOf(err) != domain.CodeShopNameRequired {
		t.Fatalf("an empty shop name = %v", err)
	}
	long := make([]rune, domain.MaxShopNameRunes+1)
	for i := range long {
		long[i] = 'م'
	}
	if _, err := domain.ParseShopName(string(long)); errs.CodeOf(err) != domain.CodeShopNameTooLong {
		t.Fatalf("an over-long shop name = %v", err)
	}
}

func TestShopNameIsStoredAndApplied(t *testing.T) {
	name := "  المونة  "
	next, changes, err := domain.Defaults().Apply(domain.Update{ShopName: &name})
	if err != nil || next.ShopName != "المونة" || len(changes) != 1 || changes[0].Key != domain.KeyShopName {
		t.Fatalf("next %+v changes %+v err %v", next, changes, err)
	}
	got, problems := domain.FromStored(map[string]string{domain.KeyShopName: "المونة"})
	if got.ShopName != "المونة" || len(problems) != 0 {
		t.Fatalf("got %+v problems %+v", got, problems)
	}
	got, problems = domain.FromStored(map[string]string{domain.KeyShopName: "  "})
	if got.ShopName != "" || len(problems) != 1 {
		t.Fatalf("a damaged stored shop name: %+v %+v", got, problems)
	}
	empty := ""
	if _, _, err := domain.Defaults().Apply(domain.Update{ShopName: &empty}); errs.CodeOf(err) != domain.CodeShopNameRequired {
		t.Fatalf("clearing the shop name = %v", err)
	}
}

func TestRateModeAndLocalCurrency(t *testing.T) {
	d := domain.Defaults()
	if d.RateMode != domain.RateManual || d.LocalCurrency != "SYP" {
		t.Fatalf("defaults = %+v", d)
	}
	got, problems := domain.FromStored(map[string]string{domain.KeyRateMode: "automatic", domain.KeyLocalCurrency: "SYP"})
	if got.RateMode != domain.RateAutomatic || got.LocalCurrency != "SYP" || len(problems) != 0 {
		t.Fatalf("stored = %+v, %v", got, problems)
	}
	got, problems = domain.FromStored(map[string]string{domain.KeyRateMode: "sometimes", domain.KeyLocalCurrency: "syp"})
	if got.RateMode != domain.RateManual || got.LocalCurrency != "SYP" || len(problems) != 2 {
		t.Fatalf("damaged values were used: %+v, %v", got, problems)
	}
	automatic := "automatic"
	next, changes, err := d.Apply(domain.Update{RateMode: &automatic})
	if err != nil || next.RateMode != domain.RateAutomatic || len(changes) != 1 || changes[0] != (domain.Change{Key: domain.KeyRateMode, Value: "automatic"}) {
		t.Fatalf("Apply = %+v %v %v", next, changes, err)
	}
	bad := "auto"
	if _, _, err := d.Apply(domain.Update{RateMode: &bad}); errs.CodeOf(err) != domain.CodeInvalidRateMode {
		t.Fatalf("bad mode: %v", err)
	}
}
