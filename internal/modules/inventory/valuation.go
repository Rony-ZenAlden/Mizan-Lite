package inventory

import (
	"context"
	"sort"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/inventory/domain"
)

// PermValuationView guards the value of what is on the shelf.
//
// Separate from `PermStockView`, which is quantities. A storeman needs to know there are eleven
// left; what the eleven are worth is a commercial figure, and the two are not the same secret.
const PermValuationView = "inventory.valuation.view"

// Stable codes for the valuation.
const (
	CodeControlMissing = "inventory.control_ledger_missing"
)

// ControlLedger reports what the GENERAL LEDGER says the stock is worth.
//
// # Why inventory asks rather than accounting answering
//
// The same shape Phase 7 used for partner balances, and for the same reason: without it, a
// valuation is a sum of levels that agrees with itself and with nothing else. Inventory cannot
// import accounting (`module-isolation`), so it declares the narrow question it needs answered
// and the composition root satisfies it.
//
// One method, one number. A port that could read the chart of accounts would let inventory start
// making accounting decisions, which is §20.3's rule: when the books must differ, the ACTION
// differs, never the module's knowledge of accounts.
type ControlLedger interface {
	// StockValueMinor returns what the inventory control account holds, debit-positive.
	StockValueMinor(ctx context.Context, companyID id.ID) (int64, error)
}

// ValuedLine is one variant's stock in one warehouse, with what it is worth.
type ValuedLine struct {
	WarehouseID   id.ID
	WarehouseName string
	ProductID     id.ID
	VariantID     id.ID
	VariantSKU    string
	ProductName   string

	QuantityMicro int64
	// AvgCostMicro is the moving average this stock is carried at, per unit.
	AvgCostMicro int64
	ValueMinor   int64
}

// Valuation is what the whole company's stock is worth, and whether the books agree.
type Valuation struct {
	Lines      []ValuedLine
	TotalMinor int64

	// LedgerMinor is what the general ledger's stock control account holds. Absent when no
	// control ledger is attached, which is a build without accounting rather than a failure.
	LedgerMinor int64
	HasLedger   bool

	// DifferenceMinor is the valuation less the ledger. Zero is the only good answer.
	//
	// REPORTED, never repaired — 4.6's rule and 7.4 D3's, arrived at independently in three
	// modules now. A difference means either a movement that posted nothing or a posting with no
	// movement, and both need a person to look at the evidence rather than a job to erase it.
	DifferenceMinor int64
}

// Valuation reports what is on the shelf and what it is worth.
//
// # Why this reads levels and not movements
//
// `stock_levels` carries `avg_cost_micro`, maintained by the same costing the ledger's entries
// were computed from, and `VerifyLedger` already proves the levels reconcile to the movements
// (4.6). Recomputing from the movement history here would be a SECOND costing implementation —
// which is exactly what 8.2 declined to write in SQL, for the reason Phase 6 paid for.
func (s *Service) Valuation(ctx context.Context, companyID id.ID) (Valuation, error) {
	levels, err := s.repos.ValuedLevels(ctx, companyID)
	if err != nil {
		return Valuation{}, err
	}

	valuation := Valuation{Lines: make([]ValuedLine, 0, len(levels))}
	for _, level := range levels {
		// The value uses the SAME function the movements were valued with. A shorter
		// `quantity * cost / scale` here would be a third implementation of a conversion Phase 6
		// found wrong in its second.
		line := ValuedLine{
			WarehouseID: level.WarehouseID, WarehouseName: level.WarehouseName,
			ProductID: level.ProductID, VariantID: level.VariantID,
			VariantSKU: level.VariantSKU, ProductName: level.ProductName,
			QuantityMicro: level.QuantityMicro, AvgCostMicro: level.AvgCostMicro,
			ValueMinor: domain.ValueOf(level.QuantityMicro, level.AvgCostMicro, level.Decimals),
		}
		valuation.TotalMinor += line.ValueMinor
		valuation.Lines = append(valuation.Lines, line)
	}

	// Largest value first: the question "what is my money tied up in" is a ranking. Ties break on
	// the SKU so two runs of the same report agree.
	sort.SliceStable(valuation.Lines, func(i, j int) bool {
		if valuation.Lines[i].ValueMinor != valuation.Lines[j].ValueMinor {
			return valuation.Lines[i].ValueMinor > valuation.Lines[j].ValueMinor
		}
		return valuation.Lines[i].VariantSKU < valuation.Lines[j].VariantSKU
	})

	if s.control != nil {
		ledger, ledgerErr := s.control.StockValueMinor(ctx, companyID)
		if ledgerErr != nil {
			return Valuation{}, ledgerErr
		}
		valuation.HasLedger = true
		valuation.LedgerMinor = ledger
		valuation.DifferenceMinor = valuation.TotalMinor - ledger
	}
	return valuation, nil
}

// VerifyValuation is the valuation reduced to its one important number.
//
// For a scheduled check that wants an answer rather than a report. It REFUSES when no control
// ledger is attached, rather than returning zero: "the books agree" and "nobody asked the books"
// must not look the same to a job that only reports failures.
func (s *Service) VerifyValuation(ctx context.Context, companyID id.ID) (int64, error) {
	if s.control == nil {
		return 0, errs.Internal(CodeControlMissing,
			"this build cannot compare the stock valuation with the ledger")
	}
	valuation, err := s.Valuation(ctx, companyID)
	if err != nil {
		return 0, err
	}
	return valuation.DifferenceMinor, nil
}

// AttachControlLedger gives inventory the ledger side of its valuation check.
//
// Set after construction for the same reason partner's ledgers are (7.4 D2): the composition root
// builds accounting and inventory in an order, and whichever is second cannot be a constructor
// argument to the first without a cycle.
func (s *Service) AttachControlLedger(control ControlLedger) { s.control = control }
