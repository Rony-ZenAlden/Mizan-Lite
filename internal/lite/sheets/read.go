package sheets

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// Codes for reading a workbook.
const (
	CodeNotAWorkbook = "lite.sheets.not_a_workbook"
	CodeTooLarge     = "lite.sheets.too_large"
	CodeExponent     = "lite.sheets.exponent"
)

// Limits on a workbook the shop imports: generous for a pantry's product list, and a guard against a file that is not one.
const (
	MaxReadBytes = 10 << 20
	MaxReadRows  = 10_000
	MaxColumns   = 50
)

// Read is a sheet as text: each row's cells by column, trailing empty cells dropped, empty rows kept so row numbers match what
// the spreadsheet shows (row 1 is Rows[0]).
type Read struct {
	Name string
	Rows [][]string
}

// ReadXLSX reads every sheet of an .xlsx workbook as text (L8 Q-L8.9). A number is its stored digits ("3.25"); a shared or inline
// string is its text; a formula cell is its cached value. No arithmetic, no float: a number written with an exponent is refused
// by name rather than rounded.
func ReadXLSX(body []byte) ([]Read, error) {
	if len(body) > MaxReadBytes {
		return nil, errs.Validation(CodeTooLarge, "the workbook is too large").WithParam("max", strconv.Itoa(MaxReadBytes>>20))
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryValidation, CodeNotAWorkbook, "not a zip")
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	shared, err := sharedStrings(files["xl/sharedStrings.xml"])
	if err != nil {
		return nil, err
	}
	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err = decode(files["xl/workbook.xml"], &book); err != nil {
		return nil, err
	}
	var rels struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err = decode(files["xl/_rels/workbook.xml.rels"], &rels); err != nil {
		return nil, err
	}
	targets := map[string]string{}
	for _, r := range rels.Rels {
		t := strings.TrimPrefix(r.Target, "/")
		if !strings.HasPrefix(t, "xl/") {
			t = path.Join("xl", t)
		}
		targets[r.ID] = t
	}
	var out []Read
	for _, s := range book.Sheets {
		rows, sheetErr := sheetRows(files[targets[s.RID]], shared)
		if sheetErr != nil {
			return nil, sheetErr
		}
		out = append(out, Read{Name: s.Name, Rows: rows})
	}
	if len(out) == 0 {
		return nil, errs.Validation(CodeNotAWorkbook, "the workbook has no sheet")
	}
	return out, nil
}

func decode(f *zip.File, v any) error {
	if f == nil {
		return errs.Validation(CodeNotAWorkbook, "a workbook part is missing")
	}
	rc, err := f.Open()
	if err != nil {
		return errs.Wrap(err, errs.CategoryValidation, CodeNotAWorkbook, "opening "+f.Name)
	}
	defer rc.Close()
	if err = xml.NewDecoder(io.LimitReader(rc, MaxReadBytes*4)).Decode(v); err != nil {
		return errs.Wrap(err, errs.CategoryValidation, CodeNotAWorkbook, "reading "+f.Name)
	}
	return nil
}

// richText is a string item: plain <t>, or runs of <r><t>.
type richText struct {
	T    string `xml:"t"`
	Runs []struct {
		T string `xml:"t"`
	} `xml:"r"`
}

func (r richText) text() string {
	var b strings.Builder
	b.WriteString(r.T)
	for _, run := range r.Runs {
		b.WriteString(run.T)
	}
	return b.String()
}

func sharedStrings(f *zip.File) ([]string, error) {
	if f == nil {
		return nil, nil
	}
	var sst struct {
		Items []richText `xml:"si"`
	}
	if err := decode(f, &sst); err != nil {
		return nil, err
	}
	out := make([]string, len(sst.Items))
	for i, item := range sst.Items {
		out[i] = item.text()
	}
	return out, nil
}

func sheetRows(f *zip.File, shared []string) ([][]string, error) {
	var ws struct {
		Rows []struct {
			R     int `xml:"r,attr"`
			Cells []struct {
				Ref    string   `xml:"r,attr"`
				Type   string   `xml:"t,attr"`
				Value  string   `xml:"v"`
				Inline richText `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := decode(f, &ws); err != nil {
		return nil, err
	}
	var rows [][]string
	for i, r := range ws.Rows {
		n := r.R
		if n <= 0 {
			n = i + 1
		}
		if n > MaxReadRows {
			return nil, errs.Validation(CodeTooLarge, "too many rows").WithParam("max", strconv.Itoa(MaxReadRows))
		}
		for len(rows) < n {
			rows = append(rows, nil)
		}
		var cells []string
		for j, c := range r.Cells {
			col := j
			if c.Ref != "" {
				col = columnIndex(c.Ref)
			}
			if col < 0 || col >= MaxColumns {
				continue
			}
			var text string
			switch c.Type {
			case "s":
				idx, err := strconv.Atoi(strings.TrimSpace(c.Value))
				if err != nil || idx < 0 || idx >= len(shared) {
					return nil, errs.Validation(CodeNotAWorkbook, "a shared string is missing").WithParam("cell", c.Ref)
				}
				text = shared[idx]
			case "inlineStr":
				text = c.Inline.text()
			case "b":
				text = map[string]string{"1": "TRUE", "0": "FALSE"}[c.Value]
			case "str", "e":
				text = c.Value
			default:
				text = strings.TrimSpace(c.Value)
				if strings.ContainsAny(text, "eE") {
					return nil, errs.Validation(CodeExponent, "a number written with an exponent").WithParam("cell", c.Ref).WithParam("value", text)
				}
			}
			for len(cells) <= col {
				cells = append(cells, "")
			}
			cells[col] = text
		}
		for len(cells) > 0 && strings.TrimSpace(cells[len(cells)-1]) == "" {
			cells = cells[:len(cells)-1]
		}
		rows[n-1] = cells
	}
	return rows, nil
}

// columnIndex is a cell reference's column, from 0: "A1" → 0, "AB12" → 27; -1 when there is none.
func columnIndex(ref string) int {
	n := 0
	seen := false
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
		seen = true
	}
	if !seen {
		return -1
	}
	return n - 1
}
