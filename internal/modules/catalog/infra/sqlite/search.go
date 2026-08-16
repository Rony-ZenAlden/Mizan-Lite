package sqlite

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/search"
)

// SearchProducts finds products by code, name, or variant SKU.
//
// # The rank is the point
//
// A shopkeeper with a scanner searches by SKU or code, and both are exact — so a row matching one
// of them is what they meant, while a row whose NAME happens to contain the same characters is a
// coincidence. Without the rank the coincidence appears above the answer as often as not.
//
// DISTINCT on the product, because a product with six variants would otherwise appear six times
// for a query matching its name — and the six rows all open the same screen.
func (r *Repos) SearchProducts(
	ctx context.Context, companyID id.ID, query string, limit int,
) ([]search.Result, error) {
	pattern := search.Like(query)

	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT p.id, p.name, COALESCE(p.code, ''),
		       MIN(CASE
		             WHEN LOWER(p.code) LIKE ? ESCAPE '\' THEN 0
		             WHEN LOWER(v.sku)  LIKE ? ESCAPE '\' THEN 1
		             ELSE 2
		           END)
		  FROM products p
		  LEFT JOIN product_variants v ON v.product_id = p.id
		 WHERE p.company_id = ? AND p.is_active = 1
		   AND (LOWER(p.code) LIKE ? ESCAPE '\'
		     OR LOWER(p.name) LIKE ? ESCAPE '\'
		     OR LOWER(v.sku)  LIKE ? ESCAPE '\')
		 GROUP BY p.id, p.name, p.code
		 ORDER BY 4, p.name
		 LIMIT ?`,
		pattern, pattern, string(companyID), pattern, pattern, pattern, limit)
	if err != nil {
		return nil, r.wrap(err, "searching products")
	}
	defer func() { _ = rows.Close() }()

	out := make([]search.Result, 0, limit)
	for rows.Next() {
		var (
			result search.Result
			code   string
		)
		if err = rows.Scan(&result.ID, &result.Label, &code, &result.Rank); err != nil {
			return nil, r.wrap(err, "searching products")
		}
		result.Kind = "catalog.product"
		result.Subtitle = code
		out = append(out, result)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "searching products")
	}
	return out, nil
}
