//go:build windows

package printers

import (
	"context"
	"image"
	"syscall"
	"unsafe"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Windows printing through the spooler (winspool.drv) and GDI (gdi32.dll). Compiled on every build; never run on this
// project's machines (O1) — the owner's printer is the test (L7 §12.3).
var (
	winspool          = syscall.NewLazyDLL("winspool.drv")
	gdi32             = syscall.NewLazyDLL("gdi32.dll")
	procEnumPrinters  = winspool.NewProc("EnumPrintersW")
	procGetDefault    = winspool.NewProc("GetDefaultPrinterW")
	procOpenPrinter   = winspool.NewProc("OpenPrinterW")
	procClosePrinter  = winspool.NewProc("ClosePrinter")
	procStartDoc      = winspool.NewProc("StartDocPrinterW")
	procEndDoc        = winspool.NewProc("EndDocPrinter")
	procStartPage     = winspool.NewProc("StartPagePrinter")
	procEndPage       = winspool.NewProc("EndPagePrinter")
	procWritePrinter  = winspool.NewProc("WritePrinter")
	procCreateDC      = gdi32.NewProc("CreateDCW")
	procDeleteDC      = gdi32.NewProc("DeleteDC")
	procGdiStartDoc   = gdi32.NewProc("StartDocW")
	procGdiEndDoc     = gdi32.NewProc("EndDoc")
	procGdiStartPage  = gdi32.NewProc("StartPage")
	procGdiEndPage    = gdi32.NewProc("EndPage")
	procStretchDIBits = gdi32.NewProc("StretchDIBits")
	procGetDeviceCaps = gdi32.NewProc("GetDeviceCaps")
)

const (
	printerEnumLocal       = 0x2
	printerEnumConnections = 0x4
	horzres                = 8
	srccopy                = 0x00CC0020
	dibRGBColors           = 0
)

type printerInfo4 struct {
	name       *uint16
	server     *uint16
	attributes uint32
}

type docInfo1 struct {
	docName    *uint16
	outputFile *uint16
	datatype   *uint16
}

type gdiDocInfo struct {
	size     int32
	docName  *uint16
	output   *uint16
	datatype *uint16
	fwType   uint32
}

type spooler struct{}

// New is this operating system's printing.
func New() System { return spooler{} }

func (spooler) List(context.Context) ([]Printer, error) {
	var needed, returned uint32
	flags := uintptr(printerEnumLocal | printerEnumConnections)
	_, _, _ = procEnumPrinters.Call(flags, 0, 4, 0, 0, uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if needed == 0 {
		return nil, nil
	}
	buf := make([]byte, needed)
	r, _, callErr := procEnumPrinters.Call(flags, 0, 4, uintptr(unsafe.Pointer(&buf[0])), uintptr(needed),
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if r == 0 {
		return nil, errs.Wrap(callErr, errs.CategoryInternal, CodeListFailed, "listing printers")
	}
	def := defaultPrinter()
	size := unsafe.Sizeof(printerInfo4{})
	found := make([]Printer, 0, returned)
	for i := uintptr(0); i < uintptr(returned); i++ {
		info := (*printerInfo4)(unsafe.Pointer(&buf[i*size]))
		name := syscall.UTF16ToString(unsafe.Slice(info.name, 1024))
		found = append(found, Printer{Name: name, Default: name == def})
	}
	return found, nil
}

func defaultPrinter() string {
	var n uint32
	_, _, _ = procGetDefault.Call(0, uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n)
	if r, _, _ := procGetDefault.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func (s spooler) Send(ctx context.Context, job Job) error {
	if err := check(ctx, s, job); err != nil {
		return err
	}
	if job.Path == PathRaw {
		return raw(job)
	}
	return driver(job)
}

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func failed(err error, what string, job Job) error {
	return errs.Wrap(err, errs.CategoryConflict, CodeSendFailed, what).WithParam("printer", job.Printer)
}

// raw writes the ESC/POS bytes through the spooler with the RAW datatype. A v4 (XPS) driver refuses it (L7 H7).
func raw(job Job) error {
	var handle syscall.Handle
	if r, _, err := procOpenPrinter.Call(uintptr(unsafe.Pointer(utf16(job.Printer))), uintptr(unsafe.Pointer(&handle)), 0); r == 0 {
		return failed(err, "opening the printer", job)
	}
	defer func() { _, _, _ = procClosePrinter.Call(uintptr(handle)) }()
	doc := docInfo1{docName: utf16(job.Title), datatype: utf16("RAW")}
	if r, _, err := procStartDoc.Call(uintptr(handle), 1, uintptr(unsafe.Pointer(&doc))); r == 0 {
		return failed(err, "starting the print job", job)
	}
	defer func() { _, _, _ = procEndDoc.Call(uintptr(handle)) }()
	if r, _, err := procStartPage.Call(uintptr(handle)); r == 0 {
		return failed(err, "starting the page", job)
	}
	var written uint32
	r, _, err := procWritePrinter.Call(uintptr(handle), uintptr(unsafe.Pointer(&job.Escpos[0])), uintptr(len(job.Escpos)), uintptr(unsafe.Pointer(&written)))
	_, _, _ = procEndPage.Call(uintptr(handle))
	if r == 0 || int(written) != len(job.Escpos) {
		return failed(err, "writing the receipt", job)
	}
	return nil
}

// bitmapInfo is a BITMAPINFOHEADER and a two-colour palette.
type bitmapInfo struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
	palette       [2][4]byte
}

// driver draws the bitmap on the printer's page through GDI, scaled to the printable width.
func driver(job Job) error {
	dc, _, err := procCreateDC.Call(uintptr(unsafe.Pointer(utf16("WINSPOOL"))), uintptr(unsafe.Pointer(utf16(job.Printer))), 0, 0)
	if dc == 0 {
		return failed(err, "opening the printer", job)
	}
	defer func() { _, _, _ = procDeleteDC.Call(dc) }()
	info := gdiDocInfo{docName: utf16(job.Title)}
	info.size = int32(unsafe.Sizeof(info))
	if r, _, callErr := procGdiStartDoc.Call(dc, uintptr(unsafe.Pointer(&info))); int32(r) <= 0 {
		return failed(callErr, "starting the print job", job)
	}
	defer func() { _, _, _ = procGdiEndDoc.Call(dc) }()
	if r, _, callErr := procGdiStartPage.Call(dc); int32(r) <= 0 {
		return failed(callErr, "starting the page", job)
	}
	bits, header := dib(job.Raster)
	pageWidth, _, _ := procGetDeviceCaps.Call(dc, horzres)
	b := job.Raster.Bounds()
	height := int(pageWidth) * b.Dy() / b.Dx()
	r, _, callErr := procStretchDIBits.Call(dc, 0, 0, pageWidth, uintptr(height), 0, 0, uintptr(b.Dx()), uintptr(b.Dy()),
		uintptr(unsafe.Pointer(&bits[0])), uintptr(unsafe.Pointer(&header)), dibRGBColors, srccopy)
	_, _, _ = procGdiEndPage.Call(dc)
	if r == 0 {
		return failed(callErr, "drawing the receipt", job)
	}
	return nil
}

// dib is the bitmap as a bottom-up 1-bit DIB: rows padded to four bytes, 1 white.
func dib(img *image.Gray) ([]byte, bitmapInfo) {
	b := img.Bounds()
	stride := ((b.Dx() + 31) / 32) * 4
	bits := make([]byte, stride*b.Dy())
	for y := 0; y < b.Dy(); y++ {
		row := bits[(b.Dy()-1-y)*stride:]
		for x := 0; x < b.Dx(); x++ {
			if img.GrayAt(b.Min.X+x, b.Min.Y+y).Y >= 128 {
				row[x/8] |= 0x80 >> (x % 8)
			}
		}
	}
	header := bitmapInfo{width: int32(b.Dx()), height: int32(b.Dy()), planes: 1, bitCount: 1, palette: [2][4]byte{{0, 0, 0, 0}, {255, 255, 255, 0}}}
	header.size = 40
	return bits, header
}
