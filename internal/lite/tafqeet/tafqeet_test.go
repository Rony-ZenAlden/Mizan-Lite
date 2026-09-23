package tafqeet_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/tafqeet"
)

// TestTheReferenceInvoiceTotal is the total on the invoice the owner gave as the model (2026-09-24): 1,334.500 dollars.
func TestTheReferenceInvoiceTotal(t *testing.T) {
	got, err := tafqeet.Arabic("1334.50", tafqeet.USD)
	if err != nil {
		t.Fatal(err)
	}
	if want := "فقط ألف وثلاثمائة وأربعة وثلاثون دولاراً أمريكياً وخمسون سنتاً لا غير"; got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// TestTheNumberDecidesTheNounsForm: one rule per row — the noun after 1, 2, 3–10, 11–99, and a round hundred.
func TestTheNumberDecidesTheNounsForm(t *testing.T) {
	for amount, want := range map[string]string{
		"0":    "فقط صفر دولار أمريكي لا غير",
		"1":    "فقط دولار أمريكي واحد لا غير",
		"2":    "فقط دولاران أمريكيان لا غير",
		"3":    "فقط ثلاثة دولارات أمريكية لا غير",
		"10":   "فقط عشرة دولارات أمريكية لا غير",
		"11":   "فقط أحد عشر دولاراً أمريكياً لا غير",
		"12":   "فقط اثنا عشر دولاراً أمريكياً لا غير",
		"19":   "فقط تسعة عشر دولاراً أمريكياً لا غير",
		"21":   "فقط واحد وعشرون دولاراً أمريكياً لا غير",
		"99":   "فقط تسعة وتسعون دولاراً أمريكياً لا غير",
		"100":  "فقط مائة دولار أمريكي لا غير",
		"103":  "فقط مائة وثلاثة دولارات أمريكية لا غير",
		"115":  "فقط مائة وخمسة عشر دولاراً أمريكياً لا غير",
		"200":  "فقط مائتا دولار أمريكي لا غير",
		"250":  "فقط مائتان وخمسون دولاراً أمريكياً لا غير",
		"1200": "فقط ألف ومائتا دولار أمريكي لا غير",
	} {
		if got, err := tafqeet.Arabic(amount, tafqeet.USD); err != nil || got != want {
			t.Errorf("%s:\n got  %s (%v)\n want %s", amount, got, err, want)
		}
	}
}

// TestThousandsAndMillionsAreCountedToo: a scale word is a noun counted like any other, and one followed straight by the
// currency takes its construct form — ألفا دولار, أحد عشر ألف دولار.
func TestThousandsAndMillionsAreCountedToo(t *testing.T) {
	for amount, want := range map[string]string{
		"1000":       "فقط ألف دولار أمريكي لا غير",
		"2000":       "فقط ألفا دولار أمريكي لا غير",
		"2500":       "فقط ألفان وخمسمائة دولار أمريكي لا غير",
		"3000":       "فقط ثلاثة آلاف دولار أمريكي لا غير",
		"11000":      "فقط أحد عشر ألف دولار أمريكي لا غير",
		"11005":      "فقط أحد عشر ألفاً وخمسة دولارات أمريكية لا غير",
		"103000":     "فقط مائة وثلاثة آلاف دولار أمريكي لا غير",
		"200000":     "فقط مائتا ألف دولار أمريكي لا غير",
		"1000000":    "فقط مليون دولار أمريكي لا غير",
		"2000000":    "فقط مليونا دولار أمريكي لا غير",
		"3000000":    "فقط ثلاثة ملايين دولار أمريكي لا غير",
		"1011000":    "فقط مليون وأحد عشر ألف دولار أمريكي لا غير",
		"2000000000": "فقط مليارا دولار أمريكي لا غير",
	} {
		if got, err := tafqeet.Arabic(amount, tafqeet.USD); err != nil || got != want {
			t.Errorf("%s:\n got  %s (%v)\n want %s", amount, got, err, want)
		}
	}
}

// TestAFeminineCurrencyTurnsTheNumbers: ليرة is feminine, so the numbers that agree agree with it and those that take the
// opposite gender take the masculine form — ثلاث ليرات, إحدى عشرة ليرة, ثمان وعشرون ليرة.
func TestAFeminineCurrencyTurnsTheNumbers(t *testing.T) {
	for amount, want := range map[string]string{
		"1":       "فقط ليرة سورية واحدة لا غير",
		"2":       "فقط ليرتان سوريتان لا غير",
		"3":       "فقط ثلاث ليرات سورية لا غير",
		"8":       "فقط ثماني ليرات سورية لا غير",
		"10":      "فقط عشر ليرات سورية لا غير",
		"11":      "فقط إحدى عشرة ليرة سورية لا غير",
		"12":      "فقط اثنتا عشرة ليرة سورية لا غير",
		"18":      "فقط ثماني عشرة ليرة سورية لا غير",
		"21":      "فقط إحدى وعشرون ليرة سورية لا غير",
		"28":      "فقط ثمان وعشرون ليرة سورية لا غير",
		"3000":    "فقط ثلاثة آلاف ليرة سورية لا غير",
		"1298500": "فقط مليون ومائتان وثمانية وتسعون ألفاً وخمسمائة ليرة سورية لا غير",
		"150.25":  "فقط مائة وخمسون ليرة سورية وخمسة وعشرون قرشاً لا غير",
	} {
		if got, err := tafqeet.Arabic(amount, tafqeet.SYP); err != nil || got != want {
			t.Errorf("%s:\n got  %s (%v)\n want %s", amount, got, err, want)
		}
	}
}

// TestSmallUnitsAlone: an amount under one dollar is its cents, and "5" written ".5" is fifty.
func TestSmallUnitsAlone(t *testing.T) {
	for amount, want := range map[string]string{
		"0.50": "فقط خمسون سنتاً لا غير",
		"0.5":  "فقط خمسون سنتاً لا غير",
		"0.05": "فقط خمسة سنتات لا غير",
		"0.01": "فقط سنت واحد لا غير",
		"3.02": "فقط ثلاثة دولارات أمريكية وسنتان لا غير",
	} {
		if got, err := tafqeet.Arabic(amount, tafqeet.USD); err != nil || got != want {
			t.Errorf("%s:\n got  %s (%v)\n want %s", amount, got, err, want)
		}
	}
}

func TestEnglish(t *testing.T) {
	for amount, want := range map[string]string{
		"1334.50": "One thousand three hundred thirty-four US dollars and fifty cents only",
		"1":       "One US dollar only",
		"0.01":    "One cent only",
		"2000000": "Two million US dollars only",
		"115.05":  "One hundred fifteen US dollars and five cents only",
	} {
		if got, err := tafqeet.English(amount, tafqeet.USD); err != nil || got != want {
			t.Errorf("%s:\n got  %s (%v)\n want %s", amount, got, err, want)
		}
	}
}

// TestWhatCannotBeWrittenIsRefused: a sign, a grouping comma, more decimals than cents, or a trillion. Words that do not
// say the figure are worse than no words: the line exists to be checked against the figure.
func TestWhatCannotBeWrittenIsRefused(t *testing.T) {
	for _, amount := range []string{"", "-5", "1,334.50", "1.234", "abc", "1.2.3", "1000000000000", "١٢"} {
		if _, err := tafqeet.Arabic(amount, tafqeet.USD); errs.CodeOf(err) != tafqeet.CodeInvalid {
			t.Errorf("%q: error %v, want %s", amount, err, tafqeet.CodeInvalid)
		}
	}
}

func TestForCode(t *testing.T) {
	if c, ok := tafqeet.ForCode("usd"); !ok || c.Major.One != tafqeet.USD.Major.One {
		t.Fatal("USD not found by its code")
	}
	if _, ok := tafqeet.ForCode("EUR"); ok {
		t.Fatal("a currency this package cannot name was found")
	}
}
