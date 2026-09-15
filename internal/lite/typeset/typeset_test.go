package typeset_test

import (
	"image"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

func setter(t *testing.T) *typeset.Typesetter {
	t.Helper()
	ts, err := typeset.Default()
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

var body = typeset.Style{Size: 22}

// visual is the text of a line's glyphs read left to right, bidi controls dropped.
func visual(l typeset.Line) string {
	var b strings.Builder
	for _, g := range l.Glyphs {
		for _, r := range g.Runes {
			if !typeset.IsBidiControl(r) {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func TestArabicIsJoinedAndLigated(t *testing.T) {
	ts := setter(t)
	if l := ts.Layout("لا", body, typeset.RTL); len(l.Glyphs) != 1 || string(l.Glyphs[0].Runes) != "لا" {
		t.Fatalf("lam-alef is one glyph: %+v", l.Glyphs)
	}
	isolated := ts.Layout("ب", body, typeset.RTL).Glyphs[0].GID
	three := ts.Layout("ببب", body, typeset.RTL).Glyphs
	if len(three) != 3 {
		t.Fatalf("%d glyphs", len(three))
	}
	forms := map[uint16]bool{isolated: true}
	for _, g := range three {
		forms[g.GID] = true
	}
	if len(forms) != 4 {
		t.Fatalf("initial, medial and final beh are not three forms beside the isolated one: %v", forms)
	}
}

func TestAFigureIsIsolatedLeftToRight(t *testing.T) {
	ts := setter(t)
	plain := visual(ts.Layout("فاتورة رقم 12 · 2026-09-14 10:32", body, typeset.RTL))
	isolated := visual(ts.Layout("فاتورة رقم 12 · "+typeset.Isolate("2026-09-14 10:32"), body, typeset.RTL))
	if !strings.Contains(isolated, "2026-09-14 10:32") {
		t.Fatalf("isolated, the date reads %q", isolated)
	}
	if strings.Contains(plain, "2026-09-14 10:32") {
		t.Fatalf("without isolation the date should reorder (L7 H3) — the test no longer shows why the rule exists: %q", plain)
	}
	amount := visual(ts.Layout("المجموع "+typeset.Isolate("13.00 USD"), body, typeset.RTL))
	if !strings.HasPrefix(amount, "13.00 USD") {
		t.Fatalf("an isolated amount sits at the line's left end, in its own order: %q", amount)
	}
	if typeset.Isolate("") != "" || !typeset.IsBidiControl('⁦') || typeset.IsBidiControl('a') {
		t.Fatal("isolate helpers")
	}
}

func TestMixedArabicAndLatinLineOrder(t *testing.T) {
	ts := setter(t)
	l := ts.Layout("زيت زيتون (Olive oil)", body, typeset.RTL)
	var latinMax, arabicMin float32 = -1, 1 << 20
	for _, g := range l.Glyphs {
		switch r := g.Runes[0]; {
		case r >= 'A' && r <= 'z':
			latinMax = max(latinMax, g.X)
		case r >= 0x0600 && r <= 0x06FF:
			arabicMin = min(arabicMin, g.X)
		}
	}
	if latinMax < 0 || latinMax >= arabicMin {
		t.Fatalf("in a right-to-left line the Latin sits left of the Arabic: latin %.1f, arabic %.1f", latinMax, arabicMin)
	}
	if !strings.Contains(visual(l), "Olive oil") {
		t.Fatalf("the Latin reads left to right: %q", visual(l))
	}
	en := ts.Layout("Olive oil 3.25 USD", body, typeset.LTR)
	if visual(en) != "Olive oil 3.25 USD" || en.Width <= 0 || en.Ascent <= 0 || en.Descent <= 0 {
		t.Fatalf("a left-to-right line: %q %+v", visual(en), en)
	}
}

func TestWrappingKeepsWordsWhole(t *testing.T) {
	ts := setter(t)
	text := "المتجر يحتفظ بنسخة احتياطية كل يوم وعند الإغلاق وقبل كل تحديث لقاعدة البيانات"
	lines := ts.Wrap(text, body, typeset.RTL, 200)
	if len(lines) < 3 {
		t.Fatalf("%d lines", len(lines))
	}
	words := strings.Fields(text)
	var got []string
	for _, l := range lines {
		if l.Width > 200.5 {
			t.Fatalf("a line of %.1f over 200", l.Width)
		}
		var logical []rune
		for i := len(l.Glyphs) - 1; i >= 0; i-- { // right to left is logical order in an Arabic line
			logical = append(logical, l.Glyphs[i].Runes...)
		}
		got = append(got, strings.Fields(string(logical))...)
	}
	if strings.Join(got, " ") != strings.Join(words, " ") {
		t.Fatalf("words broken or lost:\n%q\n%q", strings.Join(got, " "), strings.Join(words, " "))
	}
	if ts.Wrap("", body, typeset.RTL, 100) != nil {
		t.Fatal("nothing to wrap")
	}
}

func TestDrawingMarksOnlyInsideTheLine(t *testing.T) {
	ts := setter(t)
	l := ts.Layout("بقالية المونة", typeset.Style{Size: 34, Weight: typeset.Bold}, typeset.RTL)
	img := image.NewAlpha(image.Rect(0, 0, 400, 60))
	ts.Draw(img, l, 10, 44)
	inked, outside := 0, 0
	for y := 0; y < 60; y++ {
		for x := 0; x < 400; x++ {
			if img.AlphaAt(x, y).A > 128 {
				inked++
				if float32(x) < 8 || float32(x) > 12+l.Width {
					outside++
				}
			}
		}
	}
	if inked < 300 || outside > 0 {
		t.Fatalf("%d pixels inked, %d outside the line's box", inked, outside)
	}
	ts.Draw(img, typeset.Line{}, 0, 0)
	ts.Draw(image.NewAlpha(image.Rect(0, 0, 10, 10)), l, 500, 500) // entirely off the canvas
	if len(typeset.License()) < 1000 || len(ts.FontData(typeset.Bold)) < 100_000 || ts.Advance(typeset.Regular, l.Glyphs[0].GID) <= 0 ||
		ts.Metrics(typeset.Regular).Ascent <= 0 {
		t.Fatal("font data, licence and metrics")
	}
}
