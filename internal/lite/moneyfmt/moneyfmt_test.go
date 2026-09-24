package moneyfmt_test

import (
	"testing"
	"unicode"

	"github.com/mizan-erp/mizan/internal/lite/moneyfmt"
)

func syp(mode moneyfmt.Display) moneyfmt.Shop {
	return moneyfmt.Shop{Mode: mode, Local: "SYP"}
}

func TestTheOldPoundIsLeftExactlyAsItIs(t *testing.T) {
	shop := syp(moneyfmt.Legacy)
	for _, text := range []string{"15000", "0", "4.88", "-2000", "1234567"} {
		if got := shop.Display(text, "SYP"); got != text {
			t.Errorf("Display(%q) = %q — the old pound must pass through untouched", text, got)
		}
		if got := shop.Base(text, "SYP"); got != text {
			t.Errorf("Base(%q) = %q", text, got)
		}
	}
}

func TestTheNewPoundDropsTwoNoughts(t *testing.T) {
	shop := syp(moneyfmt.New)
	for typed, want := range map[string]string{
		"15000":   "150",
		"500":     "5",      // the cash note
		"13675":   "136.75", // the market rate
		"97500":   "975",
		"50":      "0.5",
		"7":       "0.07",
		"0":       "0",
		"-20000":  "-200",
		"1234567": "12345.67",
	} {
		if got := shop.Display(typed, "SYP"); got != want {
			t.Errorf("Display(%q) = %q, want %q", typed, got, want)
		}
	}
}

// The dollar is not the pound's redenomination: a price of $3.25 reads $3.25 however the shop reads its pounds.
func TestDollarsAreNeverRedenominated(t *testing.T) {
	for _, mode := range []moneyfmt.Display{moneyfmt.Legacy, moneyfmt.New, moneyfmt.Dual} {
		shop := syp(mode)
		if got := shop.Display("3.25", "USD"); got != "3.25" {
			t.Errorf("%s: a dollar price was changed to %q", mode, got)
		}
		if got := shop.Base("3.25", "USD"); got != "3.25" {
			t.Errorf("%s: a typed dollar was changed to %q", mode, got)
		}
	}
}

func TestDualCarriesBothFigures(t *testing.T) {
	shop := syp(moneyfmt.Dual)
	got := shop.Display("15000", "SYP")
	if got != "150 (15000)" {
		t.Fatalf("Display = %q, want the new figure with the old one in brackets", got)
	}
	fresh, legacy, ok := moneyfmt.SplitDual(got)
	if !ok || fresh != "150" || legacy != "15000" {
		t.Fatalf("SplitDual(%q) = %q, %q, %v", got, fresh, legacy, ok)
	}
	// A person types the new figure in dual mode: the old one is there to be recognised, not typed.
	if base := shop.Base("150", "SYP"); base != "15000" {
		t.Fatalf("Base(150) = %q, want 15000", base)
	}
}

// TestADualReadingIsPrintableWhereverItLands is the fault this shape was chosen to prevent: a screen, a receipt or a
// workbook cell that prints the figure without knowing it carries two must still show something a person can read. The
// old separator was a control character, which every such place drew as a broken box.
func TestADualReadingIsPrintableWhereverItLands(t *testing.T) {
	shop := syp(moneyfmt.Dual)
	for _, stored := range []string{"15000", "13675", "0", "-20000", "7"} {
		shown := shop.Display(stored, "SYP")
		for _, r := range shown {
			if unicode.IsControl(r) || !unicode.IsPrint(r) {
				t.Errorf("Display(%q) = %q carries %U, which no screen or printer can draw", stored, shown, r)
			}
		}
		// And what a bare printer draws is the reading the owner asked for: "137 (13700)".
		fresh, legacy, ok := moneyfmt.SplitDual(shown)
		if !ok || fresh+" ("+legacy+")" != shown {
			t.Errorf("Display(%q) = %q does not read as a figure and its old self", stored, shown)
		}
	}
	// A single figure is not a dual reading, and must not be taken apart as one.
	for _, single := range []string{"15000", "4.88", "-2000", "0"} {
		if _, _, ok := moneyfmt.SplitDual(single); ok {
			t.Errorf("SplitDual(%q) claimed two figures", single)
		}
	}
}

// TestWhatAPersonTypesComesBackUnchanged is the property that matters most: a figure typed, stored and read again is the
// same figure. A shop that types 150 and later reads 1.50 has been lied to about its own money.
func TestWhatAPersonTypesComesBackUnchanged(t *testing.T) {
	for _, mode := range []moneyfmt.Display{moneyfmt.Legacy, moneyfmt.New, moneyfmt.Dual} {
		shop := syp(mode)
		for _, typed := range []string{"150", "5", "136.75", "0", "0.07", "12345.67", "-200"} {
			stored := shop.Base(typed, "SYP")
			read := shop.Display(stored, "SYP")
			if mode == moneyfmt.Dual {
				read, _, _ = moneyfmt.SplitDual(read)
			}
			if read != typed {
				t.Errorf("%s: typed %q, stored %q, read back %q", mode, typed, stored, read)
			}
		}
	}
}

// And the other direction: what the books hold survives being shown and typed back.
func TestWhatTheBooksHoldSurvivesTheRoundTrip(t *testing.T) {
	for _, mode := range []moneyfmt.Display{moneyfmt.Legacy, moneyfmt.New, moneyfmt.Dual} {
		shop := syp(mode)
		for _, stored := range []string{"15000", "500", "13675", "1", "0", "1234567"} {
			shown := shop.Display(stored, "SYP")
			if mode == moneyfmt.Dual {
				shown, _, _ = moneyfmt.SplitDual(shown)
			}
			if back := shop.Base(shown, "SYP"); back != stored {
				t.Errorf("%s: books held %q, shown %q, typed back as %q", mode, stored, shown, back)
			}
		}
	}
}

func TestAnUnknownDisplaySettingReadsAsTheOldPound(t *testing.T) {
	for _, raw := range []string{"", "nonsense", "NEWISH"} {
		if got := moneyfmt.Parse(raw); got != moneyfmt.Legacy {
			t.Errorf("Parse(%q) = %q, want the old pound", raw, got)
		}
		if moneyfmt.Valid(raw) {
			t.Errorf("Valid(%q) said yes", raw)
		}
	}
	for _, raw := range []string{"legacy", "new", "dual", "DUAL", " new "} {
		if !moneyfmt.Valid(raw) {
			t.Errorf("Valid(%q) said no", raw)
		}
	}
}

// An empty figure is not a nought: a report leaves a cell empty where a shop has no rate, and it must stay empty.
func TestAnEmptyFigureStaysEmpty(t *testing.T) {
	for _, mode := range []moneyfmt.Display{moneyfmt.Legacy, moneyfmt.New, moneyfmt.Dual} {
		shop := syp(mode)
		if got := shop.Display("", "SYP"); got != "" {
			t.Errorf("%s: an empty figure became %q", mode, got)
		}
		if got := shop.Base("", "SYP"); got != "" {
			t.Errorf("%s: an empty typed figure became %q", mode, got)
		}
	}
}

// TestAFigureTypedInArabicDigitsIsTheSameFigure (0.9.9): the keyboard decides which digits arrive (numinput), and a
// figure typed with Arabic-Indic digits and the Arabic decimal separator is the figure typed with Latin ones. Base once
// moved the point in the text as typed, which knows only '.', and read ١٫٥ new pounds as 1.5 old pounds instead of 150 —
// a till's tendered amount, a payment, a delivery's cost, all a hundredth of what was typed.
func TestAFigureTypedInArabicDigitsIsTheSameFigure(t *testing.T) {
	for _, mode := range []moneyfmt.Display{moneyfmt.New, moneyfmt.Dual} {
		shop := syp(mode)
		for _, c := range []struct{ typed, want string }{
			{"١٫٥", "150"},
			{"١٥٠", "15000"},
			{"۱۵۰٫۲۵", "15025"},
			{" 136٫75 ", "13675"}, // a mixed figure, with the space a keyboard leaves
			{"٫٠٧", "7"},
			{"-٢٠٠", "-20000"},
		} {
			if got := shop.Base(c.typed, "SYP"); got != c.want {
				t.Errorf("%s: Base(%q) = %q, want %q", mode, c.typed, got, c.want)
			}
		}
	}
}

// What cannot be read as a figure is handed on as it was typed, for the parser it is going to to refuse with its reason.
// Moving a point in it would turn "1,500" into a different wrong figure, and the refusal would quote something the
// person never typed.
func TestWhatIsNotAFigureIsHandedOnAsTyped(t *testing.T) {
	shop := syp(moneyfmt.New)
	for _, typed := range []string{"1,500", "abc", "1.2.3", "٬٥٠٠"} {
		if got := shop.Base(typed, "SYP"); got != typed {
			t.Errorf("Base(%q) = %q, want it untouched", typed, got)
		}
	}
}

// TestADollarsOnlyShopShiftsNothing (0.10.0): "usd" is a setting of its own, and a shop that reads it moves no point in
// any figure — it types no pounds, and the pounds the books still hold are shown by no screen of it.
func TestADollarsOnlyShopShiftsNothing(t *testing.T) {
	if moneyfmt.Parse(" USD ") != moneyfmt.USD || !moneyfmt.Valid("usd") {
		t.Fatal("usd is not a display")
	}
	shop := moneyfmt.Shop{Mode: moneyfmt.USD, Local: "SYP"}
	if !shop.USDOnly() || (moneyfmt.Shop{Mode: moneyfmt.Dual, Local: "SYP"}).USDOnly() {
		t.Fatal("USDOnly")
	}
	if got := shop.Display("15000", "SYP"); got != "15000" {
		t.Fatalf("Display = %q", got)
	}
	if got := shop.Base("150", "SYP"); got != "150" {
		t.Fatalf("Base = %q", got)
	}
	if got := shop.Display("3.25", "USD"); got != "3.25" {
		t.Fatalf("Display = %q", got)
	}
}
