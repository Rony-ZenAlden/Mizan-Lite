package domain

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// Basis is how a landed cost spreads across the goods it brought in.
type Basis string

// The allocation bases.
//
// `Weight` and `Volume` are named but not yet computable: they need product dimensions the
// catalog does not carry. Declared now so that adding them is a value change rather than a schema
// change (§D.4's pattern), and REFUSED at runtime rather than silently falling back to another
// basis — a freight charge spread by value when the operator asked for weight is wrong in a way
// nobody would ever notice.
const (
	ByValue    Basis = "value"
	ByQuantity Basis = "quantity"
	ByWeight   Basis = "weight"
	ByVolume   Basis = "volume"
)

// Supported reports whether this build can allocate on a basis.
func (b Basis) Supported() bool { return b == ByValue || b == ByQuantity }

// LandedCostStatus is where a charge has got to.
type LandedCostStatus string

// The landed cost statuses.
const (
	LandedDraft     LandedCostStatus = "draft"
	LandedApplied   LandedCostStatus = "applied"
	LandedCancelled LandedCostStatus = "cancelled"
)

// LandedCost is a charge that belongs to what the stock cost.
type LandedCost struct {
	ID          id.ID
	CompanyID   id.ID
	ReceiptID   id.ID
	ChargeType  string
	Description string
	PartnerID   id.ID
	Basis       Basis

	CurrencyCode string
	AmountMinor  int64

	Status LandedCostStatus
}

// NewLandedCost builds a charge, or refuses.
func NewLandedCost(
	identifier, companyID, receiptID id.ID,
	chargeType, currencyCode string, basis Basis, amountMinor int64,
) (LandedCost, error) {
	chargeType = strings.TrimSpace(chargeType)

	if identifier.IsZero() || companyID.IsZero() || receiptID.IsZero() {
		return LandedCost{}, errs.Validation(CodeInvalidLandedCost,
			"a landed cost needs an identity, a company, and a delivery")
	}
	if chargeType == "" {
		return LandedCost{}, errs.Validation(CodeInvalidLandedCost,
			"a landed cost needs to say what kind of charge it is")
	}
	if amountMinor <= 0 {
		// A charge of nothing is not a charge, and a NEGATIVE one is a credit note from the
		// carrier — a different document with a different effect on what is owed.
		return LandedCost{}, errs.Validation(CodeInvalidLandedCost,
			"a landed cost must be for a positive amount")
	}
	if basis == "" {
		basis = ByValue
	}
	if !basis.Supported() {
		return LandedCost{}, errs.Validation(CodeUnsupportedBasis,
			"this build cannot allocate on that basis yet").WithParam("basis", string(basis))
	}

	return LandedCost{
		ID: identifier, CompanyID: companyID, ReceiptID: receiptID,
		ChargeType: chargeType, Basis: basis, Status: LandedDraft,
		CurrencyCode: strings.ToUpper(currencyCode), AmountMinor: amountMinor,
	}, nil
}

// LandedAllocation is one line's share of a charge.
type LandedAllocation struct {
	ReceiptLineID id.ID
	WeightMicro   int64
	AmountMinor   int64
	MovementID    id.ID
}

// AllocateLandedCost spreads a charge across the lines it brought in.
//
// # Why the weights come from the caller
//
// The basis decides what to weigh by, and only the caller has the receipt in front of it. This
// function's job is the SPLIT, which is where the arithmetic goes wrong — §D.3's trap 4: naive
// proportional allocation leaves the total off by a few minor units on every shipment, and those
// units land in nobody's account.
//
// The kernel's largest-remainder allocator is the mechanism, and Phase 6 is the fourth caller
// that needed one — which is why it now lives in the kernel rather than in three copies.
func AllocateLandedCost(
	amountMinor int64, lines []ReceiptLine, basis Basis,
) ([]LandedAllocation, error) {
	if len(lines) == 0 {
		return nil, errs.Validation(CodeInvalidLandedCost,
			"that delivery has no lines to spread a charge across")
	}
	if !basis.Supported() {
		return nil, errs.Validation(CodeUnsupportedBasis,
			"this build cannot allocate on that basis yet").WithParam("basis", string(basis))
	}

	weights := make([]int64, len(lines))
	for i, line := range lines {
		switch basis {
		case ByQuantity:
			weights[i] = line.QuantityStockMicro
		default:
			weights[i] = line.ValueMinor
		}
		if weights[i] < 0 {
			weights[i] = 0
		}
	}

	parts, err := round.Allocate(amountMinor, weights)
	if err != nil {
		return nil, errs.Validation(CodeInvalidLandedCost, err.Error())
	}

	out := make([]LandedAllocation, 0, len(lines))
	for i, line := range lines {
		out = append(out, LandedAllocation{
			ReceiptLineID: line.ID, WeightMicro: weights[i], AmountMinor: parts[i],
		})
	}
	return out, nil
}
