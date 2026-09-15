//go:build !windows

package printers_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/printers"
)

var ctx = context.Background()

type fakeCups struct {
	printers string
	def      string
	sent     [][]string
	bodies   [][]byte
	refuse   bool
}

func (f *fakeCups) run(_ context.Context, name string, args ...string) ([]byte, error) {
	switch {
	case name == "lpstat" && args[0] == "-e":
		if f.printers == "" {
			return nil, errors.New("exit status 1")
		}
		return []byte(f.printers), nil
	case name == "lpstat" && args[0] == "-d":
		return []byte("system default destination: " + f.def + "\n"), nil
	case name == "lp":
		f.sent = append(f.sent, args)
		body, _ := os.ReadFile(args[len(args)-1])
		f.bodies = append(f.bodies, body)
		if f.refuse {
			return nil, errors.New("lp: Unsupported document-format \"application/vnd.cups-raw\"")
		}
		return []byte("request id is Xprinter-12 (1 file(s))\n"), nil
	}
	return nil, errors.New("unexpected command " + name)
}

func TestPrintersAreListedWithTheDefault(t *testing.T) {
	f := &fakeCups{printers: "Xprinter_XP_80\nOffice_Laser\n", def: "Office_Laser"}
	got, err := printers.NewForTest(f.run, t.TempDir()).List(ctx)
	if err != nil || len(got) != 2 || got[0] != (printers.Printer{Name: "Xprinter_XP_80"}) || !got[1].Default {
		t.Fatalf("%+v %v", got, err)
	}
	if none, err := printers.NewForTest((&fakeCups{}).run, t.TempDir()).List(ctx); err != nil || len(none) != 0 {
		t.Fatalf("no printers installed is an empty list: %+v %v", none, err)
	}
}

func TestTheRawJobIsWhatWasRendered(t *testing.T) {
	f := &fakeCups{printers: "Xprinter_XP_80\n"}
	dir := t.TempDir()
	sys := printers.NewForTest(f.run, dir)
	escpos := []byte{0x1B, 0x40, 0x1D, 0x76, 0x30, 0x00}
	if err := sys.Send(ctx, printers.Job{Printer: "Xprinter_XP_80", Path: printers.PathRaw, Title: "Receipt 12", Escpos: escpos}); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(f.sent[0][:len(f.sent[0])-1], " ")
	if args != "-d Xprinter_XP_80 -t Receipt 12 -o raw" || !bytes.Equal(f.bodies[0], escpos) {
		t.Fatalf("lp %s with % X", args, f.bodies[0])
	}
	pdf := []byte("%PDF-1.7 receipt")
	if err := sys.Send(ctx, printers.Job{Printer: "Xprinter_XP_80", Path: printers.PathDriver, Title: "Receipt 12", DriverPDF: pdf, PaperMillimetres: 80}); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(f.sent[1][:len(f.sent[1])-1], " "); args != "-d Xprinter_XP_80 -t Receipt 12 -o fit-to-page" || !bytes.Equal(f.bodies[1], pdf) {
		t.Fatalf("driver path: lp %s", args)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("job files left behind: %v", left)
	}
}

func TestAPrinterFailureHasACode(t *testing.T) {
	f := &fakeCups{printers: "Xprinter_XP_80\n", refuse: true}
	sys := printers.NewForTest(f.run, t.TempDir())
	job := printers.Job{Printer: "Xprinter_XP_80", Path: printers.PathRaw, Escpos: []byte{1}}
	if err := sys.Send(ctx, job); errs.CodeOf(err) != printers.CodeSendFailed {
		t.Fatalf("a refused job: %v", err)
	}
	if err := sys.Send(ctx, printers.Job{Path: printers.PathRaw}); errs.CodeOf(err) != printers.CodeNoPrinter {
		t.Fatalf("no printer chosen: %v", err)
	}
	if err := sys.Send(ctx, printers.Job{Printer: "Gone", Path: printers.PathRaw}); errs.CodeOf(err) != printers.CodeNotFound {
		t.Fatalf("a printer since removed: %v", err)
	}
	if _, ok := printers.ParsePath("usb"); ok {
		t.Fatal("unknown path accepted")
	}
}

// TestTheRealCommandsAreRunFromThePath runs New() against stand-in lp and lpstat scripts: the exec plumbing, not a fake of it.
func TestTheRealCommandsAreRunFromThePath(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell")
	}
	bin := t.TempDir()
	out := filepath.Join(bin, "received")
	scripts := map[string]string{
		"lpstat": "#!/bin/sh\nif [ \"$1\" = \"-e\" ]; then echo Stand_In; else echo 'system default destination: Stand_In'; fi\n",
		"lp":     "#!/bin/sh\nfor last; do :; done\ncat \"$last\" > '" + out + "'\necho \"$@\" >> '" + out + ".args'\n",
	}
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	sys := printers.New()
	found, err := sys.List(ctx)
	if err != nil || len(found) != 1 || !found[0].Default {
		t.Fatalf("%+v %v", found, err)
	}
	if err = sys.Send(ctx, printers.Job{Printer: "Stand_In", Path: printers.PathRaw, Title: "T", Escpos: []byte("ESCPOS")}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(out)
	args, _ := os.ReadFile(out + ".args")
	if string(body) != "ESCPOS" || !strings.HasPrefix(string(args), "-d Stand_In -t T -o raw ") {
		t.Fatalf("%q %q", body, args)
	}
}
