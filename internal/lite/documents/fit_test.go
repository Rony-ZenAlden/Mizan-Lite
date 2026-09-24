package documents

import (
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

// textOf is what a laid-out line says, bidi controls left out.
func textOf(ln typeset.Line) string {
	var b strings.Builder
	for _, g := range ln.Glyphs {
		for _, r := range g.Runes {
			if !typeset.IsBidiControl(r) {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// crossings lists every line of text that runs across one of a page's vertical rules — a figure spilling out of its
// cell into the next, or out of the table.
func crossings(pages []page) []string {
	const eps = 0.01
	var out []string
	for n, p := range pages {
		var rules []op
		for _, o := range p.ops {
			if o.kind == opLine && o.x == o.x2 {
				rules = append(rules, o)
			}
		}
		for _, o := range p.ops {
			if o.kind != opText {
				continue
			}
			left, right := o.x, o.x+o.line.Width
			for _, r := range rules {
				if o.y < r.y || o.y > r.y2 {
					continue // the rule is another row's
				}
				if left < r.x-eps && right > r.x+eps {
					out = append(out, "page "+itoa(n+1)+": "+textOf(o.line))
				}
			}
		}
	}
	return out
}

// TestNoFigureCrossesItsCell (0.10.1, the owner's report of 2026-09-24): on an invoice the line's total ran out of its
// column. A cell's text now stays inside its rules whatever it holds — a dual reading, a figure far wider than any
// column, an amount with its currency's name — in either direction, at a PDF's points and at a printer's dots.
func TestNoFigureCrossesItsCell(t *testing.T) {
	ts, err := typeset.Default()
	if err != nil {
		t.Fatal(err)
	}
	wide := []string{"13,038.75 (1,303,875)", "123,456,789,012.34", "1,874,000", "18,740 (1,874,000)", "98,765,432 (9,876,543,200)"}
	rows := make([][]Cell, 0, len(wide))
	for _, w := range wide {
		rows = append(rows, []Cell{F(w), T("كنبة زاوية مع طاولة وسط"), F("12"), T("قطعة"), F("2.5"), T("حسم 5٪"), M(Money(w, "ل.س"))})
	}
	for _, dir := range []Direction{RTL, LTR} {
		doc := Document{Direction: dir, Blocks: []Block{Table{Grid: true, Widths: []int{16, 25, 9, 9, 7, 11, 17},
			Headings: []string{"الإجمالي", "الصنف", "الكمية", "الوحدة", "طرد", "ملاحظات", "السعر"}, Rows: rows,
			Footer: [][]Cell{
				{{Text: "مجموع إجمالي الفاتورة", Span: 5}, {Text: Money("98,765,432 (9,876,543,200)", "ل.س"), End: true, Span: 2}},
				{{Text: "فقط ثمانية وتسعون مليوناً لا غير", Span: 5}, T("الصافي"), {Text: Money("98,765,432 (9,876,543,200)", "ل.س"), End: true}},
			}}}}
		for _, media := range []Media{A4, A4.Scaled(110.0 / 72), A4.Scaled(200.0 / 72)} {
			out := layout(ts, doc, media)
			if bad := crossings(out.pages); len(bad) > 0 {
				t.Errorf("direction %d at %.0f wide: text crosses a rule: %q", dir, media.Width, bad)
			}
		}
	}

	// A figure that fits its column keeps its size: the guarantee shrinks only what would have spilled.
	doc := Document{Direction: RTL, Blocks: []Block{Table{Grid: true, Widths: []int{16, 25, 9, 9, 7, 11, 17},
		Headings: []string{"الإجمالي", "الصنف", "الكمية", "الوحدة", "طرد", "ملاحظات", "السعر"},
		Rows:     [][]Cell{{F("1,303,875"), T("كنبة"), F("1"), T("قطعة"), F(""), T(""), F("1,303,875")}}}}}
	sizes := map[float32]bool{}
	for _, o := range layout(ts, doc, A4).pages[0].ops {
		if o.kind == opText && textOf(o.line) == "1,303,875" {
			sizes[o.line.Style.Size] = true
		}
	}
	if len(sizes) != 1 || !sizes[A4.Body] {
		t.Fatalf("a seven-digit total that fits its column was set at %v, want the body size %v", sizes, A4.Body)
	}
}

// TestADualTooWideBreaksAtItsBracket: before anything is set smaller, a dual reading too wide for its column puts the old
// figure under the new one — both at full size when each fits.
func TestADualTooWideBreaksAtItsBracket(t *testing.T) {
	ts, err := typeset.Default()
	if err != nil {
		t.Fatal(err)
	}
	doc := Document{Direction: RTL, Blocks: []Block{Table{Grid: true, Widths: []int{16, 25, 9, 9, 7, 11, 17},
		Headings: []string{"الإجمالي", "الصنف", "الكمية", "الوحدة", "طرد", "ملاحظات", "السعر"},
		Rows:     [][]Cell{{F("13,038.75 (1,303,875)"), T("كنبة"), F("1"), T("قطعة"), F(""), T(""), F("95.00")}}}}}
	var fresh, legacy *op
	ops := layout(ts, doc, A4).pages[0].ops
	for i, o := range ops {
		switch textOf(o.line) {
		case "13,038.75":
			fresh = &ops[i]
		case "(1,303,875)":
			legacy = &ops[i]
		}
	}
	if fresh == nil || legacy == nil {
		t.Fatal("the dual reading was not broken into its new and old figures")
	}
	if legacy.y <= fresh.y || fresh.line.Style.Size != A4.Body || legacy.line.Style.Size != A4.Body {
		t.Fatalf("new figure at y %.1f size %.1f, old at y %.1f size %.1f: want the old under the new, both at %.1f",
			fresh.y, fresh.line.Style.Size, legacy.y, legacy.line.Style.Size, A4.Body)
	}
}
