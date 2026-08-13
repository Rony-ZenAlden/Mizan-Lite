package domain

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// ReceiptStatus is where a delivery has got to.
type ReceiptStatus string

// The receipt statuses.
const (
	ReceiptDraft     ReceiptStatus = "draft"
	ReceiptConfirmed ReceiptStatus = "confirmed"
	ReceiptCancelled ReceiptStatus = "cancelled"
)

// Receipt is what actually arrived.
type Receipt struct {
	ID          id.ID
	CompanyID   id.ID
	BranchID    id.ID
	WarehouseID id.ID
	// OrderID is zero where goods arrived against no order — a replacement for damaged stock, or
	// a cash-and-carry purchase. A real delivery with a real cost and one fewer side to match.
	OrderID id.ID

	Status      ReceiptStatus
	Number      string
	PartnerID   id.ID
	PartnerName string
	ReceiptDate string

	DeliveryNoteReference string
	ReceivedByName        string

	CurrencyCode string
	ValueMinor   int64

	BilledAt string
	BillID   id.ID
	Notes    string
}

// NewReceipt builds a draft receipt, or refuses.
func NewReceipt(
	identifier, companyID, branchID, warehouseID, partnerID id.ID,
	partnerName, receiptDate, currencyCode string,
) (Receipt, error) {
	partnerName = strings.TrimSpace(partnerName)

	if identifier.IsZero() || companyID.IsZero() || branchID.IsZero() {
		return Receipt{}, errs.Validation(CodeInvalidReceipt,
			"a receipt needs an identity, a company, and a branch")
	}
	if warehouseID.IsZero() {
		// Where the goods physically went. A delivery with no warehouse is stock that exists
		// somewhere nobody can look.
		return Receipt{}, errs.Validation(CodeInvalidReceipt,
			"a receipt needs to say where the goods went")
	}
	if partnerID.IsZero() || partnerName == "" {
		return Receipt{}, errs.Validation(CodeNoSupplier,
			"a receipt needs to say who delivered")
	}
	if strings.TrimSpace(receiptDate) == "" {
		return Receipt{}, errs.Validation(CodeInvalidReceipt, "a receipt needs a date")
	}

	return Receipt{
		ID: identifier, CompanyID: companyID, BranchID: branchID, WarehouseID: warehouseID,
		Status: ReceiptDraft, PartnerID: partnerID, PartnerName: partnerName,
		ReceiptDate: receiptDate, CurrencyCode: strings.ToUpper(currencyCode),
	}, nil
}

// RequireDraft refuses to change a confirmed delivery.
//
// Confirming is irreversible: stock has moved and the books have accrued. A correction is a
// supplier return or an adjustment, both of which leave a record of themselves — editing the
// receipt would leave the stock ledger describing a delivery that no longer says it happened.
func (r Receipt) RequireDraft() error {
	switch r.Status {
	case ReceiptDraft:
		return nil
	case ReceiptConfirmed:
		return errs.Conflict(CodeAlreadyConfirmed,
			"this delivery is confirmed; the goods are already on the shelf")
	default:
		return errs.Conflict(CodeCancelled, "this delivery was cancelled")
	}
}

// RequireBillable refuses to bill a receipt twice.
//
// Billing one twice pays a supplier twice for one delivery, which is exactly what the three-way
// match exists to prevent — and it is the failure mode that costs real money rather than
// tidiness.
func (r Receipt) RequireBillable() error {
	if r.Status != ReceiptConfirmed {
		return errs.Conflict(CodeNotConfirmed,
			"only a confirmed delivery can be billed")
	}
	if r.BilledAt != "" {
		return errs.Conflict(CodeAlreadyBilled,
			"this delivery has already been billed").WithParam("bill", string(r.BillID))
	}
	return nil
}

// ReceiptLine is one thing that arrived.
type ReceiptLine struct {
	ID          id.ID
	LineNumber  int
	OrderLineID id.ID
	ProductID   id.ID
	VariantID   id.ID

	ProductName string
	VariantSKU  string
	UomCode     string

	UomID              id.ID
	QuantityMicro      int64
	QuantityStockMicro int64

	UnitCostMicro int64
	ValueMinor    int64

	LotID      id.ID
	SerialID   id.ID
	MovementID id.ID

	Notes string
}

// NewReceiptLine builds a receipt line, or refuses.
func NewReceiptLine(
	identifier, productID, variantID, uomID id.ID,
	lineNumber int, quantityMicro, quantityStockMicro int64,
) (ReceiptLine, error) {
	if identifier.IsZero() || productID.IsZero() || variantID.IsZero() {
		return ReceiptLine{}, errs.Validation(CodeInvalidLine,
			"a receipt line needs an identity, a product, and a variant")
	}
	if uomID.IsZero() {
		return ReceiptLine{}, errs.Validation(CodeInvalidLine, "a receipt line needs a unit")
	}
	if quantityMicro <= 0 || quantityStockMicro <= 0 {
		// Receiving nothing is not a delivery. Receiving a NEGATIVE quantity is a return, which
		// is its own document with its own accounting and its own effect on what we owe.
		return ReceiptLine{}, errs.Validation(CodeNonPositiveQty,
			"a receipt line must be for a positive quantity")
	}

	return ReceiptLine{
		ID: identifier, LineNumber: lineNumber, ProductID: productID, VariantID: variantID,
		UomID: uomID, QuantityMicro: quantityMicro, QuantityStockMicro: quantityStockMicro,
	}, nil
}

// Value computes what a receipt line's goods were worth.
func (l ReceiptLine) Value(unitCostMicro int64, decimals int) ReceiptLine {
	l.UnitCostMicro = unitCostMicro
	l.ValueMinor = grossMinor(l.QuantityMicro, unitCostMicro, decimals)
	return l
}

// ── the over-receipt rule ───────────────────────────────────────────────────────

// TolerancePolicy is how much more than was ordered a business will take.
type TolerancePolicy struct {
	// PercentMicro is the allowance, 10⁻⁶ scaled: 20_000 is 2%.
	PercentMicro int64
}

// CheckOverReceipt decides whether a delivery may exceed what is outstanding.
//
// # Why this is a POLICY and not a rule
//
// Suppliers deliver 102 where 100 was ordered — bulk goods are cut, weighed, or counted by
// machine, and exactness costs more than the difference. Whether that is acceptable is a
// business decision, and it differs by trade: a fastener wholesaler expects tolerance and a
// pharmacy dispensing controlled drugs does not.
//
// Refusing outright would make the software wrong for half its market. Accepting silently would
// remove the control the document exists for — a supplier who over-delivers by 40% and invoices
// for it has been paid for goods nobody ordered.
//
// # The tolerance is measured against the OUTSTANDING quantity, not the ordered one
//
// An order for 100 delivered as 60 then 45 is not a 5% over-delivery on the second note; it is
// 5 more than the 40 still owed. Measuring against the ordered quantity would let each of five
// deliveries be 2% over and the total be 10% over, which is the arithmetic a supplier who wants
// to over-ship relies on.
func CheckOverReceipt(outstandingMicro, arrivingMicro int64, policy TolerancePolicy) error {
	if arrivingMicro <= outstandingMicro {
		return nil
	}
	if outstandingMicro <= 0 {
		// Nothing was outstanding, so a percentage of it is nothing. This is a delivery against
		// a line already filled, and no tolerance makes that acceptable — it is either a
		// duplicate or a delivery meant for somebody else.
		return errs.Conflict(CodeNothingOutstanding,
			"that line has already been filled in full").
			WithParam("arriving", itoa(arrivingMicro))
	}

	excess := arrivingMicro - outstandingMicro
	// excess/outstanding, as a 10⁻⁶ percentage, without floating point: (excess × 10⁶) /
	// outstanding compared against the allowance. Multiplication first, so a small excess against
	// a large outstanding does not truncate to zero and quietly pass.
	allowed := outstandingMicro * policy.PercentMicro / 1_000_000
	if excess <= allowed {
		return nil
	}
	return errs.Conflict(CodeOverReceipt,
		"more arrived than was ordered, beyond what this business allows").
		WithParam("outstanding", itoa(outstandingMicro)).
		WithParam("arriving", itoa(arrivingMicro)).
		WithParam("allowed_excess", itoa(allowed))
}
