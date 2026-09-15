package domain

import (
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// The receipt printer and the receipt's header and footer (L7 §5), and where backups are copied (L7 §6.2). Changed by the
// owner through the bindings, which ask for the PIN.
const (
	KeyPrinterName   = "receipt.printer"
	KeyPaperWidth    = "receipt.paper_mm"
	KeyPrintPath     = "receipt.path"
	KeyAutoPrint     = "receipt.auto_print"
	KeyCashDrawer    = "receipt.cash_drawer"
	KeyShopPhone     = "shop.phone"
	KeyShopAddress   = "shop.address"
	KeyReceiptFooter = "receipt.footer"
	KeyBackupFolder  = "backup.outside_folder"
)

// Codes for the printing and backup settings.
const (
	CodeInvalidPaper      = "lite.settings.invalid_paper"
	CodeInvalidPath       = "lite.settings.invalid_print_path"
	CodeInvalidAuto       = "lite.settings.invalid_auto_print"
	CodeInvalidFlag       = "lite.settings.invalid_flag"
	CodeTextTooLong       = "lite.settings.text_too_long"
	CodeFolderNotAbsolute = "lite.settings.folder_not_absolute"
)

// Automatic printing (Q-L7.2).
const (
	AutoPrintNone   = "none"
	AutoPrintCredit = "credit" // credit sales, debt payments and refunds print themselves; a cash sale on a button
	AutoPrintAll    = "all"
)

// Print paths (D-L7.9). The driver path is the default: the owner's printer is installed with its driver (Q-L7.1).
const (
	PrintPathDriver = "driver"
	PrintPathRaw    = "raw"
)

// Receipt is the printer and what the receipt carries.
type Receipt struct {
	Printer   string
	PaperMM   int
	Path      string
	AutoPrint string
	Drawer    bool
	Phone     string
	Address   string
	Footer    string
}

// DefaultReceipt is a fresh installation's: no printer, 80 mm, through the driver, credit printed automatically.
func DefaultReceipt() Receipt {
	return Receipt{PaperMM: 80, Path: PrintPathDriver, AutoPrint: AutoPrintCredit}
}

func tooLong(field string, limit int) error {
	return errs.Validation(CodeTextTooLong, "too long").WithField(field, CodeTextTooLong, "too long").WithParam("max", strconv.Itoa(limit))
}

// ParseText trims text and bounds it.
func ParseText(raw, field string, limit int) (string, error) {
	s := strings.TrimSpace(raw)
	if utf8.RuneCountInString(s) > limit {
		return "", tooLong(field, limit)
	}
	return s, nil
}

// ParsePaper accepts 80 or 58.
func ParsePaper(raw string) (int, error) {
	switch strings.TrimSpace(raw) {
	case "80":
		return 80, nil
	case "58":
		return 58, nil
	}
	return 0, errs.Validation(CodeInvalidPaper, "paper is 80 or 58 mm").WithField("paperMm", CodeInvalidPaper, "invalid")
}

// ParsePrintPath accepts driver or raw.
func ParsePrintPath(raw string) (string, error) {
	switch v := strings.TrimSpace(raw); v {
	case PrintPathDriver, PrintPathRaw:
		return v, nil
	}
	return "", errs.Validation(CodeInvalidPath, "driver or raw").WithField("path", CodeInvalidPath, "invalid")
}

// ParseAutoPrint accepts none, credit or all.
func ParseAutoPrint(raw string) (string, error) {
	switch v := strings.TrimSpace(raw); v {
	case AutoPrintNone, AutoPrintCredit, AutoPrintAll:
		return v, nil
	}
	return "", errs.Validation(CodeInvalidAuto, "none, credit or all").WithField("autoPrint", CodeInvalidAuto, "invalid")
}

// ParseFlag accepts true or false.
func ParseFlag(raw, field string) (bool, error) {
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, errs.Validation(CodeInvalidFlag, "true or false").WithField(field, CodeInvalidFlag, "invalid")
	}
	return v, nil
}

// ParseFolder accepts an absolute folder path, or empty for none.
func ParseFolder(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	if utf8.RuneCountInString(s) > 1024 {
		return "", tooLong("folder", 1024)
	}
	if !filepath.IsAbs(s) {
		return "", errs.Validation(CodeFolderNotAbsolute, "a full folder path").WithField("folder", CodeFolderNotAbsolute, "invalid")
	}
	return filepath.Clean(s), nil
}

// Bounds of the receipt's texts.
const (
	MaxPrinterRunes = 200
	MaxPhoneRunes   = 40
	MaxLineRunes    = 120
)

// storedPrinting resolves one printing or backup row; handled is false for a key it does not own.
func (s *Settings) storedPrinting(key, value string) (handled bool, err error) {
	r := &s.Receipt
	switch key {
	case KeyPrinterName:
		r.Printer, err = ParseText(value, "printer", MaxPrinterRunes)
	case KeyPaperWidth:
		r.PaperMM, err = ParsePaper(value)
	case KeyPrintPath:
		r.Path, err = ParsePrintPath(value)
	case KeyAutoPrint:
		r.AutoPrint, err = ParseAutoPrint(value)
	case KeyCashDrawer:
		r.Drawer, err = ParseFlag(value, "drawer")
	case KeyShopPhone:
		r.Phone, err = ParseText(value, "phone", MaxPhoneRunes)
	case KeyShopAddress:
		r.Address, err = ParseText(value, "address", MaxLineRunes)
	case KeyReceiptFooter:
		r.Footer, err = ParseText(value, "footer", MaxLineRunes)
	case KeyBackupFolder:
		s.BackupFolder, err = ParseFolder(value)
	default:
		return false, nil
	}
	return true, err
}

// PrintingUpdate is a change to the printing and backup settings; nil leaves a field as it is.
type PrintingUpdate struct {
	Printer, PaperMM, Path, AutoPrint, Drawer, Phone, Address, Footer, BackupFolder *string
}

func (s Settings) applyPrinting(u PrintingUpdate, changes []Change) (Settings, []Change, error) {
	type field struct {
		raw     *string
		key     string
		current string
		set     func(string) (string, error)
	}
	next := s
	r := &next.Receipt
	fields := []field{
		{u.Printer, KeyPrinterName, s.Receipt.Printer, func(v string) (string, error) {
			p, err := ParseText(v, "printer", MaxPrinterRunes)
			r.Printer = p
			return p, err
		}},
		{u.PaperMM, KeyPaperWidth, strconv.Itoa(s.Receipt.PaperMM), func(v string) (string, error) { p, err := ParsePaper(v); r.PaperMM = p; return strconv.Itoa(p), err }},
		{u.Path, KeyPrintPath, s.Receipt.Path, func(v string) (string, error) { p, err := ParsePrintPath(v); r.Path = p; return p, err }},
		{u.AutoPrint, KeyAutoPrint, s.Receipt.AutoPrint, func(v string) (string, error) { p, err := ParseAutoPrint(v); r.AutoPrint = p; return p, err }},
		{u.Drawer, KeyCashDrawer, strconv.FormatBool(s.Receipt.Drawer), func(v string) (string, error) {
			p, err := ParseFlag(v, "drawer")
			r.Drawer = p
			return strconv.FormatBool(p), err
		}},
		{u.Phone, KeyShopPhone, s.Receipt.Phone, func(v string) (string, error) {
			p, err := ParseText(v, "phone", MaxPhoneRunes)
			r.Phone = p
			return p, err
		}},
		{u.Address, KeyShopAddress, s.Receipt.Address, func(v string) (string, error) {
			p, err := ParseText(v, "address", MaxLineRunes)
			r.Address = p
			return p, err
		}},
		{u.Footer, KeyReceiptFooter, s.Receipt.Footer, func(v string) (string, error) {
			p, err := ParseText(v, "footer", MaxLineRunes)
			r.Footer = p
			return p, err
		}},
		{u.BackupFolder, KeyBackupFolder, s.BackupFolder, func(v string) (string, error) { p, err := ParseFolder(v); next.BackupFolder = p; return p, err }},
	}
	for _, f := range fields {
		if f.raw == nil {
			continue
		}
		value, err := f.set(*f.raw)
		if err != nil {
			return s, nil, err
		}
		if value != f.current {
			changes = append(changes, Change{Key: f.key, Value: value})
		}
	}
	return next, changes, nil
}

// mergeDefault restores the default of the one receipt field a damaged row named.
func mergeDefault(r, d Receipt, key string) Receipt {
	switch key {
	case KeyPrinterName:
		r.Printer = d.Printer
	case KeyPaperWidth:
		r.PaperMM = d.PaperMM
	case KeyPrintPath:
		r.Path = d.Path
	case KeyAutoPrint:
		r.AutoPrint = d.AutoPrint
	case KeyCashDrawer:
		r.Drawer = d.Drawer
	case KeyShopPhone:
		r.Phone = d.Phone
	case KeyShopAddress:
		r.Address = d.Address
	case KeyReceiptFooter:
		r.Footer = d.Footer
	}
	return r
}
