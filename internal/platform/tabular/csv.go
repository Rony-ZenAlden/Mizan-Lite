// Package tabular writes rows as CSV, for export (§9.3).
//
// # Why CSV and not XLSX
//
// XLSX needs a library, and a library that writes a binary format is a dependency for a decade. A
// shop that wants a spreadsheet opens the CSV in one — every spreadsheet on every platform reads
// it, and so does every accountant's own system.
//
// # Why this is a package and not three functions in the bindings
//
// Two things here are easy to get wrong and expensive to get wrong twice: the BOM that makes
// Excel read Arabic correctly, and the leading-character escape that stops a spreadsheet
// executing a cell. Both belong in one place, and the second one belongs there for a reason worth
// stating in full below.
package tabular

import (
	"bytes"
	"encoding/csv"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// CodeWriteFailed is the only failure this package has.
const CodeWriteFailed = "tabular.write_failed"

// Sheet is what an export produces: named columns and their rows.
//
// Deliberately not "a report" or "a query": this package knows nothing about where the rows came
// from, which is what stops it growing a second way to fetch them.
type Sheet struct {
	// Columns are the headers, in order. Already TRANSLATED by the caller — this package has no
	// locale and inventing one would put i18n in two places.
	Columns []string
	// Rows are the values, each the same length as Columns.
	Rows [][]string
}

// CSV renders a sheet.
//
// # The byte-order mark is not optional
//
// Excel on Windows reads a UTF-8 file as the system's legacy code page unless it begins with a
// BOM — so an Arabic product name opens as mojibake for exactly the users this application is
// built for. Every other consumer tolerates the BOM; Excel does not tolerate its absence.
func CSV(sheet Sheet) ([]byte, error) {
	if err := validate(sheet); err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	buffer.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(&buffer)
	if err := writer.Write(sanitiseRow(sheet.Columns)); err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "writing the header")
	}
	for _, row := range sheet.Rows {
		if err := writer.Write(sanitiseRow(row)); err != nil {
			return nil, errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "writing a row")
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "finishing the file")
	}
	return buffer.Bytes(), nil
}

func validate(sheet Sheet) error {
	if len(sheet.Columns) == 0 {
		return errs.Validation(CodeWriteFailed, "a sheet needs columns")
	}
	for i, row := range sheet.Rows {
		if len(row) != len(sheet.Columns) {
			// A short row silently shifts every value after it into the wrong column, and the
			// file still opens. Refusing here is the only place it can be caught.
			return errs.Internal(CodeWriteFailed,
				"a row does not match the columns").
				WithParam("row", itoa(i+1)).
				WithParam("values", itoa(len(row))).
				WithParam("columns", itoa(len(sheet.Columns)))
		}
	}
	return nil
}

func sanitiseRow(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = sanitise(value)
	}
	return out
}

// sanitise defuses a cell a spreadsheet would treat as a formula.
//
// # Why an export has to think about this at all
//
// A cell beginning with `=`, `+`, `-` or `@` is a FORMULA to Excel, LibreOffice and Google
// Sheets. `=1+1` displays as 2, which is merely wrong; `=cmd|'/c calc'!A1` is a command the
// spreadsheet will offer to run.
//
// The values here come from a shop's own data — a product named "-- clearance --", a partner
// whose note starts with "+974…" — so this is not primarily about an attacker. It is about an
// export that silently changes what the data SAYS. That it also closes an injection route is the
// second reason, not the first.
//
// A leading apostrophe is the fix every spreadsheet understands: it forces the cell to text and
// is not displayed.
func sanitise(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}

// Filename builds a safe download name from a report name and a range.
//
// The name reaches a filesystem, so anything that could traverse or confuse one is replaced
// rather than escaped: a report called "Sales / Margin" must not produce a path.
func Filename(report, from, to string) string {
	clean := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
				b.WriteRune(r)
			case r == '-', r == '_':
				b.WriteRune(r)
			case r == ' ' || r == '/' || r == '.':
				b.WriteRune('-')
			}
		}
		return strings.Trim(b.String(), "-")
	}

	name := clean(report)
	if name == "" {
		name = "export"
	}
	if from != "" && to != "" {
		name += "_" + clean(from) + "_" + clean(to)
	}
	return name + ".csv"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
