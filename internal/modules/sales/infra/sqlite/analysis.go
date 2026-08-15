package sqlite

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/sales/domain"
)

// SoldLine is one posted line, with the document facts an analysis needs to place it.
//
// # Why the lines come back RAW rather than aggregated in SQL
//
// The obvious query sums `quantity_stock_micro * cost_micro` per product and returns the total.
// It would be a SECOND implementation of `domain.CostOfLine`, in a language with different
// integer division, rounding at a different point.
//
// Phase 6 found what that costs. `valueOf` produced values in major units from fields named
// `…Minor` for two phases, invisible because every consuming test used a zero-decimal currency.
// The defect was not the arithmetic being hard; it was two places believing they agreed.
//
// So the rows come back and the arithmetic happens once, in the function the document itself
// used. A year of a shop's sales is tens of thousands of lines, which is nothing to sum in Go —
// and if it ever is, the answer is an index, then a projection with a verifier, in that order.
type SoldLine struct {
	Line domain.Line

	DocumentType   string
	DocumentDate   string
	DocumentNumber string
	PartnerID      id.ID
	PartnerName    string
	CurrencyCode   string
}

// Sign is +1 for an invoice and −1 for a credit note.
//
// # The single most important line in this file
//
// A credit note's lines carry POSITIVE quantities — `NewLine` refuses a negative one, because a
// return is a document rather than a negative row. So an analysis that summed both document
// types would report a returned sale as two sales, and a shop with heavy returns would read as
// its own best month.
//
// The sign lives here, next to the query, so no caller can forget it and no caller can apply it
// twice.
func (s SoldLine) Sign() int64 {
	if s.DocumentType == string(domain.CreditNote) {
		return -1
	}
	return 1
}

// SoldLinesInRange reads every posted sale line whose document falls in the range.
//
// Drafts and cancelled documents are excluded by `status = 'posted'`. A draft is not a sale, and
// an analysis that counted one would report revenue nobody has agreed to — which is the same
// filter every document query in this codebase applies and the one an aggregate is most likely
// to leave out.
func (r *Repos) SoldLinesInRange(
	ctx context.Context, companyID id.ID, from, to string,
) ([]SoldLine, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT l.id, l.line_number, l.product_id, l.variant_id, l.product_name,
		       l.variant_sku, l.uom_id, l.uom_code,
		       l.quantity_micro, l.quantity_stock_micro,
		       l.unit_price_minor, l.discount_minor, l.tax_amount_minor,
		       l.net_minor, l.total_minor, l.cost_micro,
		       d.document_type, d.document_date, COALESCE(d.document_number, ''),
		       COALESCE(d.partner_id, ''), COALESCE(d.partner_name, ''), d.currency_code
		  FROM sales_lines l
		  JOIN sales_documents d ON d.id = l.document_id
		 WHERE d.company_id = ?
		   AND d.status = 'posted'
		   AND d.document_type IN ('invoice', 'credit_note')
		   AND d.document_date >= ? AND d.document_date <= ?
		 ORDER BY d.document_date, d.id, l.line_number`,
		string(companyID), from, to)
	if err != nil {
		return nil, r.wrap(err, "reading sold lines")
	}
	defer func() { _ = rows.Close() }()

	out := make([]SoldLine, 0, 128)
	for rows.Next() {
		var sold SoldLine
		if err = rows.Scan(
			&sold.Line.ID, &sold.Line.LineNumber, &sold.Line.ProductID, &sold.Line.VariantID,
			&sold.Line.ProductName, &sold.Line.VariantSKU, &sold.Line.UomID, &sold.Line.UomCode,
			&sold.Line.QuantityMicro, &sold.Line.QuantityStockMicro,
			&sold.Line.UnitPriceMinor, &sold.Line.DiscountMinor, &sold.Line.TaxAmountMinor,
			&sold.Line.NetMinor, &sold.Line.TotalMinor, &sold.Line.CostMicro,
			&sold.DocumentType, &sold.DocumentDate, &sold.DocumentNumber,
			&sold.PartnerID, &sold.PartnerName, &sold.CurrencyCode,
		); err != nil {
			return nil, r.wrap(err, "reading sold lines")
		}
		out = append(out, sold)
	}
	return out, r.wrapIfErr(rows.Err(), "reading sold lines")
}

func (r *Repos) wrapIfErr(err error, what string) error {
	if err == nil {
		return nil
	}
	return r.wrap(err, what)
}
