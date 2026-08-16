package sqlite

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/search"
)

// SearchPartners finds partners by code, name, phone, or tax number.
//
// # Phone is included on purpose
//
// It is how a shop actually identifies a returning customer: they give a number, not a code. A
// search that matched only names would send the operator to the customer list to scroll.
func (r *Repos) SearchPartners(
	ctx context.Context, companyID id.ID, query string, limit int,
) ([]search.Result, error) {
	pattern := search.Like(query)

	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, name, COALESCE(code, ''), COALESCE(phone, ''),
		       CASE
		         WHEN LOWER(code)  LIKE ? ESCAPE '\' THEN 0
		         WHEN LOWER(phone) LIKE ? ESCAPE '\' THEN 1
		         ELSE 2
		       END
		  FROM partners
		 WHERE company_id = ? AND is_active = 1
		   AND (LOWER(code)       LIKE ? ESCAPE '\'
		     OR LOWER(name)       LIKE ? ESCAPE '\'
		     OR LOWER(phone)      LIKE ? ESCAPE '\'
		     OR LOWER(COALESCE(tax_number, '')) LIKE ? ESCAPE '\')
		 ORDER BY 5, name
		 LIMIT ?`,
		pattern, pattern, string(companyID), pattern, pattern, pattern, pattern, limit)
	if err != nil {
		return nil, r.wrap(err, "searching partners")
	}
	defer func() { _ = rows.Close() }()

	out := make([]search.Result, 0, limit)
	for rows.Next() {
		var (
			result      search.Result
			code, phone string
		)
		if err = rows.Scan(&result.ID, &result.Label, &code, &phone, &result.Rank); err != nil {
			return nil, r.wrap(err, "searching partners")
		}
		result.Kind = "partner.partner"
		// Both, when there are both: a shop with four customers called Mohammed needs the phone
		// number to tell them apart, and the code alone will not do it.
		result.Subtitle = code
		if phone != "" {
			if result.Subtitle != "" {
				result.Subtitle += " · "
			}
			result.Subtitle += phone
		}
		out = append(out, result)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "searching partners")
	}
	return out, nil
}
