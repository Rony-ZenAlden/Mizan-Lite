package bindings

import (
	"encoding/base64"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/platform/tabular"
)

// ExportDTO is a file the frontend saves.
//
// # Why base64 and not a path
//
// The alternative was writing the file and returning where it went. That means this process
// choosing a directory on the user's machine — which is a permission question on macOS, a
// different directory on Windows, and a surprise on both. Handing the BYTES back lets the webview
// use the platform's own save dialog, which is the one the user already knows.
//
// Base64 because the Wails boundary is JSON. A megabyte of CSV becomes 1.4MB of string, which is
// nothing next to a save dialog the user is looking at.
type ExportDTO struct {
	Filename string `json:"filename"`
	// MimeType is what the save dialog should offer.
	MimeType string `json:"mimeType"`
	// ContentBase64 is the file.
	ContentBase64 string `json:"contentBase64"`
	// Rows is how many data rows it holds, so a screen can say "exported 412 rows" rather than
	// leaving the user to open the file to find out it was empty.
	Rows int `json:"rows"`
}

// exportSheet renders a sheet and wraps it for the boundary.
//
// # The columns are the CALLER's
//
// This function receives a finished sheet. It does not query, does not know what a report is, and
// cannot re-derive a column — which is DoD criterion 7: **every export's columns are the report's
// own, and no export re-queries.** A second query would be a second answer, and the user would
// have a spreadsheet that disagrees with the screen it came from.
func exportSheet(sheet tabular.Sheet, report, from, to string) envelope.Result[ExportDTO] {
	content, err := tabular.CSV(sheet)
	if err != nil {
		return envelope.Fail[ExportDTO](err)
	}
	return envelope.Ok(ExportDTO{
		Filename:      tabular.Filename(report, from, to),
		MimeType:      "text/csv;charset=utf-8",
		ContentBase64: base64.StdEncoding.EncodeToString(content),
		Rows:          len(sheet.Rows),
	})
}

// ExportAnalysis renders a sales or spend analysis as a spreadsheet.
//
// The DTO the SCREEN was given is what gets exported — not a fresh query with the same arguments.
// That is what makes the file and the screen agree by construction rather than by coincidence.
func (i *Insight) ExportAnalysis(
	analysis AnalysisDTO, title string, withMargin bool,
) envelope.Result[ExportDTO] {
	if _, _, err := i.guard("ExportAnalysis"); err != nil {
		return envelope.Fail[ExportDTO](err)
	}

	columns := []string{"key", "label", "documents", "quantityMicro", "revenueMinor"}
	if withMargin {
		columns = append(columns, "costMinor", "grossMarginMinor", "marginPercentMicro")
	}

	rows := make([][]string, 0, len(analysis.Rows)+1)
	for _, row := range analysis.Rows {
		values := []string{
			row.Key, row.Label, itoaInt(row.Documents), row.QuantityMicro, row.RevenueMinor,
		}
		if withMargin {
			values = append(values, row.CostMinor, row.GrossMarginMinor, row.MarginPercentMicro)
		}
		rows = append(rows, values)
	}

	// The TOTAL is a row, because a spreadsheet the user sums themselves will disagree with the
	// screen the moment a report is filtered — and because the backend's total is the one the
	// report already computed.
	total := []string{"", "TOTAL", itoaInt(analysis.Total.Documents),
		analysis.Total.QuantityMicro, analysis.Total.RevenueMinor}
	if withMargin {
		total = append(total, analysis.Total.CostMinor, analysis.Total.GrossMarginMinor,
			analysis.Total.MarginPercentMicro)
	}
	rows = append(rows, total)

	return exportSheet(tabular.Sheet{Columns: columns, Rows: rows},
		title, analysis.From, analysis.To)
}

// ExportValuation renders a stock valuation as a spreadsheet.
func (i *Insight) ExportValuation(valuation ValuationDTO) envelope.Result[ExportDTO] {
	if _, _, err := i.guard("ExportValuation"); err != nil {
		return envelope.Fail[ExportDTO](err)
	}

	rows := make([][]string, 0, len(valuation.Lines))
	for _, line := range valuation.Lines {
		rows = append(rows, []string{
			line.ProductName, line.VariantSKU, line.WarehouseName,
			line.QuantityMicro, line.AvgCostMicro, line.ValueMinor,
		})
	}
	return exportSheet(tabular.Sheet{
		Columns: []string{
			"productName", "variantSku", "warehouseName",
			"quantityMicro", "avgCostMicro", "valueMinor",
		},
		Rows: rows,
	}, "stock-valuation", "", "")
}

// ExportStatement renders a profit and loss or balance sheet section tree.
//
// FLATTENED with a depth column, unlike the DTO. A spreadsheet has no nesting, and a reader
// indents on the depth — which is why the depth crosses the boundary at all.
func (i *Insight) ExportStatement(
	nodes []StatementNodeDTO, title string, from, to string,
) envelope.Result[ExportDTO] {
	if _, _, err := i.guard("ExportStatement"); err != nil {
		return envelope.Fail[ExportDTO](err)
	}

	rows := make([][]string, 0, len(nodes)*4)
	var walk func(node StatementNodeDTO)
	walk = func(node StatementNodeDTO) {
		rows = append(rows, []string{
			itoaInt(node.Depth), node.Code, node.Name, node.Type, node.AmountMinor,
		})
		for _, child := range node.Children {
			walk(child)
		}
	}
	for _, node := range nodes {
		walk(node)
	}

	return exportSheet(tabular.Sheet{
		Columns: []string{"depth", "code", "name", "type", "amountMinor"},
		Rows:    rows,
	}, title, from, to)
}

func itoaInt(n int) string {
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
