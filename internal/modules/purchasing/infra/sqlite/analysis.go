package sqlite

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// PurchasedLine is one posted purchase line, with the document facts an analysis needs.
//
// # Two document types, one row shape
//
// A bill is spend; a supplier return is spend coming back. They live in different tables with
// different date columns, and a UNION here rather than two queries in the service is deliberate:
// it puts the `status = 'posted'` filter and the sign in ONE place. 8.2 found the same thing from
// the other direction — three separate GROUP BY queries would be three places to forget the sign.
type PurchasedLine struct {
	DocumentType   string
	DocumentDate   string
	DocumentNumber string

	PartnerID   id.ID
	PartnerName string

	ProductID   id.ID
	VariantID   id.ID
	VariantSKU  string
	ProductName string

	QuantityMicro int64
	// NetMinor is after discount and BEFORE tax, matching 8.2's revenue.
	//
	// Recoverable tax is not a cost — it is reclaimed — and non-recoverable tax is already in
	// what the goods cost, through the accrual. Adding `tax_amount_minor` here would count the
	// recoverable part as spend.
	NetMinor int64
}

// Sign is +1 for a bill and −1 for a return.
//
// A supplier return's lines carry POSITIVE quantities, exactly as a credit note's do. An analysis
// that summed both would report goods sent back as more goods bought — and a shop that returns a
// bad delivery would read as having spent twice.
func (p PurchasedLine) Sign() int64 {
	if p.DocumentType == "purchasing.return" {
		return -1
	}
	return 1
}

// PurchasedLinesInRange reads every posted bill line and return line in the range.
//
// # Bills, not receipts or orders
//
// An order is an intention and a receipt is goods arriving; neither is money owed. The BILL is
// where the supplier states the price, and a spend report built on orders would count what was
// asked for rather than what was charged — which is the whole reason 6.5 made the bill a separate
// document from the receipt.
func (r *Repos) PurchasedLinesInRange(
	ctx context.Context, companyID id.ID, from, to string,
) ([]PurchasedLine, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT 'purchasing.bill', b.bill_date, COALESCE(b.document_number, ''),
		       b.partner_id, b.partner_name,
		       l.product_id, l.variant_id, l.variant_sku, l.product_name,
		       l.quantity_micro, l.net_minor
		  FROM purchase_bill_lines l
		  JOIN purchase_bills b ON b.id = l.bill_id
		 WHERE b.company_id = ? AND b.status = 'posted'
		   AND b.bill_date >= ? AND b.bill_date <= ?
		UNION ALL
		SELECT 'purchasing.return', s.return_date, COALESCE(s.document_number, ''),
		       s.partner_id, s.partner_name,
		       l.product_id, l.variant_id, l.variant_sku, l.product_name,
		       l.quantity_micro, l.net_minor
		  FROM supplier_return_lines l
		  JOIN supplier_returns s ON s.id = l.return_id
		 WHERE s.company_id = ? AND s.status = 'posted'
		   AND s.return_date >= ? AND s.return_date <= ?
		 ORDER BY 2, 3`,
		string(companyID), from, to, string(companyID), from, to)
	if err != nil {
		return nil, r.wrap(err, "reading purchased lines")
	}
	defer func() { _ = rows.Close() }()

	out := make([]PurchasedLine, 0, 128)
	for rows.Next() {
		var line PurchasedLine
		if err = rows.Scan(&line.DocumentType, &line.DocumentDate, &line.DocumentNumber,
			&line.PartnerID, &line.PartnerName,
			&line.ProductID, &line.VariantID, &line.VariantSKU, &line.ProductName,
			&line.QuantityMicro, &line.NetMinor); err != nil {
			return nil, r.wrap(err, "reading purchased lines")
		}
		out = append(out, line)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "reading purchased lines")
	}
	return out, nil
}
