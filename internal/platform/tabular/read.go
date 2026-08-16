package tabular

import (
	"bytes"
	"encoding/csv"
	"io"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Stable codes for reading.
const (
	CodeReadFailed    = "tabular.read_failed"
	CodeMissingColumn = "tabular.missing_column"
	CodeNoRows        = "tabular.no_rows"
)

// maxRows bounds an import.
//
// Not a performance limit — the row-by-row service calls are the slow part, and they are the
// point. It bounds MEMORY: a file chosen by mistake (a database dump, a log) would otherwise be
// read whole before anything noticed it was not a product list.
const maxRows = 50_000

// Record is one parsed row, addressed by column NAME.
//
// # Why not positional
//
// A spreadsheet somebody edited has its columns in whatever order they left them, and an importer
// that read by position would put SKUs in the price column without complaining. Naming them means
// a reordered file still works and a MISSING one is named in the error.
type Record struct {
	// Line is the row's position in the file, counting the header as line 1. It is what an error
	// message says, and it must match what the user sees in their spreadsheet.
	Line   int
	values map[string]string
}

// Get returns a trimmed value, or "" when the column was absent.
func (r Record) Get(column string) string {
	return strings.TrimSpace(r.values[strings.ToLower(column)])
}

// Has reports whether a column carried anything.
func (r Record) Has(column string) bool { return r.Get(column) != "" }

// Read parses a CSV into records, requiring the named columns.
//
// # The BOM is stripped, because we write one
//
// A file exported by this application and edited in Excel comes back with the mark still on it,
// and the first header would otherwise carry it — which matches nothing, and produces
// "missing column: code" for a file that plainly has one.
func Read(content []byte, required ...string) ([]Record, error) {
	content = bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})

	reader := csv.NewReader(bytes.NewReader(content))
	// Rows may legitimately differ in length: a spreadsheet drops trailing empty cells.
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, errs.Validation(CodeNoRows, "that file is empty")
		}
		return nil, errs.Wrap(err, errs.CategoryValidation, CodeReadFailed,
			"reading the file's header")
	}

	positions := make(map[string]int, len(header))
	for i, name := range header {
		positions[strings.ToLower(strings.TrimSpace(name))] = i
	}
	for _, column := range required {
		if _, found := positions[strings.ToLower(column)]; !found {
			return nil, errs.Validation(CodeMissingColumn,
				"that file has no column called that").WithParam("column", column)
		}
	}

	records := make([]Record, 0, 128)
	line := 1
	for {
		row, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		line++
		if readErr != nil {
			return nil, errs.Wrap(readErr, errs.CategoryValidation, CodeReadFailed,
				"reading line "+itoa(line))
		}
		if len(records) >= maxRows {
			return nil, errs.Validation(CodeReadFailed,
				"that file has too many rows to import at once").
				WithParam("limit", itoa(maxRows))
		}
		if isBlank(row) {
			// A spreadsheet saved with trailing blank lines is ordinary, and reporting each as a
			// failed row would bury the real ones.
			continue
		}

		values := make(map[string]string, len(positions))
		for name, position := range positions {
			if position < len(row) {
				values[name] = row[position]
			}
		}
		records = append(records, Record{Line: line, values: values})
	}

	if len(records) == 0 {
		return nil, errs.Validation(CodeNoRows, "that file has a header and no rows")
	}
	return records, nil
}

func isBlank(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}
