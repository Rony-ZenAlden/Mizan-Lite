package domain

import (
	"math/big"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// BillStatus is where a supplier invoice has got to.
type BillStatus string

// The bill statuses.
const (
	BillDraft     BillStatus = "draft"
	BillPosted    BillStatus = "posted"
	BillCancelled BillStatus = "cancelled"
)

// Bill is a supplier's invoice.
type Bill struct {
	ID        id.ID
	CompanyID id.ID
	BranchID  id.ID

	Status      BillStatus
	Number      string
	PartnerID   id.ID
	PartnerName string
	BillDate    string
	DueDate     string

	// SupplierInvoiceNumber is THEIR number, which is the one that matters: a payment reference
	// quotes it and a statement reconciliation matches on it.
	SupplierInvoiceNumber string

	CurrencyCode      string
	ExchangeRateMicro int64

	NetMinor      int64
	TaxMinor      int64
	DiscountMinor int64
	TotalMinor    int64

	// AccruedMinor is what the receipts behind this bill put into GRNI, and what the posting
	// must clear exactly.
	AccruedMinor int64
	// VarianceMinor is net − accrued. Positive means the supplier charged more than was ordered.
	VarianceMinor int64

	Notes string
}

// NewBill builds a draft bill, or refuses.
func NewBill(
	identifier, companyID, branchID, partnerID id.ID,
	partnerName, billDate, supplierInvoiceNumber, currencyCode string,
) (Bill, error) {
	partnerName = strings.TrimSpace(partnerName)
	supplierInvoiceNumber = strings.TrimSpace(supplierInvoiceNumber)

	if identifier.IsZero() || companyID.IsZero() || branchID.IsZero() {
		return Bill{}, errs.Validation(CodeInvalidBill,
			"a bill needs an identity, a company, and a branch")
	}
	if partnerID.IsZero() || partnerName == "" {
		return Bill{}, errs.Validation(CodeNoSupplier, "a bill needs a supplier")
	}
	if strings.TrimSpace(billDate) == "" {
		return Bill{}, errs.Validation(CodeInvalidBill, "a bill needs a date")
	}
	if supplierInvoiceNumber == "" {
		// Required, not optional. Without it a payment cannot quote a reference the supplier
		// recognises, a statement cannot be reconciled, and the duplicate-invoice check has
		// nothing to compare — which is the check that stops a business paying twice.
		return Bill{}, errs.Validation(CodeNoSupplierInvoice,
			"a bill needs the supplier's own invoice number").
			WithField("supplier_invoice_number", CodeNoSupplierInvoice, "required")
	}

	return Bill{
		ID: identifier, CompanyID: companyID, BranchID: branchID,
		Status: BillDraft, PartnerID: partnerID, PartnerName: partnerName,
		BillDate: billDate, SupplierInvoiceNumber: supplierInvoiceNumber,
		CurrencyCode: strings.ToUpper(currencyCode), ExchangeRateMicro: unitScale,
	}, nil
}

// RequireDraft refuses to change a posted bill.
func (b Bill) RequireDraft() error {
	switch b.Status {
	case BillDraft:
		return nil
	case BillPosted:
		return errs.Conflict(CodeBillPosted,
			"this bill has been posted; correct it with a debit note")
	default:
		return errs.Conflict(CodeCancelled, "this bill was cancelled")
	}
}

// BillLine is one thing a supplier is charging for.
type BillLine struct {
	ID            id.ID
	LineNumber    int
	ReceiptLineID id.ID
	ProductID     id.ID
	VariantID     id.ID

	ProductName string
	VariantSKU  string
	UomCode     string

	QuantityMicro int64

	UnitPriceMicro       int64
	AccruedUnitCostMicro int64

	DiscountMinor  int64
	TaxRateMicro   int64
	TaxCode        string
	TaxAmountMinor int64
	NetMinor       int64
	AccruedMinor   int64
	TotalMinor     int64

	Notes string
}

// PriceVarianceMinor is what the supplier charged over what was accrued.
//
// Negative when they charged less, which is just as real and just as much a variance — a
// supplier honouring a discount nobody recorded shows up here.
func (l BillLine) PriceVarianceMinor() int64 { return l.NetMinor - l.AccruedMinor }

// ── the three-way match ─────────────────────────────────────────────────────────

// MatchResult reports how a bill line compares with what was ordered and received.
type MatchResult struct {
	// QuantityMatches is whether the invoiced quantity equals what arrived.
	QuantityMatches bool
	// PriceMatches is whether the invoiced price equals what was ordered.
	PriceMatches bool
	// PriceVarianceMinor is the difference, positive when the supplier charged more.
	PriceVarianceMinor int64
}

// Matched reports whether both sides agree.
func (m MatchResult) Matched() bool { return m.QuantityMatches && m.PriceMatches }

// Match compares a bill line with the delivery it pays for.
//
// # Quantity is guaranteed by CONSTRUCTION; price is reported
//
// A bill line names a receipt line and takes its quantity in full, so "invoiced for goods that
// never arrived" is not a state the schema can hold. `QuantityMatches` is therefore always true
// in practice — it is reported so that a screen showing a match has all three sides on it, and
// so that a future change which made the quantity settable would have somewhere to be caught.
//
// Being invoiced at a different PRICE is ordinary: a surcharge, a currency movement, a price
// agreed by telephone and never recorded. It is a fact to book and to show somebody, not a
// reason to reject a delivery that has already been unloaded and put away. Refusing it would
// leave the goods on the shelf, the GRNI accrued, and no way to close the loop except by
// editing the order retrospectively — which is worse than the variance.
func Match(billed BillLine, receivedMicro int64) MatchResult {
	result := MatchResult{
		QuantityMatches:    billed.QuantityMicro == receivedMicro,
		PriceVarianceMinor: billed.PriceVarianceMinor(),
	}
	result.PriceMatches = result.PriceVarianceMinor == 0
	return result
}

// Price sets a bill line's price and recomputes its money.
func (l BillLine) Price(unitPriceMicro, discountMinor int64, decimals int) (BillLine, error) {
	if unitPriceMicro < 0 {
		return BillLine{}, errs.Validation(CodeNegativePrice,
			"a purchase price cannot be negative")
	}

	gross := grossMinor(l.QuantityMicro, unitPriceMicro, decimals)
	if discountMinor > gross {
		return BillLine{}, errs.Validation(CodeInvalidLine,
			"a discount cannot exceed the line it discounts")
	}

	l.UnitPriceMicro = unitPriceMicro
	l.DiscountMinor = discountMinor
	l.NetMinor = gross - discountMinor
	l.TotalMinor = l.NetMinor + l.TaxAmountMinor
	return l, nil
}

// Tax applies a resolved tax.
func (l BillLine) Tax(amountMinor, rateMicro int64, code string) BillLine {
	l.TaxAmountMinor = amountMinor
	l.TaxRateMicro = rateMicro
	l.TaxCode = code
	l.TotalMinor = l.NetMinor + amountMinor
	return l
}

// BillTotals recomputes a bill's money from its lines.
func BillTotals(lines []BillLine) (net, tax, discount, total, accrued int64) {
	for _, line := range lines {
		net += line.NetMinor
		tax += line.TaxAmountMinor
		discount += line.DiscountMinor
		total += line.TotalMinor
		accrued += line.AccruedMinor
	}
	return net, tax, discount, total, accrued
}

// SplitVariance divides a variance into the two non-negative halves a posting needs.
//
// # Why two numbers instead of one signed one
//
// The posting engine refuses negative amounts, because a negative would silently flip a line to
// the other side of the entry — a credit that was meant to be a debit, balancing perfectly and
// meaning the opposite. So a rule that must move money in either direction gets TWO lines with
// two amounts, exactly one of which is ever non-zero; the other resolves to nothing and Phase 2
// skips it.
//
// The same shape as Phase 5's `cash_short` and `cash_over`, and for the same reason: when the
// books must differ, the ACTION differs, never the module's knowledge of accounts (§20.3).
func SplitVariance(varianceMinor int64) (over, under int64) {
	if varianceMinor > 0 {
		return varianceMinor, 0
	}
	return 0, -varianceMinor
}

// ── the variance split (6.4) ────────────────────────────────────────────────────

// VarianceSplit is a price difference divided by where the goods are now.
type VarianceSplit struct {
	// StockMinor is the part belonging to goods still on the shelf. It REVALUES them.
	StockMinor int64
	// ExpenseMinor is the part belonging to goods already sold, whose cost has posted.
	ExpenseMinor int64
}

// SplitByWhereTheGoodsAre divides a line's price variance between stock and expense.
//
// # Why the split exists at all
//
// A bill that disagrees with the order it bills is correcting what the goods cost. Where those
// goods are decides what the correction can do:
//
//   - **Still on the shelf** → the stock is carrying the wrong cost, and revaluing it fixes both
//     the balance sheet and every future sale. Phase 4 built `Revaluation` for exactly this in
//     4.2 and nothing has called it until now.
//   - **Already sold** → the cost of that sale posted at the old figure, in a period that may be
//     closed. §D.4 forbids reopening periods to restate costing, and the same argument holds
//     here: the correction goes to an adjustment account, visible and explainable, rather than
//     rewriting history.
//
// # The proportion is capped at what was received
//
// `onHandMicro` is the whole variant's stock, which may exceed this delivery — other receipts
// contribute to it. Attributing more than THIS delivery's quantity would revalue goods that came
// in at a price this bill says nothing about.
func SplitByWhereTheGoodsAre(
	varianceMinor, receivedMicro, onHandMicro int64,
) VarianceSplit {
	if varianceMinor == 0 || receivedMicro <= 0 {
		return VarianceSplit{}
	}
	if onHandMicro <= 0 {
		// Everything has gone. The whole correction belongs to costs already posted.
		return VarianceSplit{ExpenseMinor: varianceMinor}
	}

	remaining := onHandMicro
	if remaining > receivedMicro {
		remaining = receivedMicro
	}

	// Proportional, computed multiplication-first so a small remainder against a large delivery
	// does not truncate to nothing — the same trap the over-receipt tolerance carries (6.2).
	//
	// There was a `remaining == receivedMicro` shortcut here returning the whole variance. A
	// drill deleted it and nothing failed, because the proportional path already answers
	// `variance × received / received`, which is the variance. Redundant code that makes a
	// drill on the real path ambiguous is worse than no code (5.8's lesson).
	stock := scaledShare(varianceMinor, remaining, receivedMicro)
	return VarianceSplit{StockMinor: stock, ExpenseMinor: varianceMinor - stock}
}

// scaledShare is `amount × part / whole`, exact and sign-preserving.
func scaledShare(amount, part, whole int64) int64 {
	product := new(big.Int).Mul(big.NewInt(amount), big.NewInt(part))
	return new(big.Int).Quo(product, big.NewInt(whole)).Int64()
}
