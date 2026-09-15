package documents_test

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/documents"
	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

var update = flag.Bool("update", false, "rewrite the golden bitmaps")

func setter(t *testing.T) *typeset.Typesetter {
	t.Helper()
	ts, err := typeset.Default()
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

// receipt is a credit sale's receipt, as the binding layer builds one.
func receipt(dir documents.Direction) documents.Document {
	iso := typeset.Isolate
	if dir == documents.LTR {
		return documents.Document{Direction: dir, Blocks: []documents.Block{
			documents.Title{Text: "Al-Mouna Grocery", Subtitle: "Receipt No. " + iso("12") + " · " + iso("2026-09-14 10:32"), Center: true},
			documents.Rule{Dashed: true},
			documents.Paragraph{Text: "Local olive oil", Bold: true},
			documents.Pairs{Rows: []documents.Pair{{Label: iso("2.000") + " L × " + documents.Money("6.50", "USD"), Value: documents.Cell{Text: documents.Money("13.00", "USD")}}}},
			documents.Rule{Dashed: true},
			documents.Pairs{Rows: []documents.Pair{
				{Label: "Total", Value: documents.Cell{Text: documents.Money("76000", "SYP")}, Bold: true},
				{Label: "Paid now", Value: documents.Cell{Text: documents.Money("20000", "SYP")}},
			}},
			documents.Signature{Label: "Customer's signature"},
			documents.Paragraph{Text: "Thank you", Center: true},
		}}
	}
	return documents.Document{Direction: dir, Blocks: []documents.Block{
		documents.Title{Text: "بقالية المونة", Subtitle: "فاتورة رقم " + iso("12") + " · " + iso("2026-09-14 10:32"), Center: true},
		documents.Rule{Dashed: true},
		documents.Paragraph{Text: "زيت زيتون بلدي", Bold: true},
		documents.Pairs{Rows: []documents.Pair{{Label: iso("2.000") + " لتر × " + documents.Money("6.50", "دولار"), Value: documents.Cell{Text: documents.Money("13.00", "دولار")}}}},
		documents.Paragraph{Text: "لبنة بلدية", Bold: true},
		documents.Pairs{Rows: []documents.Pair{{Label: iso("0.750") + " كغ × " + documents.Money("60000", "ل.س"), Value: documents.Cell{Text: documents.Money("45000", "ل.س")}}}},
		documents.Rule{Dashed: true},
		documents.Pairs{Rows: []documents.Pair{
			{Label: "المجموع", Value: documents.Cell{Text: documents.Money("76000", "ل.س")}, Bold: true},
			{Label: "المدفوع الآن", Value: documents.Cell{Text: documents.Money("20000", "ل.س")}},
			{Label: "أُضيف إلى دين أبو محمد - الحلاق", Value: documents.Cell{Text: documents.Money("56000", "ل.س")}},
		}},
		documents.Stamp{Text: "نسخة"},
		documents.Signature{Label: "توقيع الزبون"},
		documents.Paragraph{Text: "شكراً لزيارتكم", Center: true},
	}}
}

// golden compares a bitmap with its stored image, allowing a handful of pixels to differ: a rasteriser's antialiasing may
// round a boundary pixel differently on another processor, and that is not a different receipt.
func golden(t *testing.T, name string, img *image.Gray) {
	t.Helper()
	path := filepath.Join("testdata", name+".png")
	if *update {
		if err := os.WriteFile(path, documents.PNG(img), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v — run go test ./internal/lite/documents/ -update, and look at the image before committing it", err)
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	want, ok := decoded.(*image.Gray)
	if !ok || want.Bounds() != img.Bounds() {
		t.Fatalf("%s: size %v, golden %v", name, img.Bounds(), decoded.Bounds())
	}
	differ := 0
	for i := range img.Pix {
		if img.Pix[i] != want.Pix[i] {
			differ++
		}
	}
	if limit := len(img.Pix) / 1000; differ > limit {
		_ = os.WriteFile(filepath.Join(t.TempDir(), name+".png"), documents.PNG(img), 0o644)
		t.Fatalf("%s: %d pixels differ from the golden (limit %d)", name, differ, limit)
	}
}

func TestAReceiptRastersToItsGolden(t *testing.T) {
	ts := setter(t)
	for _, c := range []struct {
		name  string
		dir   documents.Direction
		media documents.Media
	}{
		{"credit-receipt-ar-80", documents.RTL, documents.Receipt80},
		{"credit-receipt-en-80", documents.LTR, documents.Receipt80},
		{"credit-receipt-ar-58", documents.RTL, documents.Receipt58},
	} {
		img := documents.Raster(ts, receipt(c.dir), c.media)
		if img.Bounds().Dx() != int(c.media.Width) || img.Bounds().Dy() < 300 {
			t.Fatalf("%s: %v", c.name, img.Bounds())
		}
		for _, p := range img.Pix {
			if p != 0 && p != 255 {
				t.Fatalf("%s: a pixel of %d — a thermal bitmap is black or white", c.name, p)
			}
		}
		golden(t, c.name, img)
	}
}

func TestEveryFigureIsIsolated(t *testing.T) {
	for _, dir := range []documents.Direction{documents.RTL, documents.LTR} {
		if bad := documents.Unisolated(receipt(dir)); len(bad) > 0 {
			t.Fatalf("figures not isolated: %q", bad)
		}
	}
	doc := documents.Document{Blocks: []documents.Block{
		documents.Paragraph{Text: "فاتورة رقم 12"},
		documents.Pairs{Rows: []documents.Pair{{Label: "x", Value: documents.T("13.00")}}},
		documents.Table{Rows: [][]documents.Cell{{documents.F("1"), documents.T("سطر 2")}}},
	}}
	if bad := documents.Unisolated(doc); len(bad) != 3 {
		t.Fatalf("the checker missed a bare figure: %q", bad)
	}
}

func TestGroupAndMoney(t *testing.T) {
	for in, want := range map[string]string{"1710500": "1,710,500", "-1234.50": "-1,234.50", "0.00": "0.00", "999": "999", "12a": "12a", "": "", "-": "-", "1000000.5": "1,000,000.5"} {
		if got := documents.Group(in); got != want {
			t.Errorf("Group(%q) = %q, want %q", in, got, want)
		}
	}
	if documents.Money("76000", "ل.س") != "\u206676,000\u2069 ل.س" {
		t.Fatal(documents.Money("76000", "ل.س"))
	}
}

func TestEscposFramesTheBitmapInBands(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 576, 600))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	img.SetGray(0, 0, color.Gray{Y: 0})     // the first dot of the first row
	img.SetGray(575, 599, color.Gray{Y: 0}) // the last dot of the last row
	out := documents.ESCPOS(img, true)
	if !bytes.HasPrefix(out, []byte{0x1B, 0x40, 0x1D, 0x76, 0x30, 0x00, 72, 0, 255, 0}) {
		t.Fatalf("header % X", out[:10])
	}
	if out[10] != 0x80 {
		t.Fatalf("the first dot: %08b", out[10])
	}
	second := 2 + 8 + 72*255
	third := second + 8 + 72*255
	if !bytes.Equal(out[second:second+8], []byte{0x1D, 0x76, 0x30, 0x00, 72, 0, 255, 0}) || !bytes.Equal(out[third:third+8], []byte{0x1D, 0x76, 0x30, 0x00, 72, 0, 90, 0}) {
		t.Fatal("bands of 255, 255 and 90 rows")
	}
	lastRowEnd := third + 8 + 72*90
	if out[lastRowEnd-1] != 0x01 {
		t.Fatalf("the last dot: %08b", out[lastRowEnd-1])
	}
	if !bytes.Equal(out[lastRowEnd:], []byte{0x1B, 0x64, 0x04, 0x1D, 0x56, 0x42, 0x00, 0x1B, 0x70, 0x00, 0x19, 0xFA}) {
		t.Fatalf("feed, cut and drawer: % X", out[lastRowEnd:])
	}
	if bytes.Contains(documents.ESCPOS(img, false), []byte{0x1B, 0x70}) {
		t.Fatal("the drawer opened without being asked")
	}
}

// ─── a minimal PDF reader, for reading back what the writer wrote ──────────

type pdf struct {
	raw  []byte
	objs map[int]string
}

func readPDF(t *testing.T, raw []byte) pdf {
	t.Helper()
	if !bytes.HasPrefix(raw, []byte("%PDF-1.7")) || !bytes.HasSuffix(raw, []byte("%%EOF\n")) {
		t.Fatal("not a PDF")
	}
	start := regexp.MustCompile(`startxref\n(\d+)\n`).FindSubmatch(raw)
	xref, _ := strconv.Atoi(string(start[1]))
	table := string(raw[xref:])
	header := regexp.MustCompile(`^xref\n0 (\d+)\n`).FindStringSubmatch(table)
	if header == nil {
		t.Fatal("no xref at startxref")
	}
	count, _ := strconv.Atoi(header[1])
	lines := strings.Split(table, "\n")[3 : 3+count-1]
	p := pdf{raw: raw, objs: map[int]string{}}
	for i, line := range lines {
		off, _ := strconv.Atoi(line[:10])
		prefix := fmt.Sprintf("%d 0 obj\n", i+1)
		if !bytes.HasPrefix(raw[off:], []byte(prefix)) {
			t.Fatalf("object %d is not at its xref offset", i+1)
		}
		end := bytes.Index(raw[off:], []byte("\nendobj\n"))
		p.objs[i+1] = string(raw[off+len(prefix) : off+end])
	}
	return p
}

func (p pdf) stream(t *testing.T, n int) []byte {
	t.Helper()
	body := p.objs[n]
	i := strings.Index(body, "stream\n")
	j := strings.LastIndex(body, "\nendstream")
	r, err := zlib.NewReader(strings.NewReader(body[i+7 : j]))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(r)
	return out
}

// text is the page's text through each font's ToUnicode map, glyph by glyph, in drawing order.
func (p pdf) text(t *testing.T) []string {
	t.Helper()
	maps := map[string]map[string]string{}
	var pages []string
	for n, body := range p.objs {
		if strings.Contains(body, "/Subtype /Type0") {
			fontName := regexp.MustCompile(`/BaseFont /(\S+)`).FindStringSubmatch(body)[1]
			uni, _ := strconv.Atoi(regexp.MustCompile(`/ToUnicode (\d+) 0 R`).FindStringSubmatch(body)[1])
			m := map[string]string{}
			for _, e := range regexp.MustCompile(`<([0-9A-F]{4})> <([0-9A-F]+)>`).FindAllStringSubmatch(string(p.stream(t, uni)), -1) {
				var s []rune
				for k := 0; k+4 <= len(e[2]); k += 4 {
					v, _ := strconv.ParseUint(e[2][k:k+4], 16, 16)
					s = append(s, rune(v))
				}
				m[e[1]] = string(s)
			}
			maps[fontName] = m
			_ = n
		}
	}
	fontOf := map[string]string{"F1": "IBMPlexSansArabic", "F2": "IBMPlexSansArabic-Bold"}
	for n := 1; n <= len(p.objs); n++ {
		body := p.objs[n]
		if !strings.Contains(body, "/Type /Page ") {
			continue
		}
		content, _ := strconv.Atoi(regexp.MustCompile(`/Contents (\d+) 0 R`).FindStringSubmatch(body)[1])
		var b strings.Builder
		for _, g := range regexp.MustCompile(`/(F\d) [\d.]+ Tf [^<]*<([0-9A-F]{4})> Tj`).FindAllStringSubmatch(string(p.stream(t, content)), -1) {
			b.WriteString(maps[fontOf[g[1]]][g[2]])
		}
		pages = append(pages, b.String())
	}
	return pages
}

func report(rows int) documents.Document {
	var body [][]documents.Cell
	for i := range rows {
		body = append(body, []documents.Cell{documents.T(fmt.Sprintf("منتج %c", 'أ'+rune(i%20))), documents.F(documents.Group(strconv.Itoa(1000 * (i + 1))))})
	}
	return documents.Document{
		Direction: documents.RTL, Running: "بقالية المونة — تقرير المنتجات", Footer: "صفحة {page} من {pages}",
		Meta: documents.Meta{Title: "تقرير المنتجات", Author: "بقالية المونة", Created: "D:20260914103200"},
		Blocks: []documents.Block{
			documents.Title{Text: "تقرير المنتجات — " + typeset.Isolate("2026-09-14")},
			documents.Table{Headings: []string{"المنتج", "الإيراد"}, Widths: []int{3, 1}, Rows: body,
				Total: []documents.Cell{documents.T("المجموع"), documents.F("1,000")}},
		},
	}
}

func TestThePDFEmbedsItsFontAndReadsBack(t *testing.T) {
	ts := setter(t)
	p := readPDF(t, documents.PDF(ts, report(12)))
	fonts, files := 0, 0
	for _, body := range p.objs {
		if strings.Contains(body, "/Subtype /CIDFontType2") && strings.Contains(body, "/CIDToGIDMap /Identity") {
			fonts++
		}
		if strings.Contains(body, "/FontFile2") {
			files++
		}
	}
	if fonts != 2 || files != 2 {
		t.Fatalf("%d CID fonts, %d embedded files — both weights embedded", fonts, files)
	}
	pages := p.text(t)
	if len(pages) != 1 || !strings.Contains(pages[0], "المنتج") || !strings.Contains(pages[0], "12,000") {
		t.Fatalf("pages %q", pages)
	}
	if !strings.Contains(p.objs[len(p.objs)], "/Title <FEFF") {
		t.Fatal("document information")
	}
}

func TestPDFTextExtractsWithoutBidiControls(t *testing.T) {
	ts := setter(t)
	// A line that starts with an isolated figure: the isolate's mark is then the first text a shared blank glyph is met with.
	doc := report(3)
	doc.Blocks = append([]documents.Block{documents.Paragraph{Text: typeset.Isolate("12") + " " + typeset.Isolate("2026-09-14")}}, doc.Blocks...)
	for _, page := range readPDF(t, documents.PDF(ts, doc)).text(t) {
		for _, r := range page {
			if typeset.IsBidiControl(r) {
				t.Fatalf("a bidi control in the extracted text: %q", page)
			}
		}
		if !strings.Contains(page, "2026-09-14") {
			t.Fatalf("the date: %q", page)
		}
	}
}

func TestATableRepeatsItsHeadingOnEachPage(t *testing.T) {
	ts := setter(t)
	pages := readPDF(t, documents.PDF(ts, report(150))).text(t)
	if len(pages) < 3 {
		t.Fatalf("%d pages for 150 rows", len(pages))
	}
	for i, page := range pages {
		if !strings.Contains(page, "المنتج") || !strings.Contains(page, "الإيراد") {
			t.Fatalf("page %d has no heading", i+1)
		}
		if i > 0 && !strings.Contains(page, "تقرير المنتجات") {
			t.Fatalf("page %d has no running header", i+1)
		}
		if !strings.Contains(page, fmt.Sprintf("%d", len(pages))) {
			t.Fatalf("page %d has no page count", i+1)
		}
	}
	if !strings.Contains(pages[len(pages)-1], "المجموع") {
		t.Fatal("the total row")
	}
}

func TestARasterBecomesADriverPage(t *testing.T) {
	ts := setter(t)
	img := documents.Raster(ts, receipt(documents.RTL), documents.Receipt80)
	p := readPDF(t, documents.RasterPDF(img, 226.77, 204.09))
	var imageObj string
	for _, body := range p.objs {
		if strings.Contains(body, "/Subtype /Image") {
			imageObj = body
		}
	}
	if !strings.Contains(imageObj, fmt.Sprintf("/Width 576 /Height %d", img.Bounds().Dy())) || !strings.Contains(imageObj, "/BitsPerComponent 1") {
		t.Fatalf("image object: %.120s", imageObj)
	}
	sum := sha256.Sum256(documents.PNG(img))
	if sum == [32]byte{} {
		t.Fatal("unreachable")
	}
}
