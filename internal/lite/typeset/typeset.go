// Package typeset lays text out the way a printed page needs it: Arabic joined and ligated, Latin and figures in their own
// direction, lines wrapped and placed glyph by glyph, in the font Lite embeds (L7 §3.2).
//
// # Why a library, and why only here
//
// Arabic changes form with its neighbours, lam-alef is one glyph, and a figure inside right-to-left text runs left to right.
// A browser does this for the screen; for a PDF or a receipt bitmap made in Go it takes a shaper. This package is the one place
// that uses one — go-text/typesetting, a Go port of HarfBuzz — and archlint's lite-typeset-only rule keeps it that way
// (A-L7.3). Everything else draws what this package placed.
//
// # The font
//
// IBM Plex Sans Arabic, Regular and Bold, under the SIL Open Font License 1.1 (fonts/OFL.txt): Arabic and Latin in one family,
// so a printout looks the same on every machine whatever fonts it has.
package typeset

import (
	"bytes"
	"embed"
	"image"
	"sort"
	"sync"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// CodeFontUnreadable is a build whose embedded font does not parse: a packaging defect, never a shop's data.
const CodeFontUnreadable = "lite.typeset.font_unreadable"

//go:embed fonts/IBMPlexSansArabic-Regular.ttf fonts/IBMPlexSansArabic-Bold.ttf fonts/OFL.txt
var fonts embed.FS

// Weight is a face of the embedded family.
type Weight int

// The weights.
const (
	Regular Weight = iota
	Bold
)

// Direction is a paragraph's direction.
type Direction int

// The directions. A document's paragraphs run in its language's direction; figures inside are isolated (Isolate).
const (
	RTL Direction = iota
	LTR
)

// Style is how a run of text is set. Size is in the output's unit — pixels for a bitmap, points for a PDF.
type Style struct {
	Size   float32
	Weight Weight
}

// Glyph is one glyph placed on a line: its font glyph id, its position from the line's left edge and baseline (Y down), and
// the text it stands for — what a PDF's text map says, bidi controls included (the PDF writer skips them).
type Glyph struct {
	Weight Weight
	GID    uint16
	X, Y   float32
	Runes  []rune
	// Cluster is the glyph's place in the text — its logical order, which a PDF writes glyphs in so copied text reads.
	Cluster int
}

// Line is a laid-out line, glyphs left to right in visual order.
type Line struct {
	Glyphs  []Glyph
	Width   float32
	Ascent  float32
	Descent float32
	Style   Style
}

const (
	lri = '⁦' // left-to-right isolate
	pdi = '⁩' // pop directional isolate
)

// Isolate marks a figure — a date, an amount, a quantity, a number — as left to right inside any paragraph, as the screen's
// <bdi dir="ltr"> does (D-L7.3). Without it an Arabic line prints 2026-09-14 as 14-09-2026 (L7 §1.2 H3).
func Isolate(s string) string {
	if s == "" {
		return s
	}
	return string(lri) + s + string(pdi)
}

// IsBidiControl reports the invisible direction marks, which a PDF's text map must not carry (L7 H4).
func IsBidiControl(r rune) bool {
	switch {
	case r >= '⁦' && r <= '⁩', r >= '‪' && r <= '‮', r == '‎', r == '‏', r == '؜':
		return true
	}
	return false
}

// Typesetter shapes and places text. Safe for concurrent use: the shaper keeps state, so calls are serialised.
type Typesetter struct {
	mu      sync.Mutex
	faces   [2]*font.Face
	data    [2][]byte
	shaper  shaping.HarfbuzzShaper
	seg     shaping.Segmenter
	wrapper shaping.LineWrapper
}

var (
	shared     *Typesetter
	sharedErr  error
	sharedOnce sync.Once
)

// Default is the process's typesetter, parsed once from the embedded fonts.
func Default() (*Typesetter, error) {
	sharedOnce.Do(func() { shared, sharedErr = New() })
	return shared, sharedErr
}

// New parses the embedded fonts.
func New() (*Typesetter, error) {
	t := &Typesetter{}
	for w, name := range []string{"fonts/IBMPlexSansArabic-Regular.ttf", "fonts/IBMPlexSansArabic-Bold.ttf"} {
		raw, err := fonts.ReadFile(name)
		if err != nil {
			return nil, errs.Wrap(err, errs.CategoryInternal, CodeFontUnreadable, "reading "+name)
		}
		face, err := font.ParseTTF(bytes.NewReader(raw))
		if err != nil {
			return nil, errs.Wrap(err, errs.CategoryInternal, CodeFontUnreadable, "parsing "+name)
		}
		t.faces[w], t.data[w] = face, raw
	}
	return t, nil
}

// License is the embedded font's licence text, which ships beside the application.
func License() []byte {
	raw, _ := fonts.ReadFile("fonts/OFL.txt")
	return raw
}

type oneFace struct{ f *font.Face }

func (m oneFace) ResolveFace(rune) *font.Face { return m.f }

func (t *Typesetter) input(runes []rune, st Style, dir Direction) shaping.Input {
	in := shaping.Input{Text: runes, RunEnd: len(runes), Face: t.faces[st.Weight], Size: fixed.Int26_6(st.Size * 64),
		Direction: di.DirectionRTL, Script: language.Arabic, Language: language.NewLanguage("ar")}
	if dir == LTR {
		in.Direction, in.Script, in.Language = di.DirectionLTR, language.Latin, language.NewLanguage("en")
	}
	return in
}

func wrapDirection(dir Direction) di.Direction {
	if dir == LTR {
		return di.DirectionLTR
	}
	return di.DirectionRTL
}

// Layout sets one line, however long.
func (t *Typesetter) Layout(text string, st Style, dir Direction) Line {
	lines := t.Wrap(text, st, dir, 1<<20)
	if len(lines) == 0 {
		return t.emptyLine(st)
	}
	out := lines[0]
	for _, l := range lines[1:] { // a paragraph wider than 2²⁰ units: keep it on one line anyway
		for _, g := range l.Glyphs {
			g.X += out.Width
			out.Glyphs = append(out.Glyphs, g)
		}
		out.Width += l.Width
	}
	return out
}

func (t *Typesetter) emptyLine(st Style) Line {
	ascent, descent := t.extents(st)
	return Line{Ascent: ascent, Descent: descent, Style: st}
}

func (t *Typesetter) extents(st Style) (float32, float32) {
	ext, _ := t.faces[st.Weight].FontHExtents()
	scale := st.Size / float32(t.faces[st.Weight].Upem())
	return ext.Ascender * scale, -ext.Descender * scale
}

// Wrap sets a paragraph into lines no wider than maxWidth, breaking between words.
func (t *Typesetter) Wrap(text string, st Style, dir Direction, maxWidth float32) []Line {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	face := t.faces[st.Weight]
	var outs []shaping.Output
	for _, run := range t.seg.Split(t.input(runes, st, dir), oneFace{face}) {
		outs = append(outs, t.shaper.Shape(run))
	}
	wrapped, _ := t.wrapper.WrapParagraphF(shaping.WrapConfig{Direction: wrapDirection(dir)}, fixed.Int26_6(maxWidth*64), runes,
		shaping.NewSliceIterator(outs))
	ascent, descent := t.extents(st)
	lines := make([]Line, 0, len(wrapped))
	for _, runs := range wrapped {
		sorted := append(shaping.Line(nil), runs...)
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].VisualIndex < sorted[j].VisualIndex })
		line := Line{Ascent: ascent, Descent: descent, Style: st}
		var x float32
		for _, run := range sorted {
			for _, g := range run.Glyphs {
				end := min(g.TextIndex()+g.RunesCount(), len(runes))
				start := min(g.TextIndex(), end)
				line.Glyphs = append(line.Glyphs, Glyph{Weight: st.Weight, GID: uint16(g.GlyphID), X: x + float32(g.XOffset)/64,
					Y: -float32(g.YOffset) / 64, Runes: runes[start:end], Cluster: g.TextIndex()})
				x += float32(g.Advance) / 64
			}
		}
		line.Width = x
		lines = append(lines, trimTrailingSpace(line, runes))
	}
	return lines
}

// trimTrailingSpace drops the width of a space a wrap leaves at a line's visual end, so an aligned line meets its edge.
func trimTrailingSpace(l Line, _ []rune) Line {
	for len(l.Glyphs) > 0 {
		last := l.Glyphs[len(l.Glyphs)-1]
		if !onlySpace(last.Runes) {
			break
		}
		l.Width = last.X
		l.Glyphs = l.Glyphs[:len(l.Glyphs)-1]
	}
	for len(l.Glyphs) > 0 && onlySpace(l.Glyphs[0].Runes) {
		shift := l.Glyphs[1:]
		if len(shift) == 0 {
			l.Glyphs, l.Width = nil, 0
			break
		}
		dx := shift[0].X
		for i := range shift {
			shift[i].X -= dx
		}
		l.Width -= dx
		l.Glyphs = shift
	}
	return l
}

func onlySpace(rs []rune) bool {
	if len(rs) == 0 {
		return false
	}
	for _, r := range rs {
		if r != ' ' && !IsBidiControl(r) {
			return false
		}
	}
	return true
}

// FontData is a weight's TrueType file, for embedding in a PDF.
func (t *Typesetter) FontData(w Weight) []byte { return t.data[w] }

// Metrics are a weight's design metrics in thousandths of an em, as a PDF font descriptor states them.
type Metrics struct {
	Ascent, Descent, CapHeight float32
	BBox                       [4]float32
}

// Metrics reports a weight's metrics.
func (t *Typesetter) Metrics(w Weight) Metrics {
	face := t.faces[w]
	scale := 1000 / float32(face.Upem())
	ext, _ := face.FontHExtents()
	return Metrics{Ascent: ext.Ascender * scale, Descent: ext.Descender * scale, CapHeight: face.LineMetric(font.CapHeight) * scale,
		BBox: [4]float32{-1000, -1000, 2000, 2000}}
}

// Advance is a glyph's advance in thousandths of an em.
func (t *Typesetter) Advance(w Weight, gid uint16) float32 {
	face := t.faces[w]
	return face.HorizontalAdvance(font.GID(gid)) * 1000 / float32(face.Upem())
}

// Draw rasterises a line onto a coverage mask with its left edge at x and its baseline at baseline.
func (t *Typesetter) Draw(dst *image.Alpha, line Line, x, baseline float32) {
	bounds := dst.Bounds()
	for _, g := range line.Glyphs {
		face := t.faces[g.Weight]
		outline, ok := face.GlyphData(font.GID(g.GID)).(font.GlyphOutline)
		if !ok || len(outline.Segments) == 0 {
			continue
		}
		scale := line.Style.Size / float32(face.Upem())
		ox, oy := x+g.X, baseline+g.Y
		minX, minY, maxX, maxY := float32(bounds.Max.X), float32(bounds.Max.Y), float32(bounds.Min.X), float32(bounds.Min.Y)
		pt := func(p ot.SegmentPoint) (float32, float32) {
			px, py := ox+p.X*scale, oy-p.Y*scale
			minX, maxX, minY, maxY = min(minX, px), max(maxX, px), min(minY, py), max(maxY, py)
			return px, py
		}
		// Bounds first, so a glyph is rasterised in its own small box rather than the whole page.
		for _, s := range outline.Segments {
			for _, a := range s.ArgsSlice() {
				pt(a)
			}
		}
		box := image.Rect(int(minX)-1, int(minY)-1, int(maxX)+2, int(maxY)+2).Intersect(bounds)
		if box.Empty() {
			continue
		}
		z := vector.NewRasterizer(box.Dx(), box.Dy())
		local := func(p ot.SegmentPoint) (float32, float32) {
			return ox + p.X*scale - float32(box.Min.X), oy - p.Y*scale - float32(box.Min.Y)
		}
		for _, s := range outline.Segments {
			switch s.Op {
			case ot.SegmentOpMoveTo:
				z.MoveTo(local(s.Args[0]))
			case ot.SegmentOpLineTo:
				z.LineTo(local(s.Args[0]))
			case ot.SegmentOpQuadTo:
				x1, y1 := local(s.Args[0])
				x2, y2 := local(s.Args[1])
				z.QuadTo(x1, y1, x2, y2)
			case ot.SegmentOpCubeTo:
				x1, y1 := local(s.Args[0])
				x2, y2 := local(s.Args[1])
				x3, y3 := local(s.Args[2])
				z.CubeTo(x1, y1, x2, y2, x3, y3)
			}
		}
		z.ClosePath()
		mask := image.NewAlpha(image.Rect(0, 0, box.Dx(), box.Dy()))
		z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
		for yy := 0; yy < box.Dy(); yy++ {
			for xx := 0; xx < box.Dx(); xx++ {
				a := mask.Pix[yy*mask.Stride+xx]
				i := dst.PixOffset(box.Min.X+xx, box.Min.Y+yy)
				if a > dst.Pix[i] {
					dst.Pix[i] = a
				}
			}
		}
	}
}
