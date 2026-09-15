//go:build !windows

package printers

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// runner runs a command with input and returns its output; replaced in tests.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		return out, errs.Wrap(err, errs.CategoryInternal, CodeSendFailed, strings.TrimSpace(stderr.String()))
	}
	return out, err
}

// cups is macOS's (and any Unix's) printing, through the lp and lpstat commands CUPS installs.
type cups struct {
	run runner
	dir string
}

// New is this operating system's printing.
func New() System { return &cups{run: execRunner, dir: os.TempDir()} }

func (c *cups) List(ctx context.Context) ([]Printer, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := c.run(ctx, "lpstat", "-e")
	if err != nil {
		// lpstat exits non-zero when no printer is installed; that is an empty list, not a failure.
		if len(bytes.TrimSpace(out)) == 0 {
			return nil, nil
		}
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeListFailed, "listing printers")
	}
	def := ""
	if d, derr := c.run(ctx, "lpstat", "-d"); derr == nil {
		if _, name, ok := strings.Cut(string(d), ": "); ok {
			def = strings.TrimSpace(name)
		}
	}
	var found []Printer
	for _, line := range strings.Split(string(out), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			found = append(found, Printer{Name: name, Default: name == def})
		}
	}
	return found, nil
}

func (c *cups) Send(ctx context.Context, job Job) error {
	if err := check(ctx, c, job); err != nil {
		return err
	}
	body, args := job.DriverPDF, []string{"-d", job.Printer, "-t", job.Title}
	ext := ".pdf"
	if job.Path == PathRaw {
		body, ext = job.Escpos, ".bin"
		args = append(args, "-o", "raw")
	} else if job.PaperMillimetres > 0 {
		args = append(args, "-o", "fit-to-page")
	}
	f, err := os.CreateTemp(c.dir, "mizan-lite-receipt-*"+ext)
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeSendFailed, "preparing the print job")
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err = f.Write(body); err == nil {
		err = f.Close()
	}
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeSendFailed, "preparing the print job")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err = c.run(ctx, "lp", append(args, filepath.Clean(name))...); err != nil {
		return errs.Wrap(err, errs.CategoryConflict, CodeSendFailed, "the print queue refused the receipt").WithParam("printer", job.Printer)
	}
	return nil
}
