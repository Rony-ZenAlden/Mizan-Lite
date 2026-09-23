// Package printers finds the operating system's printers and hands them a receipt (L7 §5): the ESC/POS bytes of its bitmap
// through the print queue as raw data, or the same bitmap through the printer's own driver (D-L7.8, D-L7.9).
//
// The only Lite package that calls the operating system — os/exec for CUPS on macOS, the spooler and GDI through syscall on
// Windows — held there by archlint's lite-os-calls-only-in-printers rule (A-L7.4). It prints what it is given and knows
// nothing of sales.
package printers

import (
	"context"
	"image"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable error codes. They double as i18n keys.
const (
	CodeNoPrinter   = "lite.printers.no_printer"
	CodeNotFound    = "lite.printers.not_found"
	CodeSendFailed  = "lite.printers.send_failed"
	CodeUnsupported = "lite.printers.unsupported"
	CodeListFailed  = "lite.printers.list_failed"
)

// Path is how a job reaches the printer.
type Path string

// The paths. The owner's printer is installed with its driver (Q-L7.1); driver is the default, raw one setting away.
const (
	PathDriver Path = "driver"
	PathRaw    Path = "raw"
)

// ParsePath accepts a path.
func ParsePath(raw string) (Path, bool) {
	switch Path(raw) {
	case PathDriver, PathRaw:
		return Path(raw), true
	}
	return "", false
}

// Printer is a printer the operating system knows.
type Printer struct {
	Name    string
	Default bool
}

// Job is a receipt to print.
type Job struct {
	Printer string
	Path    Path
	// Title names the job in the queue.
	Title string
	// Raster is the receipt, black and white; Escpos its ESC/POS bytes (the raw path); DriverPDF the bitmap as a page (the
	// driver path on macOS). Windows' driver path prints Raster itself.
	Raster    *image.Gray
	Escpos    []byte
	DriverPDF []byte
	// PaperMillimetres is the paper's width, for the driver path.
	PaperMillimetres int
	// Pages is a paged document at the printer's resolution — an A4 invoice (0.10.0). The driver path on Windows draws
	// each on a sheet of its own; macOS prints DriverPDF, which is the same pages in vector form. A receipt has none.
	Pages []*image.Gray
}

// System is the operating system's printing.
type System interface {
	List(ctx context.Context) ([]Printer, error)
	Send(ctx context.Context, job Job) error
}

// check refuses a job with no printer, or one the system does not list.
func check(ctx context.Context, s System, job Job) error {
	if job.Printer == "" {
		return errs.Conflict(CodeNoPrinter, "no receipt printer is chosen")
	}
	found, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, p := range found {
		if p.Name == job.Printer {
			return nil
		}
	}
	return errs.NotFound(CodeNotFound, "the chosen printer is not installed").WithParam("printer", job.Printer)
}
