package documents_test

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/documents"
	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

// logo is a square half black and half transparent: what a shop's logo on a clear background looks like.
func logo() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 40, 40))
	for y := range 40 {
		for x := range 40 {
			if x < 20 {
				img.Set(x, y, color.NRGBA{A: 255})
			} else {
				img.Set(x, y, color.NRGBA{}) // fully transparent: the paper
			}
		}
	}
	return img
}

// gridInvoice is an invoice's table: seven columns, and footer rows that span them (0.10.0).
func gridInvoice(dir documents.Direction) documents.Document {
	iso := typeset.Isolate
	return documents.Document{Direction: dir, Blocks: []documents.Block{
		documents.Image{Picture: logo(), MaxLines: 4},
		documents.Fields{
			Start: []documents.Field{{Label: "السيد:", Value: documents.T("رياض العمر")}, {Label: "رقم الفاتورة:", Value: documents.F("2970")}},
			End:   []documents.Field{{Label: "التاريخ:", Value: documents.F("31/08/2026")}},
		},
		documents.Table{Grid: true, Headings: []string{"الإجمالي", "الصنف", "الكمية", "الوحدة", "طرد", "ملاحظات", "السعر"},
			Widths: []int{12, 30, 10, 9, 7, 12, 10},
			Rows: [][]documents.Cell{
				{documents.F("108.00"), documents.T("فنجان قهوة"), documents.F("6"), documents.T("دستة"), documents.F("1.0"), documents.T(""), documents.F("18.00")},
			},
			Footer: [][]documents.Cell{
				{{Text: "عدد الطرود", Span: 4}, documents.F("1.0"), {Text: "", Span: 2}},
				{{Text: "فقط مائة وثمانية دولارات أمريكية لا غير", Span: 5}, documents.T("الصافي"), documents.F(iso("108.00"))},
			}},
	}}
}

func TestAGridInvoiceRastersOnBothPapers(t *testing.T) {
	ts := setter(t)
	for _, dir := range []documents.Direction{documents.RTL, documents.LTR} {
		doc := gridInvoice(dir)
		if bad := documents.Unisolated(doc); len(bad) > 0 {
			t.Fatalf("unisolated figures: %q", bad)
		}
		thermal := documents.Raster(ts, doc, documents.Receipt80)
		if thermal.Bounds().Dx() != 576 || thermal.Bounds().Dy() < 200 {
			t.Fatalf("thermal bitmap %v", thermal.Bounds())
		}
		pages := documents.RasterPages(ts, doc, documents.A4.Scaled(150.0/72))
		if len(pages) != 1 {
			t.Fatalf("%d A4 pages for a one-line invoice", len(pages))
		}
		if b := pages[0].Bounds(); b.Dx() != 1240 || b.Dy() != 1754 {
			t.Fatalf("an A4 page at 150 dpi is %v, want 1240×1754", b)
		}
	}
}

// TestASpannedFooterCellCoversItsColumns: a grid draws a vertical rule at every cell's edge and none inside a span — the
// amount in words runs under five columns without a line through it.
func TestASpannedFooterCellCoversItsColumns(t *testing.T) {
	ts := setter(t)
	page := documents.RasterPages(ts, gridInvoice(documents.RTL), documents.A4.Scaled(150.0/72))[0]
	// Count the vertical rules crossing a row: scan across the middle of the last footer row, found as the lowest band of
	// ink with rules on both of the page's content edges.
	rulesAcross := func(y int) int {
		n, inRule := 0, false
		for x := 0; x < page.Bounds().Dx(); x++ {
			dark := page.GrayAt(x, y).Y == 0
			vertical := dark && page.GrayAt(x, y-3).Y == 0 && page.GrayAt(x, y+3).Y == 0
			if vertical && !inRule {
				n++
			}
			inRule = vertical
		}
		return n
	}
	// The body row has seven cells: eight edges. Find a y where eight vertical rules cross, and one lower where the
	// amount-in-words row has four edges: its three cells.
	var body, words bool
	for y := 10; y < page.Bounds().Dy()-10; y++ {
		switch rulesAcross(y) {
		case 8:
			body = true
		case 4:
			if body {
				words = true
			}
		}
	}
	if !body || !words {
		t.Fatalf("grid rules: a seven-cell row found %v, a three-cell footer row after it %v", body, words)
	}
}

// TestALogoPrintsWhereItIsDarkAndNowhereElse: the black half inks the thermal paper, the transparent half leaves it white.
func TestALogoPrintsWhereItIsDarkAndNowhereElse(t *testing.T) {
	ts := setter(t)
	img := documents.Raster(ts, documents.Document{Direction: documents.RTL, Blocks: []documents.Block{
		documents.Image{Picture: logo(), MaxLines: 4},
	}}, documents.Receipt80)
	// The picture is centred: its left half is dark, its right half is paper.
	mid := img.Bounds().Dx() / 2
	row := documents.Receipt80.Margin + 20
	dark, white := 0, 0
	for x := mid - 40; x < mid-5; x++ {
		if img.GrayAt(x, int(row)).Y == 0 {
			dark++
		}
	}
	for x := mid + 5; x < mid+40; x++ {
		if img.GrayAt(x, int(row)).Y == 255 {
			white++
		}
	}
	if dark < 30 || white < 30 {
		t.Fatalf("the logo's black half inked %d of 35 dots and its clear half left %d of 35 white", dark, white)
	}
}

// TestThePDFCarriesTheLogo: an A4 PDF embeds the picture as an image and draws it.
func TestThePDFCarriesTheLogo(t *testing.T) {
	pdf := documents.PDF(setter(t), gridInvoice(documents.RTL))
	if !bytes.Contains(pdf, []byte("/Subtype /Image")) || !bytes.Contains(pdf, []byte("/XObject << /Im1")) {
		t.Fatal("the PDF has no image")
	}
	if !strings.HasPrefix(string(pdf), "%PDF-") {
		t.Fatal("not a PDF")
	}
}

func TestLuminanceCountsTransparencyAsPaper(t *testing.T) {
	for c, want := range map[color.Color]uint32{
		color.NRGBA{A: 255}:                         0,
		color.NRGBA{R: 255, G: 255, B: 255, A: 255}: 255,
		color.NRGBA{}:                               255,
	} {
		if got := documents.Luminance(c); got != want {
			t.Errorf("Luminance(%v) = %d, want %d", c, got, want)
		}
	}
}

// TestAMidToneLogoIsDitheredNotBlackened: a dark blue mark inks most of its dots but not all — a share, as a grey is —
// where a threshold made it a solid slab.
func TestAMidToneLogoIsDitheredNotBlackened(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.SetNRGBA(x, y, color.NRGBA{R: 90, G: 120, B: 160, A: 255}) // a mid blue, luminance about 116
		}
	}
	raster := documents.Raster(setter(t), documents.Document{Blocks: []documents.Block{documents.Image{Picture: img, MaxLines: 4}}}, documents.Receipt80)
	inked, total := 0, 0
	b := raster.Bounds()
	for y := b.Min.Y + 20; y < b.Min.Y+100; y++ {
		for x := b.Dx()/2 - 40; x < b.Dx()/2+40; x++ {
			total++
			if raster.GrayAt(x, y).Y == 0 {
				inked++
			}
		}
	}
	if share := inked * 100 / total; share < 30 || share > 70 {
		t.Fatalf("a mid-tone logo inked %d%% of its dots, want a share near half", share)
	}
}
