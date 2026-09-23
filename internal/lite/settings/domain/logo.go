package domain

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strconv"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// The shop's logo (the owner's request, 2026-09-24): an image the owner uploads, printed at the head of every invoice and
// receipt. Nothing about any one shop's logo is in the application — Mizan Lite is sold to many shops, and each brings
// its own. With no logo, the documents set the shop's name in type instead.

// Codes for the logo.
const (
	// CodeLogoInvalid refuses a file that is not a PNG or JPEG picture.
	CodeLogoInvalid = "lite.settings.logo_invalid"
	// CodeLogoTooLarge refuses a file too big to be a logo: a photograph straight from a camera, or worse.
	CodeLogoTooLarge = "lite.settings.logo_too_large"
)

const (
	// MaxLogoBytes is the largest file taken. A logo is a few hundred kilobytes; a limit well above that refuses a
	// mistake — the wrong file chosen — before it is decoded.
	MaxLogoBytes = 8 << 20
	// maxLogoPixels bounds the decoded picture, checked from the header BEFORE decoding: a small file can declare a
	// gigantic picture, and decoding it would take the memory of the till.
	maxLogoPixels = 50_000_000
	// LogoSide is the longest side kept. A 72 mm receipt is 576 dots wide and an A4 header a few centimetres tall;
	// 800 pixels prints sharply on both and keeps backups small.
	LogoSide = 800
)

// Logo is the shop's logo as stored: a PNG and its size.
type Logo struct {
	PNG           []byte
	Width, Height int
}

// NormaliseLogo reads an uploaded PNG or JPEG and returns the logo as the application keeps it: a PNG at most LogoSide
// pixels on its longest side, its proportions unchanged. Transparency is kept — the paper shows through it.
func NormaliseLogo(raw []byte) (Logo, error) {
	if len(raw) > MaxLogoBytes {
		return Logo{}, errs.Validation(CodeLogoTooLarge, "the logo file is too large").
			WithParam("max", strconv.Itoa(MaxLogoBytes>>20))
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "png" && format != "jpeg") {
		return Logo{}, errs.Validation(CodeLogoInvalid, "the logo must be a PNG or JPEG picture")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxLogoPixels {
		return Logo{}, errs.Validation(CodeLogoTooLarge, "the logo picture is too large").
			WithParam("max", strconv.Itoa(MaxLogoBytes>>20))
	}
	var img image.Image
	if format == "png" {
		img, err = png.Decode(bytes.NewReader(raw))
	} else {
		img, err = jpeg.Decode(bytes.NewReader(raw))
	}
	if err != nil {
		return Logo{}, errs.Validation(CodeLogoInvalid, "the logo must be a PNG or JPEG picture")
	}
	scaled := shrink(img, LogoSide)
	var out bytes.Buffer
	if err = png.Encode(&out, scaled); err != nil {
		return Logo{}, errs.Wrap(err, errs.CategoryInternal, CodeLogoInvalid, "encoding the logo")
	}
	b := scaled.Bounds()
	return Logo{PNG: out.Bytes(), Width: b.Dx(), Height: b.Dy()}, nil
}

// DecodeLogo reads a stored logo back.
func DecodeLogo(l Logo) (image.Image, error) {
	img, err := png.Decode(bytes.NewReader(l.PNG))
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeLogoInvalid, "the stored logo cannot be read")
	}
	return img, nil
}

// shrink returns img no larger than side on its longest side, averaging every source pixel into the one it becomes —
// a box filter, which keeps thin lines of a logo rather than dropping them the way picking one pixel in four would.
func shrink(img image.Image, side int) *image.NRGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= side && h <= side {
		out := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				out.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return out
	}
	tw, th := side, h*side/w
	if h > w {
		tw, th = w*side/h, side
	}
	tw, th = max(tw, 1), max(th, 1)
	out := image.NewNRGBA(image.Rect(0, 0, tw, th))
	for ty := range th {
		y0, y1 := ty*h/th, max((ty+1)*h/th, ty*h/th+1)
		for tx := range tw {
			x0, x1 := tx*w/tw, max((tx+1)*w/tw, tx*w/tw+1)
			var r, g, bl, a, n uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
					// Average colour weighted by opacity, so a transparent pixel's colour cannot tint the edge.
					r += uint64(c.R) * uint64(c.A)
					g += uint64(c.G) * uint64(c.A)
					bl += uint64(c.B) * uint64(c.A)
					a += uint64(c.A)
					n++
				}
			}
			px := color.NRGBA{A: uint8(a / n)}
			if a > 0 {
				px.R, px.G, px.B = uint8(r/a), uint8(g/a), uint8(bl/a)
			}
			out.SetNRGBA(tx, ty, px)
		}
	}
	return out
}
