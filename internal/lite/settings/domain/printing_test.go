package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
)

func ptr(s string) *string { return &s }

func TestThePrintingSettingsDefaultAndApply(t *testing.T) {
	d := domain.Defaults()
	if d.Receipt != (domain.Receipt{PaperMM: 80, Path: "driver", AutoPrint: "credit"}) || d.BackupFolder != "" {
		t.Fatalf("defaults %+v", d.Receipt)
	}
	next, changes, err := d.Apply(domain.Update{Printing: domain.PrintingUpdate{Printer: ptr(" Xprinter XP-80 "), PaperMM: ptr("58"), Path: ptr("raw"),
		AutoPrint: ptr("all"), Drawer: ptr("true"), Phone: ptr("0933 123 456"), Address: ptr("دمشق - الميدان"), Footer: ptr("شكراً لزيارتكم"),
		BackupFolder: ptr("/Volumes/USB/Backups/")}})
	if err != nil || len(changes) != 9 {
		t.Fatalf("%d changes, %v", len(changes), err)
	}
	if next.Receipt != (domain.Receipt{Printer: "Xprinter XP-80", PaperMM: 58, Path: "raw", AutoPrint: "all", Drawer: true, Phone: "0933 123 456",
		Address: "دمشق - الميدان", Footer: "شكراً لزيارتكم"}) || next.BackupFolder != "/Volumes/USB/Backups" {
		t.Fatalf("applied %+v %q", next.Receipt, next.BackupFolder)
	}
	stored := map[string]string{}
	for _, c := range changes {
		stored[c.Key] = c.Value
	}
	back, problems := domain.FromStored(stored)
	if len(problems) != 0 || back.Receipt != next.Receipt || back.BackupFolder != next.BackupFolder {
		t.Fatalf("stored and read back: %+v %v", back.Receipt, problems)
	}
	if _, again, _ := next.Apply(domain.Update{Printing: domain.PrintingUpdate{PaperMM: ptr("58"), Drawer: ptr("true")}}); len(again) != 0 {
		t.Fatal("an unchanged value is written")
	}
	for raw, code := range map[*domain.PrintingUpdate]string{
		{PaperMM: ptr("110")}:                    domain.CodeInvalidPaper,
		{Path: ptr("usb")}:                       domain.CodeInvalidPath,
		{AutoPrint: ptr("sometimes")}:            domain.CodeInvalidAuto,
		{Drawer: ptr("yes please")}:              domain.CodeInvalidFlag,
		{BackupFolder: ptr("backups")}:           domain.CodeFolderNotAbsolute,
		{Footer: ptr(string(make([]rune, 121)))}: domain.CodeTextTooLong,
	} {
		if _, _, err := d.Apply(domain.Update{Printing: *raw}); errs.CodeOf(err) != code {
			t.Errorf("%+v: %v, want %s", *raw, err, code)
		}
	}
	damaged, problems := domain.FromStored(map[string]string{domain.KeyPaperWidth: "wide", domain.KeyPrinterName: "Xprinter"})
	if len(problems) != 1 || damaged.Receipt.PaperMM != 80 || damaged.Receipt.Printer != "Xprinter" {
		t.Fatalf("a damaged row keeps its default: %+v %v", damaged.Receipt, problems)
	}
}
