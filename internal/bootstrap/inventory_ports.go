package bootstrap

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/inventory"
)

// inventoryControl is what the GENERAL LEDGER says the stock is worth.
//
// # The other side of the stock valuation
//
// Two completely different paths to one number. `Valuation` multiplies each level's quantity by
// the average cost the costing engine maintained; the ledger sums journal lines written by
// posting rules, from movement values computed at the moment each movement happened. They agree
// only if every movement that changed the stock also posted, and posted the same figure.
//
// When they disagree, nothing else in the system will say so. Phase 6.6 is the argument: a
// costing conversion was wrong by a factor of a hundred for two phases while every level, every
// movement and every journal entry stayed internally consistent.
type inventoryControl struct{ accounting *accounting.Service }

var _ inventory.ControlLedger = inventoryControl{}

func (c inventoryControl) StockValueMinor(
	ctx context.Context, companyID id.ID,
) (int64, error) {
	// INVENTORY is the mapping every stock-moving posting rule already names (0010). Asking for
	// the MAPPING rather than an account code is what lets a company that renumbered its chart
	// still be checked — and it is §20.3's rule: this port must not know account numbers.
	return c.accounting.BalanceOfMapping(ctx, companyID, "INVENTORY")
}
