package sqlite

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/search"
)

// SearchBills finds posted bills by our number, the SUPPLIER's invoice number, or partner name.
//
// # The supplier's own number is the one people quote
//
// A supplier chasing payment says "invoice A-3391", which is their number, not ours. 6.5 stored
// `supplier_invoice_number` for exactly that conversation, and a search that matched only our own
// numbering would fail the call it was built for.
func (r *Repos) SearchBills(
	ctx context.Context, companyID id.ID, query string, limit int,
) ([]search.Result, error) {
	pattern := search.Like(query)

	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, COALESCE(document_number, ''), supplier_invoice_number,
		       partner_name, bill_date,
		       CASE
		         WHEN LOWER(COALESCE(document_number, '')) LIKE ? ESCAPE '\' THEN 0
		         WHEN LOWER(supplier_invoice_number)       LIKE ? ESCAPE '\' THEN 0
		         ELSE 1
		       END
		  FROM purchase_bills
		 WHERE company_id = ? AND status = 'posted'
		   AND (LOWER(COALESCE(document_number, '')) LIKE ? ESCAPE '\'
		     OR LOWER(supplier_invoice_number)       LIKE ? ESCAPE '\'
		     OR LOWER(partner_name)                  LIKE ? ESCAPE '\')
		 ORDER BY 6, bill_date DESC
		 LIMIT ?`,
		pattern, pattern, string(companyID), pattern, pattern, pattern, limit)
	if err != nil {
		return nil, r.wrap(err, "searching bills")
	}
	defer func() { _ = rows.Close() }()

	out := make([]search.Result, 0, limit)
	for rows.Next() {
		var (
			result                       search.Result
			number, supplierRef, partner string
			billDate                     string
		)
		if err = rows.Scan(&result.ID, &number, &supplierRef, &partner,
			&billDate, &result.Rank); err != nil {
			return nil, r.wrap(err, "searching bills")
		}
		result.Kind = "purchasing.bill"
		result.Label = number
		// The supplier's number goes in the subtitle, because it is what the caller quoted and
		// seeing it is how they know they found the right one.
		result.Subtitle = billDate + " · " + partner
		if supplierRef != "" {
			result.Subtitle += " · " + supplierRef
		}
		out = append(out, result)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "searching bills")
	}
	return out, nil
}
