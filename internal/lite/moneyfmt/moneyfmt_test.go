package moneyfmt_test

import (
	"strings"
	"testing"

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
	parts := strings.Split(got, moneyfmt.DualSeparator)
	if len(parts) != 2 || parts[0] != "150" || parts[1] != "15000" {
		t.Fatalf("Display = %q, want the new figure and the old one", got)
	}
	// A person types the new figure in dual mode: the old one is there to be recognised, not typed.
	if base := shop.Base("150", "SYP"); base != "15000" {
		t.Fatalf("Base(150) = %q, want 15000", base)
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
				read = strings.Split(read, moneyfmt.DualSeparator)[0]
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
				shown = strings.Split(shown, moneyfmt.DualSeparator)[0]
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
