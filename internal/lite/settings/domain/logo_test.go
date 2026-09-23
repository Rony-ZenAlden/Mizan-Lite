package domain_test

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestALargeLogoIsShrunkKeepingItsShape: a 2000×1000 upload is kept at 800×400; a small one is kept as it is.
func TestALargeLogoIsShrunkKeepingItsShape(t *testing.T) {
	for _, c := range []struct{ w, h, wantW, wantH int }{{2000, 1000, 800, 400}, {600, 1200, 400, 800}, {120, 60, 120, 60}} {
		logo, err := domain.NormaliseLogo(encodePNG(t, image.NewNRGBA(image.Rect(0, 0, c.w, c.h))))
		if err != nil {
			t.Fatal(err)
		}
		if logo.Width != c.wantW || logo.Height != c.wantH {
			t.Errorf("%d×%d became %d×%d, want %d×%d", c.w, c.h, logo.Width, logo.Height, c.wantW, c.wantH)
		}
		back, err := domain.DecodeLogo(logo)
		if err != nil || back.Bounds().Dx() != c.wantW {
			t.Errorf("the stored logo does not read back: %v", err)
		}
	}
}

// TestAThinLineSurvivesTheShrink: averaging keeps a two-pixel rule that picking one pixel in two could drop.
func TestAThinLineSurvivesTheShrink(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1600, 800))
	for y := range 800 {
		for x := range 1600 {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
		img.SetNRGBA(801, y, color.NRGBA{A: 255})
		img.SetNRGBA(802, y, color.NRGBA{A: 255})
	}
	logo, err := domain.NormaliseLogo(encodePNG(t, img))
	if err != nil {
		t.Fatal(err)
	}
	back, _ := domain.DecodeLogo(logo)
	dark := false
	for x := 398; x <= 402; x++ {
		if r, _, _, _ := back.At(x, 200).RGBA(); r < 0xc000 {
			dark = true
		}
	}
	if !dark {
		t.Fatal("a two-pixel line vanished when the logo was shrunk")
	}
}

func TestAJPEGIsTakenAndStoredAsPNG(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 50, 30)), nil); err != nil {
		t.Fatal(err)
	}
	logo, err := domain.NormaliseLogo(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(logo.PNG, []byte("\x89PNG")) {
		t.Fatal("a JPEG upload was not stored as a PNG")
	}
}

// TestWhatIsNotALogoIsRefused: text, a GIF, a file too large, and a PNG whose header declares a picture of a hundred
// thousand pixels square — refused from the header, before any of it is decoded.
func TestWhatIsNotALogoIsRefused(t *testing.T) {
	var g bytes.Buffer
	if err := gif.Encode(&g, image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		raw  []byte
		code string
	}{
		"text":     {[]byte("not a picture"), domain.CodeLogoInvalid},
		"gif":      {g.Bytes(), domain.CodeLogoInvalid},
		"too big":  {make([]byte, domain.MaxLogoBytes+1), domain.CodeLogoTooLarge},
		"declared": {declaredPNG(100_000, 100_000), domain.CodeLogoTooLarge},
	} {
		if _, err := domain.NormaliseLogo(c.raw); errs.CodeOf(err) != c.code {
			t.Errorf("%s: %v, want %s", name, err, c.code)
		}
	}
}

// declaredPNG is a PNG signature and a header chunk declaring w×h — with a correct checksum, so it is a well-formed header
// and not a damaged file.
func declaredPNG(w, h uint32) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	_ = binary.Write(&ihdr, binary.BigEndian, w)
	_ = binary.Write(&ihdr, binary.BigEndian, h)
	ihdr.Write([]byte{8, 6, 0, 0, 0})
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&out, binary.BigEndian, uint32(13))
	out.Write(ihdr.Bytes())
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes()))
	return out.Bytes()
}
