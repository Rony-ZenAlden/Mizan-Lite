package documents

import (
	"image"
	"strings"

	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

// Media is a page's size and type sizes, in the output's unit (points for a PDF, dots for a bitmap).
type Media struct {
	Width, Height float32 // Height 0 is endless paper
	// PaperPoints and PrintPoints are a receipt roll's width and its printed width, for the driver path's page.
	PaperPoints, PrintPoints float32
	Margin                   float32
	Body, Small              float32
	Title, Head              float32
	Padding                  float32
	// Gutter is a table cell's inset on each side; 0 uses Padding. A4 needs more than its vertical padding, or a figure
	// aligned to its column's end meets the next column's text.
	Gutter float32
	Paged  bool
}

func (m Media) gutter() float32 {
	if m.Gutter == 0 {
		return m.Padding
	}
	return m.Gutter
}

// A4 is an A4 portrait page in points with 15 mm margins (Q-L7.7).
var A4 = Media{Width: 595.28, Height: 841.89, Margin: 42.52, Body: 10, Small: 8, Title: 16, Head: 12, Padding: 3, Gutter: 5, Paged: true}

// Scaled is the media in another unit: A4's points as a printer's or a preview's dots (0.10.0). A point is 1/72 inch,
// so a page at d dots per inch is Scaled(d/72).
func (m Media) Scaled(f float32) Media {
	m.Width *= f
	m.Height *= f
	m.Margin *= f
	m.Body *= f
	m.Small *= f
	m.Title *= f
	m.Head *= f
	m.Padding *= f
	m.Gutter *= f
	return m
}

// Receipt80 is 80 mm paper at 203 dpi: 576 dots across its 72 mm printable width.
var Receipt80 = Media{Width: 576, PaperPoints: 226.77, PrintPoints: 204.09, Margin: 8, Body: 22, Small: 19, Title: 34, Head: 25, Padding: 5}

// Receipt58 is 58 mm paper at 203 dpi: 384 dots.
var Receipt58 = Media{Width: 384, PaperPoints: 164.41, PrintPoints: 136.06, Margin: 6, Body: 19, Small: 16, Title: 28, Head: 21, Padding: 4}

type opKind int

const (
	opText opKind = iota
	opLine
	opRect
	// opFill is a solid rectangle. A barcode is drawn as a row of them.
	opFill
	// opImage is a picture drawn into a rectangle: the shop's logo.
	opImage
)

// op is a drawing instruction on a page, y measured down from the top.
type op struct {
	kind   opKind
	line   typeset.Line
	x, y   float32 // text: left edge and baseline; line: start; rect: top-left
	x2, y2 float32 // line: end; rect: width and height
	dashed bool
	width  float32
	pic    image.Image
}

type page struct{ ops []op }

// laidOut is a document placed on pages.
type laidOut struct {
	pages  []page
	height float32 // endless paper: the content's height
	media  Media
}

type layouter struct {
	ts    *typeset.Typesetter
	m     Media
	dir   Direction
	doc   Document
	pages []page
	y     float32
	// repeat is a table heading to repeat on a new page.
	repeat func()
}

func (l *layouter) cur() *page { return &l.pages[len(l.pages)-1] }

func (l *layouter) contentWidth() float32 { return l.m.Width - 2*l.m.Margin }

func (l *layouter) top() float32 {
	if l.m.Paged {
		return l.m.Margin + l.m.Small*2
	}
	return l.m.Margin
}

func (l *layouter) bottom() float32 {
	if l.m.Paged {
		return l.m.Height - l.m.Margin - l.m.Small*2
	}
	return 1 << 30
}

func (l *layouter) newPage() {
	l.pages = append(l.pages, page{})
	l.y = l.top()
	if l.repeat != nil {
		l.repeat()
	}
}

// ensure starts a new page when h does not fit.
func (l *layouter) ensure(h float32) {
	if l.y+h > l.bottom() && l.y > l.top()+1 {
		l.newPage()
	}
}

func (l *layouter) style(size float32, bold bool) typeset.Style {
	st := typeset.Style{Size: size}
	if bold {
		st.Weight = typeset.Bold
	}
	return st
}

func lineHeight(ln typeset.Line) float32 { return (ln.Ascent + ln.Descent) * 1.15 }

// startX places a line at the paragraph's start edge within [left, left+width].
func (l *layouter) startX(ln typeset.Line, left, width float32) float32 {
	if l.dir == RTL {
		return left + width - ln.Width
	}
	return left
}

func (l *layouter) endX(ln typeset.Line, left, width float32) float32 {
	if l.dir == RTL {
		return left
	}
	return left + width - ln.Width
}

func (l *layouter) text(ln typeset.Line, x, baseline float32) {
	l.cur().ops = append(l.cur().ops, op{kind: opText, line: ln, x: x, y: baseline})
}

func (l *layouter) rule(y float32, dashed bool, width float32) {
	l.cur().ops = append(l.cur().ops, op{kind: opLine, x: l.m.Margin, y: y, x2: l.m.Width - l.m.Margin, y2: y, dashed: dashed, width: width})
}

func cellText(c Cell) string {
	if c.Figure {
		return typeset.Isolate(c.Text)
	}
	return c.Text
}

// paragraph sets wrapped lines, start-aligned or centred.
func (l *layouter) paragraph(text string, st typeset.Style, center bool) {
	for _, ln := range l.ts.Wrap(text, st, l.dir, l.contentWidth()) {
		h := lineHeight(ln)
		l.ensure(h)
		x := l.startX(ln, l.m.Margin, l.contentWidth())
		if center {
			x = l.m.Margin + (l.contentWidth()-ln.Width)/2
		}
		l.text(ln, x, l.y+ln.Ascent)
		l.y += h
	}
}

func (l *layouter) block(b Block) {
	m := l.m
	switch b := b.(type) {
	case Title:
		l.paragraph(b.Text, l.style(m.Title, true), b.Center)
		if b.Subtitle != "" {
			l.paragraph(b.Subtitle, l.style(m.Small, false), b.Center)
		}
		l.y += m.Padding
	case Heading:
		l.y += m.Padding * 2
		l.ensure(m.Head * 3)
		l.paragraph(b.Text, l.style(m.Head, true), false)
		l.y += m.Padding
	case Paragraph:
		size := m.Body
		if b.Small {
			size = m.Small
		}
		l.paragraph(b.Text, l.style(size, b.Bold), b.Center)
	case Rule:
		l.y += m.Padding
		l.ensure(m.Padding * 2)
		l.rule(l.y, b.Dashed, 0.5)
		l.y += m.Padding * 2
	case Space:
		l.y += m.Body * 1.3 * b.Lines
	case Pairs:
		for _, p := range b.Rows {
			l.pair(p)
		}
	case Table:
		l.table(b)
	case Stamp:
		st := l.style(m.Title, true)
		ln := l.ts.Layout(b.Text, st, l.dir)
		h := lineHeight(ln) + m.Padding*2
		l.ensure(h + m.Padding*2)
		l.y += m.Padding
		x := m.Margin + (l.contentWidth()-ln.Width)/2
		l.cur().ops = append(l.cur().ops, op{kind: opRect, x: x - m.Padding*3, y: l.y, x2: ln.Width + m.Padding*6, y2: h, width: 2})
		l.text(ln, x, l.y+m.Padding+ln.Ascent)
		l.y += h + m.Padding
	case Barcode:
		// A module is one dot on the thermal head and one point on A4 — the narrowest bar the device can draw, which
		// is what keeps the symbol scannable. The whole symbol is centred; if it is wider than the page it is scaled
		// down to fit, because a symbol running off the edge scans as a shorter code, not as a failure.
		height := m.Body * b.HeightLines
		l.ensure(height + m.Body)
		modules := 0
		for _, w := range b.Bars {
			modules += w
		}
		module := float32(1)
		if width := float32(modules) * module; width > l.contentWidth() {
			module = l.contentWidth() / float32(modules)
		}
		x := m.Margin + (l.contentWidth()-float32(modules)*module)/2
		for i, w := range b.Bars {
			if i%2 == 0 { // even indexes are bars, odd are spaces
				l.cur().ops = append(l.cur().ops, op{kind: opFill, x: x, y: l.y, x2: float32(w) * module, y2: height})
			}
			x += float32(w) * module
		}
		l.y += height
		if b.Text != "" {
			st := l.style(m.Small, false)
			ln := l.ts.Layout(b.Text, st, LTR) // a code is read left to right in every language
			l.text(ln, m.Margin+(l.contentWidth()-ln.Width)/2, l.y+ln.Ascent)
			l.y += lineHeight(ln)
		}
		l.y += m.Body * 0.3
	case Image:
		l.image(b)
	case Fields:
		l.fields(b)
	case Signature:
		l.y += m.Body * 2.5
		l.ensure(m.Body * 2)
		st := l.style(m.Small, false)
		ln := l.ts.Layout(b.Label, st, l.dir)
		l.text(ln, l.startX(ln, m.Margin, l.contentWidth()), l.y)
		lineStart, lineEnd := m.Margin, m.Margin+l.contentWidth()-ln.Width-m.Padding*2
		if l.dir == LTR {
			lineStart, lineEnd = m.Margin+ln.Width+m.Padding*2, m.Margin+l.contentWidth()
		}
		l.cur().ops = append(l.cur().ops, op{kind: opLine, x: lineStart, y: l.y, x2: lineEnd, y2: l.y, width: 0.5})
		l.y += m.Body
	}
}

// pair sets a label at the start edge and its value at the end edge, wrapping the label if they would meet.
func (l *layouter) pair(p Pair) {
	m := l.m
	st := l.style(m.Body, p.Bold)
	value := l.ts.Layout(cellText(p.Value), l.style(m.Body, p.Bold || p.Value.Bold), l.dir)
	labelWidth := l.contentWidth() - value.Width - m.Padding*3
	labels := l.ts.Wrap(p.Label, st, l.dir, max(labelWidth, l.contentWidth()/3))
	if len(labels) == 0 {
		labels = []typeset.Line{l.ts.Layout("", st, l.dir)}
	}
	h := float32(0)
	for _, ln := range labels {
		h += lineHeight(ln)
	}
	h = max(h, lineHeight(value))
	l.ensure(h)
	y := l.y
	for _, ln := range labels {
		l.text(ln, l.startX(ln, m.Margin, l.contentWidth()), y+ln.Ascent)
		y += lineHeight(ln)
	}
	l.text(value, l.endX(value, m.Margin, l.contentWidth()), l.y+value.Ascent)
	l.y += h
}

// columns returns each column's left edge and width, the first column at the start edge.
func (l *layouter) columns(widths []int, n int) ([]float32, []float32) {
	total := float32(0)
	w := make([]float32, n)
	for i := range n {
		w[i] = 10
		if i < len(widths) && widths[i] > 0 {
			w[i] = float32(widths[i])
		}
		total += w[i]
	}
	lefts := make([]float32, n)
	x := l.m.Margin
	if l.dir == RTL {
		x = l.m.Width - l.m.Margin
	}
	for i := range n {
		w[i] = w[i] / total * l.contentWidth()
		if l.dir == RTL {
			x -= w[i]
			lefts[i] = x
		} else {
			lefts[i] = x
			x += w[i]
		}
	}
	return lefts, w
}

// box is where a cell stands: its left edge and width, the sum of the columns it spans.
type box struct {
	left, width float32
	column      int
}

// boxes places a row's cells on the columns: each takes Span columns from where the last one ended.
func boxes(cells []Cell, lefts, widths []float32) []box {
	out := make([]box, 0, len(cells))
	col := 0
	for _, c := range cells {
		if col >= len(lefts) {
			break
		}
		span := max(c.Span, 1)
		end := min(col+span, len(lefts))
		b := box{left: lefts[col], column: col}
		for i := col; i < end; i++ {
			b.left = min(b.left, lefts[i])
			b.width += widths[i]
		}
		out = append(out, b)
		col = end
	}
	return out
}

// row sets one table row. grid rules each cell on all sides; top also draws the rule above the row, which a grid's
// heading needs and every other row takes from the row before it.
func (l *layouter) row(cells []Cell, lefts, widths []float32, bold bool, size float32, endAligned []bool, grid, top bool) {
	m := l.m
	type setCell struct {
		lines          []typeset.Line
		figure, center bool
	}
	placed := boxes(cells, lefts, widths)
	set := make([]setCell, len(placed))
	h := float32(0)
	for i, bx := range placed {
		c := cells[i]
		st := l.style(size, bold || c.Bold)
		inner := bx.width - m.gutter()*2
		switch {
		case c.Figure:
			set[i] = setCell{lines: fitted(l.figureLines(c, st, inner), inner), figure: true}
		case c.End || (bx.column < len(endAligned) && endAligned[bx.column]):
			set[i] = setCell{lines: fitted(l.ts.Wrap(c.Text, st, l.dir, inner), inner), figure: true}
		default:
			set[i] = setCell{lines: fitted(l.ts.Wrap(c.Text, st, l.dir, inner), inner)}
		}
		set[i].center = c.Center
		ch := float32(0)
		for _, ln := range set[i].lines {
			ch += lineHeight(ln)
		}
		if len(set[i].lines) == 0 {
			ch = size * 1.3
		}
		h = max(h, ch)
	}
	h += m.Padding * 2
	l.ensure(h)
	rowTop := l.y
	for i, sc := range set {
		bx := placed[i]
		y := l.y + m.Padding
		for _, ln := range sc.lines {
			left, width := bx.left+m.gutter(), bx.width-m.gutter()*2
			x := l.startX(ln, left, width)
			switch {
			case sc.center:
				x = left + (width-ln.Width)/2
			case sc.figure:
				x = l.endX(ln, left, width)
			}
			l.text(ln, x, y+ln.Ascent)
			y += lineHeight(ln)
		}
	}
	l.y += h
	if !grid {
		l.rule(l.y, false, 0.25)
		return
	}
	const gridLine = 0.6
	if top {
		l.rule(rowTop, false, gridLine)
	}
	l.rule(l.y, false, gridLine)
	for _, bx := range placed {
		for _, x := range []float32{bx.left, bx.left + bx.width} {
			l.cur().ops = append(l.cur().ops, op{kind: opLine, x: x, y: rowTop, x2: x, y2: l.y, width: gridLine})
		}
	}
}

// figureLines sets a figure cell on one line — or, a dual reading too wide for its column, the new figure over the old one
// in its brackets, as the screen sets the old figure apart (0.10.1).
func (l *layouter) figureLines(c Cell, st typeset.Style, inner float32) []typeset.Line {
	whole := l.ts.Layout(cellText(c), st, l.dir)
	if whole.Width <= inner {
		return []typeset.Line{whole}
	}
	if fresh, legacy, ok := splitDual(c.Text); ok {
		return []typeset.Line{l.ts.Layout(typeset.Isolate(fresh), st, l.dir), l.ts.Layout(typeset.Isolate("("+legacy+")"), st, l.dir)}
	}
	return []typeset.Line{whole}
}

// fitted returns lines that each stand inside width: a line wider is set smaller until it fits (0.10.1). Until then a figure
// wider than its column ran across the rule into the next one — the invoice total the owner reported on 2026-09-24. A
// figure is never cut, because a cut figure reads as another amount; the columns are sized for the figures a shop prints,
// and this is the guarantee for the one that is wider.
func fitted(lines []typeset.Line, width float32) []typeset.Line {
	for i, ln := range lines {
		if ln.Width > width && width > 0 {
			lines[i] = ln.Scaled(width / ln.Width)
		}
	}
	return lines
}

func (l *layouter) table(t Table) {
	n := len(t.Headings)
	for _, r := range append(append(append([][]Cell(nil), t.Rows...), t.Total), t.Footer...) {
		span := 0
		for _, c := range r {
			span += max(c.Span, 1)
		}
		n = max(n, span)
	}
	if n == 0 {
		return
	}
	lefts, widths := l.columns(t.Widths, n)
	// A heading over a column of figures stands where its figures do.
	figures := make([]bool, n)
	if len(t.Rows) > 0 {
		for i, bx := range boxes(t.Rows[0], lefts, widths) {
			c := t.Rows[0][i]
			figures[bx.column] = c.Figure || c.End
		}
	}
	heading := func() {
		if len(t.Headings) == 0 {
			return
		}
		cells := make([]Cell, len(t.Headings))
		for i, h := range t.Headings {
			// A grid's headings stand in the middle of their columns, as a printed invoice's do.
			cells[i] = Cell{Text: h, Center: t.Grid}
		}
		l.row(cells, lefts, widths, true, l.m.Small, figures, t.Grid, t.Grid)
	}
	heading()
	l.repeat = heading
	first := len(t.Headings) == 0
	for _, r := range t.Rows {
		l.row(r, lefts, widths, false, l.m.Body, nil, t.Grid, t.Grid && first)
		first = false
	}
	if t.Total != nil {
		l.row(t.Total, lefts, widths, true, l.m.Body, nil, t.Grid, t.Grid && first)
		first = false
	}
	for _, r := range t.Footer {
		l.row(r, lefts, widths, true, l.m.Body, nil, t.Grid, t.Grid && first)
		first = false
	}
	l.repeat = nil
	l.y += l.m.Padding
}

// image sets a picture centred, as tall as MaxLines body lines allow and never wider than the page.
func (l *layouter) image(b Image) {
	if b.Picture == nil {
		return
	}
	bounds := b.Picture.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return
	}
	m := l.m
	h := m.Body * 1.3 * max(b.MaxLines, 1)
	w := h * float32(bounds.Dx()) / float32(bounds.Dy())
	if w > l.contentWidth() {
		w = l.contentWidth()
		h = w * float32(bounds.Dy()) / float32(bounds.Dx())
	}
	l.ensure(h + m.Padding)
	x := m.Margin + (l.contentWidth()-w)/2
	l.cur().ops = append(l.cur().ops, op{kind: opImage, x: x, y: l.y, x2: w, y2: h, pic: b.Picture})
	l.y += h + m.Padding
}

// fields sets two columns of "label value" lines side by side, each column start-aligned in its half.
func (l *layouter) fields(b Fields) {
	m := l.m
	gap := m.Padding * 2
	half := (l.contentWidth() - gap*2) / 2
	startLeft, endLeft := m.Margin+l.contentWidth()-half, m.Margin
	if l.dir == LTR {
		startLeft, endLeft = m.Margin, m.Margin+l.contentWidth()-half
	}
	type setField struct {
		label  typeset.Line
		values []typeset.Line
		height float32
	}
	lay := func(f Field) setField {
		label := l.ts.Layout(f.Label, l.style(m.Body, true), l.dir)
		st := l.style(m.Body, f.Value.Bold)
		var values []typeset.Line
		if f.Value.Figure {
			values = []typeset.Line{l.ts.Layout(cellText(f.Value), st, l.dir)}
		} else {
			values = l.ts.Wrap(f.Value.Text, st, l.dir, max(half-label.Width-gap, half/3))
		}
		h := lineHeight(label)
		vh := float32(0)
		for _, v := range values {
			vh += lineHeight(v)
		}
		return setField{label: label, values: values, height: max(h, vh)}
	}
	draw := func(f setField, left, y float32) {
		x := left
		if l.dir == RTL {
			x = left + half - f.label.Width
		}
		l.text(f.label, x, y+f.label.Ascent)
		vy := y
		for _, v := range f.values {
			vx := left + f.label.Width + gap
			if l.dir == RTL {
				vx = left + half - f.label.Width - gap - v.Width
			}
			l.text(v, vx, vy+v.Ascent)
			vy += lineHeight(v)
		}
	}
	for i := range max(len(b.Start), len(b.End)) {
		var start, end *setField
		h := float32(0)
		if i < len(b.Start) {
			s := lay(b.Start[i])
			start, h = &s, max(h, s.height)
		}
		if i < len(b.End) {
			e := lay(b.End[i])
			end, h = &e, max(h, e.height)
		}
		l.ensure(h)
		if start != nil {
			draw(*start, startLeft, l.y)
		}
		if end != nil {
			draw(*end, endLeft, l.y)
		}
		l.y += h + m.Padding
	}
	l.y += m.Padding
}

// Layout places a document on media.
func layout(ts *typeset.Typesetter, doc Document, m Media) laidOut {
	l := &layouter{ts: ts, m: m, dir: doc.Direction, doc: doc}
	l.newPage()
	for _, b := range doc.Blocks {
		l.block(b)
	}
	out := laidOut{pages: l.pages, media: m, height: l.y + m.Margin}
	if m.Paged {
		for i := range out.pages {
			p := &out.pages[i]
			small := typeset.Style{Size: m.Small}
			if i > 0 && doc.Running != "" {
				ln := ts.Layout(doc.Running, small, doc.Direction)
				p.ops = append(p.ops, op{kind: opText, line: ln, x: l.startX(ln, m.Margin, l.contentWidth()), y: m.Margin + ln.Ascent})
			}
			if doc.Footer != "" {
				text := strings.NewReplacer("{page}", typeset.Isolate(itoa(i+1)), "{pages}", typeset.Isolate(itoa(len(out.pages)))).Replace(doc.Footer)
				ln := ts.Layout(text, small, doc.Direction)
				p.ops = append(p.ops, op{kind: opText, line: ln, x: m.Margin + (l.contentWidth()-ln.Width)/2, y: m.Height - m.Margin})
			}
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
