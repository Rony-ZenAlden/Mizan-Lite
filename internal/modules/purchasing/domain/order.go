// Package domain holds purchasing's rules, with no knowledge of storage.
package domain

import (
	"math/big"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable codes, doubling as i18n keys.
const (
	CodeInvalidOrder       = "purchasing.invalid_order"
	CodeInvalidLine        = "purchasing.invalid_line"
	CodeNotDraft           = "purchasing.not_draft"
	CodeAlreadyPlaced      = "purchasing.already_placed"
	CodeCancelled          = "purchasing.cancelled"
	CodeClosed             = "purchasing.closed"
	CodeNoLines            = "purchasing.no_lines"
	CodeNegativePrice      = "purchasing.negative_price"
	CodeNoSupplier         = "purchasing.no_supplier"
	CodeNonPositiveQty     = "purchasing.non_positive_quantity"
	CodeOverReceipt        = "purchasing.over_receipt"
	CodeNothingOutstanding = "purchasing.nothing_outstanding"
	CodeInvalidReceipt     = "purchasing.invalid_receipt"
	CodeAlreadyConfirmed   = "purchasing.already_confirmed"
	CodeNotConfirmed       = "purchasing.not_confirmed"
	CodeAlreadyBilled      = "purchasing.already_billed"
	CodeInvalidBill        = "purchasing.invalid_bill"
	CodeBillPosted         = "purchasing.bill_posted"
	CodeNoSupplierInvoice  = "purchasing.no_supplier_invoice"
	CodeQuantityMismatch   = "purchasing.quantity_mismatch"
	CodeDuplicateInvoice   = "purchasing.duplicate_invoice"
	CodeInvalidLandedCost  = "purchasing.invalid_landed_cost"
	CodeUnsupportedBasis   = "purchasing.unsupported_basis"
	CodeLandedApplied      = "purchasing.landed_cost_applied"
)

// The numeric scales this module works in (§E).
const (
	// quantityScale is 10⁶ — all quantities.
	quantityScale = 1_000_000
	// unitScale is 10⁶ of the MAJOR currency unit — unit prices and unit costs.
	//
	// Separate from money's minor units, and the separation is the point: the price of a metre of
	// cable is routinely a fraction of a minor unit, and rounding it before multiplying by 3,000
	// metres produces a materially wrong order.
	unitScale = 1_000_000
)

// Status is where an order has got to.
type Status string

// The statuses.
const (
	Draft     Status = "draft"
	Placed    Status = "placed"
	Closed    Status = "closed"
	Cancelled Status = "cancelled"
)

// IsTerminal reports whether an order can still receive goods.
func (s Status) IsTerminal() bool { return s == Closed || s == Cancelled }

// Order is what we asked a supplier for.
type Order struct {
	ID          id.ID
	CompanyID   id.ID
	BranchID    id.ID
	WarehouseID id.ID

	Status            Status
	Number            string
	PartnerID         id.ID
	PartnerName       string
	OrderDate         string
	ExpectedDate      string
	CurrencyCode      string
	ExchangeRateMicro int64

	NetMinor      int64
	TaxMinor      int64
	DiscountMinor int64
	TotalMinor    int64

	SupplierReference string
	Notes             string
}

// NewOrder builds a draft order, or refuses.
//
// # A supplier is required, unlike a sale's customer
//
// A shop sells to whoever walks in, and §2.6 requires that a walk-in needs no record. Nobody
// orders from nobody: the whole content of the document is that a named party has undertaken to
// deliver, and an order with no supplier is a note to self.
func NewOrder(
	identifier, companyID, branchID, warehouseID, partnerID id.ID,
	partnerName, orderDate, currencyCode string,
) (Order, error) {
	partnerName = strings.TrimSpace(partnerName)

	if identifier.IsZero() || companyID.IsZero() || branchID.IsZero() {
		return Order{}, errs.Validation(CodeInvalidOrder,
			"an order needs an identity, a company, and a branch")
	}
	if warehouseID.IsZero() {
		// Required on the ORDER, not just the receipt: a business with two warehouses orders for
		// one of them, and a delivery that arrives at the other is a problem somebody has to
		// solve on the loading bay.
		return Order{}, errs.Validation(CodeInvalidOrder,
			"an order needs to say where the goods are expected")
	}
	if partnerID.IsZero() || partnerName == "" {
		return Order{}, errs.Validation(CodeNoSupplier,
			"an order needs a supplier").WithField("partner_id", CodeNoSupplier, "required")
	}
	if strings.TrimSpace(orderDate) == "" {
		return Order{}, errs.Validation(CodeInvalidOrder, "an order needs a date")
	}
	if strings.TrimSpace(currencyCode) == "" {
		return Order{}, errs.Validation(CodeInvalidOrder, "an order needs a currency")
	}

	return Order{
		ID: identifier, CompanyID: companyID, BranchID: branchID, WarehouseID: warehouseID,
		Status: Draft, PartnerID: partnerID, PartnerName: partnerName,
		OrderDate: orderDate, CurrencyCode: strings.ToUpper(currencyCode),
		ExchangeRateMicro: unitScale,
	}, nil
}

// RequireDraft refuses to change an order that has been placed.
//
// Placing is the irreversible act here, as posting is in sales: the document has been SENT, and a
// supplier is picking from it. Editing it afterwards means the paper in their hand and the record
// in ours describe different orders, and the delivery settles which one was real.
func (o Order) RequireDraft() error {
	switch o.Status {
	case Draft:
		return nil
	case Placed:
		return errs.Conflict(CodeAlreadyPlaced,
			"this order has been placed; the supplier is working from it")
	case Closed:
		return errs.Conflict(CodeClosed, "this order is closed")
	default:
		return errs.Conflict(CodeCancelled, "this order was cancelled")
	}
}

// RequireReceivable refuses goods against an order that cannot take them.
func (o Order) RequireReceivable() error {
	if o.Status == Placed {
		return nil
	}
	if o.Status == Draft {
		// A draft has not been sent, so nothing can have been delivered against it. Goods that
		// arrive anyway are a receipt against no order, which 6.2 allows as its own document —
		// but they must not silently attach here, or the order's own history becomes fiction.
		return errs.Conflict(CodeNotDraft,
			"this order has not been placed, so nothing can have arrived against it")
	}
	return errs.Conflict(CodeClosed, "this order is no longer receiving goods")
}

// Line is one thing ordered.
type Line struct {
	ID         id.ID
	LineNumber int
	ProductID  id.ID
	VariantID  id.ID

	// The §9.3 snapshot: what these things were CALLED when the order was placed.
	ProductName  string
	VariantSKU   string
	UomCode      string
	SupplierCode string

	UomID              id.ID
	QuantityMicro      int64
	QuantityStockMicro int64

	UnitPriceMicro int64
	DiscountMinor  int64
	TaxRateMicro   int64
	TaxCode        string
	TaxAmountMinor int64
	NetMinor       int64
	TotalMinor     int64

	// ReceivedMicro is how much has actually arrived, in the ORDER's unit.
	ReceivedMicro int64

	Notes string
}

// NewLine builds an order line, or refuses.
func NewLine(
	identifier, productID, variantID, uomID id.ID,
	lineNumber int, quantityMicro, quantityStockMicro int64,
) (Line, error) {
	if identifier.IsZero() || productID.IsZero() || variantID.IsZero() {
		return Line{}, errs.Validation(CodeInvalidLine,
			"a line needs an identity, a product, and a variant")
	}
	if uomID.IsZero() {
		return Line{}, errs.Validation(CodeInvalidLine, "a line needs a unit")
	}
	if lineNumber <= 0 {
		return Line{}, errs.Validation(CodeInvalidLine, "a line needs a position")
	}
	if quantityMicro <= 0 || quantityStockMicro <= 0 {
		// Ordering nothing has no reading that makes sense, and a NEGATIVE order is a return
		// wearing an order's clothes — returns are their own document.
		return Line{}, errs.Validation(CodeNonPositiveQty,
			"an order line must be for a positive quantity")
	}

	return Line{
		ID: identifier, LineNumber: lineNumber, ProductID: productID, VariantID: variantID,
		UomID: uomID, QuantityMicro: quantityMicro, QuantityStockMicro: quantityStockMicro,
	}, nil
}

// OutstandingMicro is how much of this line has still to arrive.
//
// Never negative. An over-receipt is a real thing (6.2 decides whether to allow it), but
// "outstanding" is what is still owed to us, and a supplier who sent two extra does not owe us
// minus two.
func (l Line) OutstandingMicro() int64 {
	outstanding := l.QuantityMicro - l.ReceivedMicro
	if outstanding < 0 {
		return 0
	}
	return outstanding
}

// IsFullyReceived reports whether nothing more is expected on this line.
func (l Line) IsFullyReceived() bool { return l.ReceivedMicro >= l.QuantityMicro }

// Price sets a line's unit price and recomputes its money.
//
// # Why the arithmetic goes through big.Int
//
// `quantity × unitPrice` multiplies a 10⁶-scaled quantity by a 10⁶-scaled price, giving a
// 10¹²-scaled product before it is brought back to minor units. Three thousand metres at a
// realistic price overflows int64 on the intermediate long before either operand is anywhere
// near its own limit — the classic way this arithmetic goes wrong is that it works in testing
// and fails on a genuinely large order.
func (l Line) Price(unitPriceMicro, discountMinor int64, decimals int) (Line, error) {
	if unitPriceMicro < 0 {
		return Line{}, errs.Validation(CodeNegativePrice,
			"a purchase price cannot be negative")
	}
	if discountMinor < 0 {
		return Line{}, errs.Validation(CodeNegativePrice, "a discount cannot be negative")
	}

	gross := grossMinor(l.QuantityMicro, unitPriceMicro, decimals)
	if discountMinor > gross {
		return Line{}, errs.Validation(CodeInvalidLine,
			"a discount cannot exceed the line it discounts").
			WithParam("discount", itoa(discountMinor)).WithParam("line", itoa(gross))
	}

	l.UnitPriceMicro = unitPriceMicro
	l.DiscountMinor = discountMinor
	l.NetMinor = gross - discountMinor
	l.TotalMinor = l.NetMinor + l.TaxAmountMinor
	return l, nil
}

// Tax applies a resolved tax to a line.
func (l Line) Tax(amountMinor, rateMicro int64, code string) Line {
	l.TaxAmountMinor = amountMinor
	l.TaxRateMicro = rateMicro
	l.TaxCode = code
	l.TotalMinor = l.NetMinor + amountMinor
	return l
}

// grossMinor is quantity × unit price, brought to minor units, rounded once.
//
// ONE rounding, at the end. Rounding the unit price to minor units first and then multiplying is
// the mistake §E exists to prevent: it is wrong by up to half a minor unit per UNIT, which on
// three thousand metres is not a rounding difference but a number.
func grossMinor(quantityMicro, unitPriceMicro int64, decimals int) int64 {
	product := new(big.Int).Mul(big.NewInt(quantityMicro), big.NewInt(unitPriceMicro))

	// quantity(10⁶) × price(10⁶ of the MAJOR unit) = 10¹² of the major unit.
	// Minor units are 10^decimals of the major unit, so divide by 10¹² and multiply by 10^d.
	product.Mul(product, pow10(decimals))
	divisor := new(big.Int).SetInt64(quantityScale * unitScale)

	quotient, remainder := new(big.Int).QuoRem(product, divisor, new(big.Int))
	// Half-up on the absolute value, so a negative never rounds the other way from its positive
	// twin — which is how a credit and the invoice it reverses come to differ by a unit.
	remainder.Abs(remainder)
	remainder.Mul(remainder, big.NewInt(2))
	if remainder.Cmp(divisor) >= 0 {
		if product.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	return quotient.Int64()
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

// Totals recomputes an order's money from its lines.
//
// Summed from the LINES, never accumulated as they are added. An accumulated total drifts the
// moment a line is edited or removed, and the drift is invisible: the document still balances
// against itself, just not against what it contains.
func Totals(lines []Line) (net, tax, discount, total int64) {
	for _, line := range lines {
		net += line.NetMinor
		tax += line.TaxAmountMinor
		discount += line.DiscountMinor
		total += line.TotalMinor
	}
	return net, tax, discount, total
}

func itoa(v int64) string {
	return new(big.Int).SetInt64(v).String()
}
