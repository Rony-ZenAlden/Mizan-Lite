package documents

import (
	"bytes"
	"image"
	"image/png"

	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

// Raster renders a document on endless thermal paper as a gray image, every pixel black (0) or white (255): the bitmap a
// receipt printer prints (D-L7.8), and — as PNG — the preview the screen shows (D-L7.11).
func Raster(ts *typeset.Typesetter, doc Document, m Media) *image.Gray {
	m.Paged, m.Height = false, 0
	lo := layout(ts, doc, m)
	height := int(lo.height + 0.5)
	mask := image.NewAlpha(image.Rect(0, 0, int(m.Width), height))
	for _, p := range lo.pages {
		for _, o := range p.ops {
			switch o.kind {
			case opText:
				ts.Draw(mask, o.line, o.x, o.y)
			case opLine:
				thick := max(1, int(o.width*2+0.5))
				for x := int(o.x); x < int(o.x2); x++ {
					if o.dashed && (x/4)%2 == 1 {
						continue
					}
					for t := range thick {
						mask.Pix[mask.PixOffset(x, clamp(int(o.y)+t, height))] = 255
					}
				}
			case opRect:
				x0, y0, x1, y1 := int(o.x), int(o.y), int(o.x+o.x2), int(o.y+o.y2)
				for t := range max(1, int(o.width)) {
					for x := x0; x <= x1; x++ {
						mask.Pix[mask.PixOffset(clamp(x, int(m.Width)), clamp(y0+t, height))] = 255
						mask.Pix[mask.PixOffset(clamp(x, int(m.Width)), clamp(y1-t, height))] = 255
					}
					for y := y0; y <= y1; y++ {
						mask.Pix[mask.PixOffset(clamp(x0+t, int(m.Width)), clamp(y, height))] = 255
						mask.Pix[mask.PixOffset(clamp(x1-t, int(m.Width)), clamp(y, height))] = 255
					}
				}
			}
		}
	}
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
