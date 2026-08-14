package domain

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// ReturnStatus is where a supplier return has got to.
type ReturnStatus string

// The return statuses.
const (
	ReturnDraft     ReturnStatus = "draft"
	ReturnPosted    ReturnStatus = "posted"
	ReturnCancelled ReturnStatus = "cancelled"
)

// SupplierReturn is goods going back, and the debit note that says so.
type SupplierReturn struct {
	ID          id.ID
	CompanyID   id.ID
	BranchID    id.ID
	WarehouseID id.ID

	Status      ReturnStatus
	Number      string
	PartnerID   id.ID
	PartnerName string
	ReturnDate  string

	Reason            string
	SupplierReference string
	CurrencyCode      string

	NetMinor   int64
	TaxMinor   int64
	TotalMinor int64
	// CostMinor is what the goods cost US, at the original delivery's figure. It differs from
	// the net whenever a bill corrected the price after delivery (6.4), and the difference is a
	// real gain or loss on the return rather than an error.
	CostMinor int64
}

// NewSupplierReturn builds a draft return, or refuses.
func NewSupplierReturn(
	identifier, companyID, branchID, warehouseID, partnerID id.ID,
	partnerName, returnDate, currencyCode string,
) (SupplierReturn, error) {
	partnerName = strings.TrimSpace(partnerName)

	if identifier.IsZero() || companyID.IsZero() || branchID.IsZero() || warehouseID.IsZero() {
		return SupplierReturn{}, errs.Validation(CodeInvalidReturn,
			"a return needs an identity, a company, a branch, and a warehouse")
	}
	if partnerID.IsZero() || partnerName == "" {
		return SupplierReturn{}, errs.Validation(CodeNoSupplier,
			"a return needs to say who the goods are going back to")
	}
	if strings.TrimSpace(returnDate) == "" {
		return SupplierReturn{}, errs.Validation(CodeInvalidReturn, "a return needs a date")
	}

	return SupplierReturn{
		ID: identifier, CompanyID: companyID, BranchID: branchID, WarehouseID: warehouseID,
		Status: ReturnDraft, PartnerID: partnerID, PartnerName: partnerName,
		ReturnDate: returnDate, CurrencyCode: strings.ToUpper(currencyCode),
	}, nil
}

// RequireDraft refuses to change a posted return.
func (r SupplierReturn) RequireDraft() error {
	switch r.Status {
	case ReturnDraft:
		return nil
	case ReturnPosted:
		return errs.Conflict(CodeReturnPosted,
			"this return has been posted; the goods have already gone")
	default:
		return errs.Conflict(CodeCancelled, "this return was cancelled")
	}
}

// ReturnLine is one thing going back.
type ReturnLine struct {
	ID            id.ID
	LineNumber    int
	ReceiptLineID id.ID
	ProductID     id.ID
	VariantID     id.ID

	ProductName string
	VariantSKU  string
	UomCode     string

	QuantityMicro      int64
	QuantityStockMicro int64

	UnitPriceMicro int64
	UnitCostMicro  int64
	TaxRateMicro   int64
	TaxCode        string
	TaxAmountMinor int64
	NetMinor       int64
	CostMinor      int64
	TotalMinor     int64

	MovementID id.ID
	Notes      string
}

// RequireReturnable bounds a return by what actually arrived and has not gone back already.
//
// # Both bounds are needed, and for different reasons
//
// Returning more than arrived is a debit note for goods the supplier never sent — the mirror of
// being invoiced for goods that never arrived, and just as much a way for money to move for
// nothing.
//
// Returning the same goods twice is the subtler one: each return looks reasonable on its own, and
// only the running total shows that fourteen of ten have gone back. It is the same shape as the
// over-receipt rule (6.2), measured against what REMAINS rather than what arrived.
func RequireReturnable(receivedMicro, alreadyReturnedMicro, returningMicro int64) error {
	if returningMicro <= 0 {
		return errs.Validation(CodeNonPositiveQty,
			"a return must send back a positive quantity")
	}

	remaining := receivedMicro - alreadyReturnedMicro
	if remaining <= 0 {
		return errs.Conflict(CodeNothingToReturn,
			"everything from that delivery line has already gone back").
			WithParam("received", itoa(receivedMicro))
	}
	if returningMicro > remaining {
		return errs.Conflict(CodeTooMuchReturned,
			"that is more than is left of what arrived").
			WithParam("returning", itoa(returningMicro)).
			WithParam("remaining", itoa(remaining))
	}
	return nil
}

// Price sets what the supplier is being debited, and what the goods cost us.
func (l ReturnLine) Price(
	unitPriceMicro, unitCostMicro int64, decimals int,
) (ReturnLine, error) {
	if unitPriceMicro < 0 || unitCostMicro < 0 {
		return ReturnLine{}, errs.Validation(CodeNegativePrice,
			"a return cannot carry a negative price or cost")
	}

	l.UnitPriceMicro = unitPriceMicro
	l.UnitCostMicro = unitCostMicro
	l.NetMinor = grossMinor(l.QuantityMicro, unitPriceMicro, decimals)
	// The COST is computed on the STOCK quantity, because that is what leaves the shelf — the
	// two differ whenever the return is entered in a unit other than the stock one, and using
	// the wrong one is a valuation error scaled by the conversion factor.
	l.CostMinor = grossMinor(l.QuantityStockMicro, unitCostMicro, decimals)
	l.TotalMinor = l.NetMinor + l.TaxAmountMinor
	return l, nil
}

// Tax applies the tax being given back.
func (l ReturnLine) Tax(amountMinor, rateMicro int64, code string) ReturnLine {
	l.TaxAmountMinor = amountMinor
	l.TaxRateMicro = rateMicro
	l.TaxCode = code
	l.TotalMinor = l.NetMinor + amountMinor
	return l
}

// ReturnTotals recomputes a return's money from its lines.
func ReturnTotals(lines []ReturnLine) (net, tax, total, cost int64) {
	for _, line := range lines {
		net += line.NetMinor
		tax += line.TaxAmountMinor
		total += line.TotalMinor
		cost += line.CostMinor
	}
	return net, tax, total, cost
}
