package documents

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

// PDF renders a document as A4 pages with the embedded font (D-L7.4): each weight a CIDFontType2 with Identity-H, glyph
// widths, and a text map that skips bidi controls, so copied text reads without invisible marks (L7 H4).
func PDF(ts *typeset.Typesetter, doc Document) []byte {
	lo := layout(ts, doc, A4)
	used := [2]map[uint16][]rune{{}, {}}
	contents := make([]string, len(lo.pages))
	for i, p := range lo.pages {
		var b strings.Builder
		for _, o := range p.ops {
			switch o.kind {
			case opText:
				// Drawn in logical order: where a glyph lands is its position, and a reader that copies text in the order
				// it was written then reads Arabic from its first letter.
				glyphs := append([]typeset.Glyph(nil), o.line.Glyphs...)
				sort.SliceStable(glyphs, func(i, j int) bool { return glyphs[i].Cluster < glyphs[j].Cluster })
				for _, g := range glyphs {
					if !usedAlready(used[g.Weight], g) {
						used[g.Weight][g.GID] = visibleRunes(g.Runes)
					}
					fmt.Fprintf(&b, "BT /F%d %s Tf 1 0 0 1 %s %s Tm <%04X> Tj ET\n", g.Weight+1, num(o.line.Style.Size),
						num(o.x+g.X), num(A4.Height-(o.y+g.Y)), g.GID)
				}
			case opLine:
				dash := "[] 0 d"
				if o.dashed {
					dash = "[2 2] 0 d"
				}
				fmt.Fprintf(&b, "%s %s w %s %s m %s %s l S\n", dash, num(o.width), num(o.x), num(A4.Height-o.y), num(o.x2), num(A4.Height-o.y2))
			case opRect:
				fmt.Fprintf(&b, "[] 0 d %s w %s %s %s %s re S\n", num(o.width), num(o.x), num(A4.Height-o.y-o.y2), num(o.x2), num(o.y2))
			}
		}
		contents[i] = b.String()
	}

	w := &pdfWriter{}
	w.reserve(2) // 1 catalog, 2 pages
	var fontRefs [2]int
	for weight := range 2 {
		if len(used[weight]) == 0 && weight == 1 {
			continue
		}
		fontRefs[weight] = w.font(ts, typeset.Weight(weight), used[weight])
	}
	resources := "/Font << /F1 " + ref(fontRefs[0])
	if fontRefs[1] != 0 {
		resources += " /F2 " + ref(fontRefs[1])
	}
	resources += " >>"
	var kids []string
	for _, c := range contents {
		content := w.stream("", []byte(c))
		kids = append(kids, ref(w.add(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources << %s >> /Contents %s >>",
			num(A4.Width), num(A4.Height), resources, ref(content)))))
	}
	w.set(1, "<< /Type /Catalog /Pages 2 0 R >>")
	w.set(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(kids)))
	info := w.add(fmt.Sprintf("<< /Title %s /Author %s /CreationDate (%s) >>", pdfString(doc.Meta.Title), pdfString(doc.Meta.Author), doc.Meta.Created))
	return w.bytes(info)
}

// RasterPDF is a bitmap as a PDF page of pageWidth points, the image widthPoints wide and centred — how a receipt reaches a
// printer through its driver (D-L7.9): the same bits the raw path prints.
func RasterPDF(img *image.Gray, pageWidth, imageWidth float32) []byte {
	b := img.Bounds()
	height := imageWidth * float32(b.Dy()) / float32(b.Dx())
	rowBytes := (b.Dx() + 7) / 8
	data := make([]byte, 0, rowBytes*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := make([]byte, rowBytes)
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.GrayAt(x, y).Y >= 128 { // 1 is white in DeviceGray
				row[(x-b.Min.X)/8] |= 0x80 >> ((x - b.Min.X) % 8)
			}
		}
		data = append(data, row...)
	}
	w := &pdfWriter{}
	w.reserve(2)
	xobject := w.stream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 1", b.Dx(), b.Dy()), data)
	content := w.stream("", []byte(fmt.Sprintf("q %s 0 0 %s %s 0 cm /Im1 Do Q", num(imageWidth), num(height), num((pageWidth-imageWidth)/2))))
	pageRef := w.add(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources << /XObject << /Im1 %s >> >> /Contents %s >>",
		num(pageWidth), num(height), ref(xobject), ref(content)))
	w.set(1, "<< /Type /Catalog /Pages 2 0 R >>")
	w.set(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count 1 >>", ref(pageRef)))
	return w.bytes(0)
}

func usedAlready(m map[uint16][]rune, g typeset.Glyph) bool {
	prev, ok := m[g.GID]
	return ok && (len(prev) > 0 || len(visibleRunes(g.Runes)) == 0)
}

func visibleRunes(rs []rune) []rune {
	out := make([]rune, 0, len(rs))
	for _, r := range rs {
		if !typeset.IsBidiControl(r) {
			out = append(out, r)
		}
	}
	return out
}

func num(f float32) string {
	s := fmt.Sprintf("%.2f", f)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func ref(n int) string { return fmt.Sprintf("%d 0 R", n) }

// pdfString is a text string in UTF-16BE with a byte-order mark, as PDF text strings may be.
func pdfString(s string) string {
	var b strings.Builder
	b.WriteString("<FEFF")
	for _, r := range s {
		for _, u := range utf16Units(r) {
			fmt.Fprintf(&b, "%04X", u)
		}
	}
	b.WriteString(">")
	return b.String()
}

func utf16Units(r rune) []uint16 {
	if r < 0x10000 {
		return []uint16{uint16(r)}
	}
	r -= 0x10000
	return []uint16{uint16(0xD800 + (r >> 10)), uint16(0xDC00 + (r & 0x3FF))}
}

type pdfWriter struct{ objs []string }

func (w *pdfWriter) reserve(n int) { w.objs = append(w.objs, make([]string, n)...) }

func (w *pdfWriter) set(n int, s string) { w.objs[n-1] = s }

func (w *pdfWriter) add(s string) int {
	w.objs = append(w.objs, s)
	return len(w.objs)
}

func (w *pdfWriter) stream(dict string, data []byte) int {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return w.add(fmt.Sprintf("<< %s /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", dict, z.Len(), z.String()))
}

func (w *pdfWriter) font(ts *typeset.Typesetter, weight typeset.Weight, used map[uint16][]rune) int {
	name := [2]string{"IBMPlexSansArabic", "IBMPlexSansArabic-Bold"}[weight]
	gids := make([]int, 0, len(used))
	for g := range used {
		gids = append(gids, int(g))
	}
	sort.Ints(gids)
	var widths, cmap strings.Builder
	entries := 0
	for _, g := range gids {
		fmt.Fprintf(&widths, "%d [%s] ", g, num(ts.Advance(weight, uint16(g))))
		if rs := used[uint16(g)]; len(rs) > 0 {
			var hex strings.Builder
			for _, r := range rs {
				for _, u := range utf16Units(r) {
					fmt.Fprintf(&hex, "%04X", u)
				}
			}
			fmt.Fprintf(&cmap, "<%04X> <%s>\n", g, hex.String())
			entries++
		}
	}
	toUnicode := "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def /CMapName /Adobe-Identity-UCS def /CMapType 2 def\n" +
		"1 begincodespacerange <0000> <FFFF> endcodespacerange\n" +
		fmt.Sprintf("%d beginbfchar\n", entries) + cmap.String() + "endbfchar\nendcmap CMapName currentdict /CMap defineresource pop end end"
	m := ts.Metrics(weight)
	file := w.stream(fmt.Sprintf("/Length1 %d", len(ts.FontData(weight))), ts.FontData(weight))
	descriptor := w.add(fmt.Sprintf("<< /Type /FontDescriptor /FontName /%s /Flags 4 /FontBBox [%s %s %s %s] /ItalicAngle 0 /Ascent %s /Descent %s /CapHeight %s /StemV 80 /FontFile2 %s >>",
		name, num(m.BBox[0]), num(m.BBox[1]), num(m.BBox[2]), num(m.BBox[3]), num(m.Ascent), num(m.Descent), num(m.CapHeight), ref(file)))
	cid := w.add(fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /%s /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor %s /CIDToGIDMap /Identity /W [%s] >>",
		name, ref(descriptor), widths.String()))
	uni := w.stream("", []byte(toUnicode))
	return w.add(fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H /DescendantFonts [%s] /ToUnicode %s >>", name, ref(cid), ref(uni)))
}

func (w *pdfWriter) bytes(info int) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(w.objs))
	for i, o := range w.objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(w.objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	trailer := fmt.Sprintf("/Size %d /Root 1 0 R", len(w.objs)+1)
	if info > 0 {
		trailer += " /Info " + ref(info)
	}
	fmt.Fprintf(&b, "trailer\n<< %s >>\nstartxref\n%d\n%%%%EOF\n", trailer, xref)
	return b.Bytes()
}
