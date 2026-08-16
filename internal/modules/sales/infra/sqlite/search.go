package sqlite

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/search"
)

// SearchDocuments finds posted sales documents by number or partner name.
//
// # POSTED only, and that is not a performance choice
//
// A draft has no number, so there is nothing to search it by; and a draft is a screen somebody
// has open rather than a record of anything. What an operator means by "find invoice 4471" is
// always a document that exists.
func (r *Repos) SearchDocuments(
	ctx context.Context, companyID id.ID, query string, limit int,
) ([]search.Result, error) {
	pattern := search.Like(query)

	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, document_type, COALESCE(document_number, ''),
		       COALESCE(partner_name, ''), document_date, total_minor,
		       CASE WHEN LOWER(COALESCE(document_number, '')) LIKE ? ESCAPE '\'
		            THEN 0 ELSE 1 END
		  FROM sales_documents
		 WHERE company_id = ? AND status = 'posted'
		   AND (LOWER(COALESCE(document_number, '')) LIKE ? ESCAPE '\'
		     OR LOWER(COALESCE(partner_name, ''))    LIKE ? ESCAPE '\')
		 ORDER BY 7, document_date DESC
		 LIMIT ?`,
		pattern, string(companyID), pattern, pattern, limit)
	if err != nil {
		return nil, r.wrap(err, "searching sales documents")
	}
	defer func() { _ = rows.Close() }()

	out := make([]search.Result, 0, limit)
	for rows.Next() {
		var (
			result                         search.Result
			kind, number, partner, docDate string
			totalMinor                     int64
		)
		if err = rows.Scan(&result.ID, &kind, &number, &partner, &docDate,
			&totalMinor, &result.Rank); err != nil {
			return nil, r.wrap(err, "searching sales documents")
		}
		result.Kind = "sales." + kind
		result.Label = number
		// The DATE and the PARTNER, not the amount. Money crosses the JS boundary as a string of
		// minor units (§E) and a subtitle assembled here would have to format it — which needs a
		// currency and a locale this query has neither of.
		result.Subtitle = docDate
		if partner != "" {
			result.Subtitle += " · " + partner
		}
		out = append(out, result)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "searching sales documents")
	}
	return out, nil
}
