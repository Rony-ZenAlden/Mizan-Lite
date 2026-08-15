package sqlite

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// ValuedLevel is one stock level with everything needed to value and label it.
//
// # The currency's decimals come with the row
//
// `ValueOf` needs the functional currency's scale, and reading it per row rather than per report
// is deliberate: it is the fact Phase 6.6's defect turned on, and a caller that has to fetch it
// separately is a caller that can forget to.
type ValuedLevel struct {
	WarehouseID   id.ID
	WarehouseName string
	ProductID     id.ID
	VariantID     id.ID
	VariantSKU    string
	ProductName   string

	QuantityMicro int64
	AvgCostMicro  int64
	Decimals      int
}

// ValuedLevels reads every level a company holds, with its names and its currency's scale.
//
// Levels at ZERO are excluded. A shop that has ever stocked a thousand products has a level row
// for each, and a valuation listing nine hundred lines worth nothing buries the ninety that
// matter. `VerifyLedger` is what covers the empty ones, and it reads every row.
func (r *Repos) ValuedLevels(ctx context.Context, companyID id.ID) ([]ValuedLevel, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT l.warehouse_id, w.name, l.product_id, l.variant_id,
		       v.sku, p.name, l.qty_on_hand_micro, l.avg_cost_micro,
		       c.decimal_places
		  FROM stock_levels l
		  JOIN warehouses w       ON w.id = l.warehouse_id
		  JOIN product_variants v ON v.id = l.variant_id
		  JOIN products p         ON p.id = l.product_id
		  JOIN companies co       ON co.id = l.company_id
		  JOIN currencies c       ON c.code = co.functional_currency
		 WHERE l.company_id = ? AND l.qty_on_hand_micro <> 0
		 ORDER BY p.name, v.sku`, string(companyID))
	if err != nil {
		return nil, r.wrap(err, "reading stock levels for valuation")
	}
	defer func() { _ = rows.Close() }()

	out := make([]ValuedLevel, 0, 64)
	for rows.Next() {
		var level ValuedLevel
		if err = rows.Scan(&level.WarehouseID, &level.WarehouseName, &level.ProductID,
			&level.VariantID, &level.VariantSKU, &level.ProductName,
			&level.QuantityMicro, &level.AvgCostMicro, &level.Decimals); err != nil {
			return nil, r.wrap(err, "reading stock levels for valuation")
		}
		out = append(out, level)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "reading stock levels for valuation")
	}
	return out, nil
}
