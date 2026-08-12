package domain

import (
	"math/big"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable codes for documents.
const (
	CodeInvalidDocument = "sales.invalid_document"
	CodeInvalidLine     = "sales.invalid_line"
	CodeNotDraft        = "sales.not_draft"
	CodeAlreadyPosted   = "sales.already_posted"
	CodeCancelled       = "sales.cancelled"
	CodeNoLines         = "sales.no_lines"
	CodeNegativePrice   = "sales.negative_price"
	CodeDiscountTooBig  = "sales.discount_exceeds_line"
	CodeQuotationMoves  = "sales.quotation_moves_nothing"
)

// quantityScale is the scale a quantity is held at: 10⁶.
const quantityScale = 1_000_000

// Type is what kind of document this is.
type Type string

// The document types.
const (
	Quotation  Type = "quotation"
	Order      Type = "order"
	Invoice    Type = "invoice"
	CreditNote Type = "credit_note"
)

// MovesStock reports whether posting this kind of document moves goods.
//
// A quotation is a promise and an order is an intention; neither takes anything off a shelf. Only
// an invoice issues and only a credit note returns. Putting this on the TYPE rather than in the
// posting service means a report, a screen, and the poster cannot disagree about it.
func (t Type) MovesStock() bool { return t == Invoice || t == CreditNote }

// Status is where a document has got to.
type Status string

// The statuses.
const (
	Draft     Status = "draft"
	Posted    Status = "posted"
	Cancelled Status = "cancelled"
)

// IsTerminal reports whether a document can still change.
func (s Status) IsTerminal() bool { return s == Posted || s == Cancelled }

// Document is a sale in one of its forms.
type Document struct {
	ID          id.ID
	BranchID    id.ID
	WarehouseID id.ID
	Type        Type
	Status      Status

	// Number is empty until posting (§9.4).
	Number string
	// SourceID is the document this one came from: a quotation's order, or the invoice a credit
	// note reverses.
	SourceID id.ID

	PartnerID id.ID
	// PartnerName is snapshotted for the same reason a line snapshots a product name.
	PartnerName string

	Date         string
	DueDate      string
	CurrencyCode string
	RateMicro    int64

	NetMinor      int64
	TaxMinor      int64
	DiscountMinor int64
	TotalMinor    int64
	CostMinor     int64

	IsHeld    bool
	HoldLabel string
	Notes     string
	PostedAt  string
}

// NewDocument builds a document in draft, or refuses.
func NewDocument(
	identifier, branchID id.ID, documentType Type, date, currencyCode string,
) (Document, error) {
	if identifier.IsZero() || branchID.IsZero() {
		return Document{}, errs.Validation(CodeInvalidDocument,
			"a sales document needs an identity and a branch")
	}
	switch documentType {
	case Quotation, Order, Invoice, CreditNote:
	default:
		return Document{}, errs.Validation(CodeInvalidDocument,
			"that is not a kind of sales document").WithParam("type", string(documentType))
	}
	if strings.TrimSpace(date) == "" {
		// The business date decides the fiscal period. Defaulting it to today would put a sale
		// entered on Monday for Saturday's trading into the wrong period, silently.
		return Document{}, errs.Validation(CodeInvalidDocument,
			"a sales document needs a date").WithField("date", CodeInvalidDocument, "required")
	}
	if strings.TrimSpace(currencyCode) == "" {
		return Document{}, errs.Validation(CodeInvalidDocument,
			"a sales document needs a currency")
	}

	return Document{
		ID: identifier, BranchID: branchID, Type: documentType, Status: Draft,
		Date: date, CurrencyCode: strings.ToUpper(currencyCode), RateMicro: quantityScale,
	}, nil
}

// Adopt rebuilds a document from storage without re-running construction rules.
func Adopt(d Document) Document { return d }

// RequireDraft refuses to change a document that is no longer editable.
//
// # A posted document is immutable
//
// The same rule the journal (Phase 2) and the stock ledger (Phase 4) follow, for the same reason.
// A posted invoice is corrected by a CREDIT NOTE, never by editing: editing would silently
// restate a period whose books may be closed, and would leave the stock movement and the journal
// entry describing a document that no longer says what they were made from.
func (d Document) RequireDraft() error {
	switch d.Status {
	case Posted:
		return errs.Conflict(CodeAlreadyPosted,
			"this document is posted and cannot be changed; issue a credit note instead").
			WithParam("number", d.Number)
	case Cancelled:
		return errs.Conflict(CodeCancelled,
			"this document was cancelled").WithParam("id", string(d.ID))
	}
	return nil
}

// Line is one item on a document.
type Line struct {
	ID         id.ID
	LineNumber int
	ProductID  id.ID
	VariantID  id.ID

	// The snapshot of what things were CALLED.
	ProductName string
	VariantSKU  string
	UomCode     string

	// Dual quantity (§B.3): as entered, and as it leaves the warehouse.
	QuantityMicro      int64
	UomID              id.ID
	QuantityStockMicro int64

	UnitPriceMinor int64
	PriceSource    string
	PriceListCode  string
	DiscountMinor  int64

	TaxRateMicro   int64
	TaxCode        string
	TaxAmountMinor int64

	NetMinor   int64
	TotalMinor int64
	CostMicro  int64

	MovementID   id.ID
	SourceLineID id.ID
	LotID        id.ID
	SerialID     id.ID
	Notes        string
}

// NewLine builds a line, or refuses.
//
// The money is NOT set here: price, tax, and cost are resolved at posting from the pricing
// engine, the tax engine, and the costing port. A constructor that accepted them would let a
// caller name its own price — and a till operator who can type a price is a discount nobody
// approved.
func NewLine(
	identifier, productID, variantID, uomID id.ID, lineNumber int, quantityMicro int64,
) (Line, error) {
	if identifier.IsZero() || productID.IsZero() || variantID.IsZero() || uomID.IsZero() {
		return Line{}, errs.Validation(CodeInvalidLine,
			"a line needs an identity, a variant, and a unit")
	}
	if lineNumber < 1 {
		return Line{}, errs.Validation(CodeInvalidLine, "a line needs a position")
	}
	if quantityMicro <= 0 {
		// Zero sells nothing and would still print a row; negative is a return, which is a
		// credit note rather than a negative line.
		return Line{}, errs.Validation(CodeInvalidLine,
			"a line must sell a positive quantity")
	}

	return Line{
		ID: identifier, LineNumber: lineNumber, ProductID: productID,
		VariantID: variantID, UomID: uomID, QuantityMicro: quantityMicro,
	}, nil
}

// Price sets a line's money and computes its totals.
//
// # One rounding, at the line
//
// `quantity × unit price` is a ×10⁶ product divided by 10⁶, computed with a 128-bit intermediate
// and rounded ONCE. Rounding the quantity first, or the product twice, drifts a document total by
// a minor unit per line — which a customer notices on a fifty-line invoice and nobody can explain.
func (l Line) Price(unitPriceMinor, discountMinor int64) (Line, error) {
	if unitPriceMinor < 0 {
		// A negative price would pay the customer to take the goods, and every total derived
		// from it would be wrong in a direction no report flags.
		return l, errs.Validation(CodeNegativePrice, "a price cannot be negative")
	}
	if discountMinor < 0 {
		return l, errs.Validation(CodeInvalidLine, "a discount cannot be negative")
	}

	gross := scaledProduct(l.QuantityMicro, unitPriceMinor)
	if discountMinor > gross {
		// A discount larger than the line makes the net negative, which is a refund wearing a
		// sale's clothes. Refused rather than clamped: clamping would silently give the goods
		// away and print a total the operator did not intend.
		return l, errs.Validation(CodeDiscountTooBig,
			"a discount cannot exceed the line it discounts").
			WithParam("discount", itoa(discountMinor)).WithParam("line", itoa(gross))
	}

	l.UnitPriceMinor = unitPriceMinor
	l.DiscountMinor = discountMinor
	l.NetMinor = gross - discountMinor
	l.TotalMinor = l.NetMinor + l.TaxAmountMinor
	return l, nil
}

// Tax sets a line's tax from an already-computed amount.
//
// The RATE is stored alongside, not the tax group: a group's rate changes, and what this line was
// taxed at does not. The amount comes from Phase 2's engine — this module does not compute tax,
// it records what the engine said.
func (l Line) Tax(rateMicro, amountMinor int64, code string) Line {
	l.TaxRateMicro = rateMicro
	l.TaxAmountMinor = amountMinor
	l.TaxCode = code
	l.TotalMinor = l.NetMinor + amountMinor
	return l
}

// Totals sums a document's lines.
//
// Summed from lines rather than accumulated as they are added, so a document's totals cannot
// drift from the lines that justify them — the same reasoning that makes a trial balance a sum
// over entries rather than a maintained figure.
func Totals(lines []Line) (net, tax, discount, total int64) {
	for _, line := range lines {
		net += line.NetMinor
		tax += line.TaxAmountMinor
		discount += line.DiscountMinor
		total += line.TotalMinor
	}
	return net, tax, discount, total
}

// RequirePostable refuses to post a document that is not ready.
func (d Document) RequirePostable(lines []Line) error {
	if err := d.RequireDraft(); err != nil {
		return err
	}
	if len(lines) == 0 {
		// An empty invoice consumes a number, posts a zero entry, and tells a customer nothing.
		return errs.Validation(CodeNoLines,
			"a document needs at least one line before it can be posted")
	}
	if d.Type.MovesStock() && d.WarehouseID.IsZero() {
		// An invoice must know where the goods leave from. A quotation must not — see below.
		return errs.Validation(CodeInvalidDocument,
			"a document that moves stock needs a warehouse")
	}
	if !d.Type.MovesStock() && !d.WarehouseID.IsZero() {
		// A quotation naming a warehouse implies goods are reserved there, which they are not.
		// The state is refused rather than ignored, because a screen that shows it would be
		// telling the operator something untrue.
		return errs.Validation(CodeQuotationMoves,
			"a quotation moves nothing and needs no warehouse")
	}
	return nil
}

// scaledProduct multiplies a micro-scaled quantity by a minor-unit price.
//
// The product is ×10⁶ and the division is by 10⁶, computed in 128 bits because a wholesaler's
// quantity times a hyperinflated currency's price overflows int64 long before either figure looks
// unusual. Rounded half away from zero, matching every other rounding a user sees.
func scaledProduct(quantityMicro, unitMinor int64) int64 {
	product := new(big.Int).Mul(big.NewInt(quantityMicro), big.NewInt(unitMinor))
	divisor := big.NewInt(quantityScale)

	quotient, remainder := new(big.Int).QuoRem(product, divisor, new(big.Int))
	twice := new(big.Int).Abs(remainder)
	twice.Lsh(twice, 1)
	if twice.Cmp(divisor) >= 0 {
		if product.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	if !quotient.IsInt64() {
		return 0
	}
	return quotient.Int64()
}
