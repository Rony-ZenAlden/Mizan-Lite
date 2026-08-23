package tabular

import (
	"archive/zip"
	"bytes"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// DOCX renders a sheet as a Word document: a title, and the rows as a table.
//
// # Why a shop asks for this at all
//
// A spreadsheet is for working with numbers; a Word file is for SENDING one. A customer statement
// that goes to a customer, an inventory count that goes to an accountant, a supplier reconciliation
// that goes in an email — those get opened, read, and sometimes signed, and a CSV is the wrong
// object for every one of them.
//
// Like the workbook beside it, this is written by hand: a .docx is a ZIP of two small XML parts,
// and the subset needed for "a heading and a table" is a page of markup. No dependency, nothing to
// audit, nothing to upgrade.
//
// # Right to left is a property of the DOCUMENT
//
// Word needs three separate things to render an Arabic document correctly, and getting two of them
// is worse than getting none because the result looks nearly right: the section must be RTL so the
// text flows from the right, the table must be RTL so column one is on the right, and each
// paragraph run must be marked `rtl` so the bidirectional algorithm treats mixed digits correctly.
// All three are set together below, from one flag.
func DOCX(sheet Sheet, opts DOCXOptions) ([]byte, error) {
	if err := validate(sheet); err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)

	write := func(path, body string) error {
		w, err := archive.Create(path)
		if err != nil {
			return errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "creating "+path)
		}
		if _, err = w.Write([]byte(body)); err != nil {
			return errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "writing "+path)
		}
		return nil
	}

	for _, part := range []struct{ path, body string }{
		{"[Content_Types].xml", docxContentTypes},
		{"_rels/.rels", docxPackageRels},
		{"word/document.xml", document(sheet, opts)},
	} {
		if err := write(part.path, part.body); err != nil {
			return nil, err
		}
	}

	if err := archive.Close(); err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeWriteFailed, "closing the document")
	}
	return buffer.Bytes(), nil
}

// DOCXOptions configures the document.
type DOCXOptions struct {
	// Title is the heading above the table. Blank means no heading.
	Title string
	// Subtitle is one line under it — a date range, a company name.
	Subtitle string
	// RightToLeft lays the section, the table and every run out right to left.
	RightToLeft bool
}

const docxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`

const docxPackageRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

func document(sheet Sheet, opts DOCXOptions) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)

	if title := strings.TrimSpace(opts.Title); title != "" {
		paragraph(&b, title, opts.RightToLeft, 32, true)
	}
	if subtitle := strings.TrimSpace(opts.Subtitle); subtitle != "" {
		paragraph(&b, subtitle, opts.RightToLeft, 20, false)
	}

	b.WriteString(`<w:tbl><w:tblPr>`)
	// A visible 1px grid. Without borders Word renders an invisible table, and a statement whose
	// columns are only implied is a statement somebody misreads.
	b.WriteString(`<w:tblBorders>`)
	for _, edge := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		b.WriteString(`<w:` + edge + ` w:val="single" w:sz="4" w:color="D4D4D8"/>`)
	}
	b.WriteString(`</w:tblBorders>`)
	b.WriteString(`<w:tblW w:w="5000" w:type="pct"/>`)
	if opts.RightToLeft {
		b.WriteString(`<w:bidiVisual/>`)
	}
	b.WriteString(`</w:tblPr>`)

	row(&b, sheet.Columns, opts.RightToLeft, true)
	for _, values := range sheet.Rows {
		row(&b, values, opts.RightToLeft, false)
	}
	b.WriteString(`</w:tbl>`)

	b.WriteString(`<w:sectPr>`)
	if opts.RightToLeft {
		b.WriteString(`<w:bidi/>`)
	}
	b.WriteString(`<w:pgSz w:w="11906" w:h="16838"/>`)
	b.WriteString(`<w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134"/>`)
	b.WriteString(`</w:sectPr></w:body></w:document>`)
	return b.String()
}

func row(b *strings.Builder, values []string, rtl, header bool) {
	b.WriteString(`<w:tr>`)
	for _, value := range values {
		b.WriteString(`<w:tc><w:tcPr><w:tcW w:w="0" w:type="auto"/></w:tcPr>`)
		paragraph(b, value, rtl, 20, header)
		b.WriteString(`</w:tc>`)
	}
	b.WriteString(`</w:tr>`)
}

// paragraph writes one paragraph. `half` is the font size in half-points, as Word measures it.
func paragraph(b *strings.Builder, text string, rtl bool, half int, bold bool) {
	b.WriteString(`<w:p><w:pPr>`)
	if rtl {
		b.WriteString(`<w:bidi/><w:jc w:val="right"/>`)
	}
	b.WriteString(`<w:rPr>`)
	if bold {
		b.WriteString(`<w:b/>`)
	}
	if rtl {
		// On the paragraph mark as well as the run: Word uses it for the direction of the
		// paragraph's own end, which is what decides where a trailing number lands.
		b.WriteString(`<w:rtl/>`)
	}
	b.WriteString(`</w:rPr></w:pPr><w:r><w:rPr>`)
	if bold {
		b.WriteString(`<w:b/>`)
	}
	b.WriteString(`<w:sz w:val="` + itoa(half) + `"/><w:szCs w:val="` + itoa(half) + `"/>`)
	if rtl {
		b.WriteString(`<w:rtl/>`)
	}
	b.WriteString(`</w:rPr><w:t xml:space="preserve">`)
	b.WriteString(escapeXML(text))
	b.WriteString(`</w:t></w:r></w:p>`)
}
