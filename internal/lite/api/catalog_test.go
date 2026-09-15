package api_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/lite/locales"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
)

// TestEveryLabelAPrintoutUsesIsTranslated reads the binding layer for the catalog keys printouts and exports are written with,
// and the keys they build at run time, and requires each in both languages — a missing one would print as its key.
func TestEveryLabelAPrintoutUsesIsTranslated(t *testing.T) {
	catalog, err := i18n.LoadFS(locales.FS())
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("*.go")
	literal := regexp.MustCompile(`w\.t\("([a-z0-9_.]+)"`)
	keys := map[string]bool{}
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		for _, m := range literal.FindAllStringSubmatch(string(raw), -1) {
			if m[1][len(m[1])-1] != '.' {
				keys[m[1]] = true
			}
		}
	}
	if len(keys) < 150 {
		t.Fatalf("found %d keys; the scan is not reading the printouts", len(keys))
	}
	dynamic := map[string][]string{
		"doc.payment.":      {"cash", "credit"},
		"printers.path.":    {"driver", "raw"},
		"sales.status.":     {"posted", "voided"},
		"debt.kind.":        {"opening", "charge", "payment", "write_off", "refund", "reversal"},
		"cash.kind.":        {"expense", "withdrawal", "deposit", "count", "reversal"},
		"cash.category.":    {"rent", "electricity", "wages", "transport", "supplies", "other"},
		"reports.left_out.": {"no_stock", "no_cost", "inactive", "no_rate"},
		"currency.":         {"SYP", "USD"},
		"currency.short.":   {"SYP", "USD"},
		"uom.":              {"kg", "l", "piece", "jar", "tin", "bag", "box", "bottle", "container"},
	}
	for prefix, values := range dynamic {
		for _, v := range values {
			keys[prefix+v] = true
		}
	}
	for key := range keys {
		for _, l := range []locale.Locale{"ar", "en"} {
			if !catalog.Has(l, key) {
				t.Errorf("%s: %s is used by a printout and has no translation", l, key)
			}
		}
	}
}
