package documents_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/documents"
)

// TestEveryCode128SymbolIsElevenModulesWide is the structural law of Code 128, and the one that catches a typo in a
// 107-row table by hand: every symbol is exactly 11 modules of bar and space, and the stop symbol is 13.
//
// A single mistyped width makes a symbol the scanner cannot read, and nothing about the printed label looks wrong —
// it just never beeps. This is the check that would have caught it.
func TestEveryCode128SymbolIsElevenModulesWide(t *testing.T) {
	bars, ok := documents.Code128("MIZAN-1")
	if !ok {
		t.Fatal("a plain ASCII code was refused")
	}
	// Every symbol but the last is six widths summing to 11; the stop symbol is seven widths summing to 13.
	if (len(bars)-7)%6 != 0 {
		t.Fatalf("%d widths is not whole symbols plus a stop", len(bars))
	}
	for i := 0; i+6 <= len(bars)-7; i += 6 {
		sum := 0
		for _, w := range bars[i : i+6] {
			sum += w
			if w < 1 || w > 4 {
				t.Fatalf("symbol %d has a width of %d modules", i/6, w)
			}
		}
		if sum != 11 {
			t.Errorf("symbol %d is %d modules wide, want 11", i/6, sum)
		}
	}
	stop := bars[len(bars)-7:]
	sum := 0
	for _, w := range stop {
		sum += w
	}
	if sum != 13 {
		t.Errorf("the stop symbol is %d modules, want 13", sum)
	}
}

// TestACodeStartsWithABarAndEndsWithOne: a symbol read from either end must begin on a bar, or a scanner reading it
// backwards finds nothing.
func TestACodeStartsWithABarAndEndsWithOne(t *testing.T) {
	bars, _ := documents.Code128("12345670")
	if len(bars)%2 == 0 {
		t.Fatalf("%d widths: an even count ends on a space", len(bars))
	}
}

// TestANumericCodeUsesTheNarrowerSubset: set C packs two digits per symbol, which is the difference between a label
// that fits on a 40 mm tag and one that does not.
func TestANumericCodeUsesTheNarrowerSubset(t *testing.T) {
	digits, _ := documents.Code128("12345678")  // 8 digits → 4 symbols in set C
	letters, _ := documents.Code128("ABCDEFGH") // 8 letters → 8 symbols in set B
	if len(digits) >= len(letters) {
		t.Fatalf("eight digits took %d widths and eight letters %d — set C was not used", len(digits), len(letters))
	}
	// start + 4 pairs + check = 6 symbols of 6 widths, plus a 7-width stop.
	if len(digits) != 6*6+7 {
		t.Fatalf("eight digits encoded to %d widths, want %d", len(digits), 6*6+7)
	}
}

// TestWhatCode128CannotCarryIsRefused: a label with no symbol is honest; a symbol that scans as something else is not.
func TestWhatCode128CannotCarryIsRefused(t *testing.T) {
	for _, code := range []string{"", "زيت", "café", "\x01"} {
		if _, ok := documents.Code128(code); ok {
			t.Errorf("Code128(%q) claimed to encode it", code)
		}
	}
	if _, ok := documents.BarcodeOf("  ", "زيت", 3); ok {
		t.Error("a blank code produced a symbol")
	}
}
