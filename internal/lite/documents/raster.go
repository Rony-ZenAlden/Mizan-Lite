package documents

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

// Raster renders a document on endless thermal paper as a gray image, every pixel black (0) or white (255): the bitmap a
// receipt printer prints (D-L7.8), and — as PNG — the preview the screen shows (D-L7.11).
func Raster(ts *typeset.Typesetter, doc Document, m Media) *image.Gray {
	m.Paged, m.Height = false, 0
	lo := layout(ts, doc, m)
	mask := image.NewAlpha(image.Rect(0, 0, int(m.Width), int(lo.height+0.5)))
	for _, p := range lo.pages {
		draw(ts, mask, p.ops)
	}
	return ink(mask)
}

// RasterPages renders a paged document — an A4 invoice at a printer's dots or a preview's — one bitmap per page, each the
// page's full size (0.10.0). m is paged media in dots: A4.Scaled(dpi / 72).
func RasterPages(ts *typeset.Typesetter, doc Document, m Media) []*image.Gray {
	m.Paged = true
	lo := layout(ts, doc, m)
	out := make([]*image.Gray, 0, len(lo.pages))
	for _, p := range lo.pages {
		mask := image.NewAlpha(image.Rect(0, 0, int(m.Width+0.5), int(m.Height+0.5)))
		draw(ts, mask, p.ops)
		out = append(out, ink(mask))
	}
	return out
}

// draw marks a page's ink on a mask, 255 where the paper is printed.
func draw(ts *typeset.Typesetter, mask *image.Alpha, ops []op) {
	width, height := mask.Bounds().Dx(), mask.Bounds().Dy()
	set := func(x, y int) { mask.Pix[mask.PixOffset(clamp(x, width), clamp(y, height))] = 255 }
	for _, o := range ops {
		switch o.kind {
		case opText:
			ts.Draw(mask, o.line, o.x, o.y)
		case opLine:
			thick := max(1, int(o.width*2+0.5))
			if o.x == o.x2 { // a grid's vertical rule
				for y := int(o.y); y <= int(o.y2); y++ {
					for t := range thick {
						set(int(o.x)+t-thick/2, y)
					}
				}
				continue
			}
			for x := int(o.x); x < int(o.x2); x++ {
				if o.dashed && (x/4)%2 == 1 {
					continue
				}
				for t := range thick {
					set(x, int(o.y)+t)
				}
			}
		case opFill:
			for x := int(o.x); x < int(o.x+o.x2); x++ {
				for y := int(o.y); y < int(o.y+o.y2); y++ {
					set(x, y)
				}
			}
		case opRect:
			x0, y0, x1, y1 := int(o.x), int(o.y), int(o.x+o.x2), int(o.y+o.y2)
			for t := range max(1, int(o.width)) {
				for x := x0; x <= x1; x++ {
					set(x, y0+t)
					set(x, y1-t)
				}
				for y := y0; y <= y1; y++ {
					set(x0+t, y)
					set(x1-t, y)
				}
			}
		case opImage:
			drawPicture(o, set)
		}
	}
}

// bayer is the 8×8 ordered-dither matrix: each cell a threshold, spread so that any grey inks the matching share of dots.
var bayer = [8][8]uint32{
	{0, 32, 8, 40, 2, 34, 10, 42}, {48, 16, 56, 24, 50, 18, 58, 26},
	{12, 44, 4, 36, 14, 46, 6, 38}, {60, 28, 52, 20, 62, 30, 54, 22},
	{3, 35, 11, 43, 1, 33, 9, 41}, {51, 19, 59, 27, 49, 17, 57, 25},
	{15, 47, 7, 39, 13, 45, 5, 37}, {63, 31, 55, 23, 61, 29, 53, 21},
}

// drawPicture scales a picture into its rectangle, nearest pixel, and inks it by ordered dithering — transparency
// counting as the paper it is printed on (0.10.0).
//
// Every raster here is black and white: a thermal head has one colour, and so does the page a Windows driver is handed.
// A hard threshold turned a coloured logo into a solid black shape — a dark blue mark and its lettering became one slab
// (the first invoice rendered, 2026-09-24). Dithering keeps a mid-tone as a share of dots, so the shape inside survives;
// a logo that is already black and white prints exactly as a threshold would print it.
func drawPicture(o op, set func(x, y int)) {
	b := o.pic.Bounds()
	tw, th := int(o.x2+0.5), int(o.y2+0.5)
	if tw <= 0 || th <= 0 {
		return
	}
	x0, y0 := int(o.x+0.5), int(o.y+0.5)
	for ty := range th {
		sy := b.Min.Y + ty*b.Dy()/th
		for tx := range tw {
			// A threshold from 2 to 254: pure black inks every dot, the white page none.
			threshold := bayer[ty%8][tx%8]*4 + 2
			if Luminance(o.pic.At(b.Min.X+tx*b.Dx()/tw, sy)) < threshold {
				set(x0+tx, y0+ty)
			}
		}
	}
}

// Luminance is a colour's brightness, 0 black to 255 white, over a white page.
func Luminance(c color.Color) uint32 {
	r, g, b, a := c.RGBA() // 16-bit, premultiplied
	white := 0xffff - a
	return (299*(r+white) + 587*(g+white) + 114*(b+white)) / 1000 >> 8
}

// ink turns a mask into the bitmap: 0 where printed, 255 where the paper is left white.
func ink(mask *image.Alpha) *image.Gray {
	out := image.NewGray(mask.Bounds())
	for i, a := range mask.Pix {
		if a >= 128 {
			out.Pix[i] = 0
		} else {
			out.Pix[i] = 255
		}
	}
	return out
}

func clamp(v, limit int) int { return min(max(v, 0), limit-1) }

// PNG encodes a bitmap.
func PNG(img *image.Gray) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// bandRows is the most rows one GS v 0 command carries: a long receipt is sent in bands, so no printer's buffer overflows.
const bandRows = 255

// ESCPOS is a bitmap as ESC/POS bytes: initialise, the image in GS v 0 bands, feed, partial cut — and, when drawer is true,
// the pulse that opens a cash drawer wired to the printer (Q-L7.11).
func ESCPOS(img *image.Gray, drawer bool) []byte {
	b := img.Bounds()
	w := b.Dx()
	rowBytes := (w + 7) / 8
	var out bytes.Buffer
	out.Write([]byte{0x1B, 0x40}) // ESC @
	for top := b.Min.Y; top < b.Max.Y; top += bandRows {
		rows := min(bandRows, b.Max.Y-top)
		out.Write([]byte{0x1D, 0x76, 0x30, 0x00, byte(rowBytes), byte(rowBytes >> 8), byte(rows), byte(rows >> 8)})
		for y := top; y < top+rows; y++ {
			row := make([]byte, rowBytes)
			for x := b.Min.X; x < b.Max.X; x++ {
				if img.GrayAt(x, y).Y < 128 {
					row[(x-b.Min.X)/8] |= 0x80 >> ((x - b.Min.X) % 8)
				}
			}
			out.Write(row)
		}
	}
	out.Write([]byte{0x1B, 0x64, 0x04})       // ESC d 4: feed four lines
	out.Write([]byte{0x1D, 0x56, 0x42, 0x00}) // GS V B 0: feed to the cutter and cut, partially
	if drawer {
		out.Write([]byte{0x1B, 0x70, 0x00, 0x19, 0xFA}) // ESC p 0 25 250
	}
	return out.Bytes()
}
