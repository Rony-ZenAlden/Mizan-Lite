package domain

import (
	"errors"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/settle"
)

// Method is how money left the business.
type Method string

// The payment methods.
//
// There is no `credit` here, unlike a customer payment. A sale on credit is a real event — goods
// leave and nothing is paid — but you cannot PAY a supplier on credit; not paying them is simply
// the bill still being outstanding.
const (
	Cash         Method = "cash"
	Card         Method = "card"
	BankTransfer Method = "bank_transfer"
	Cheque       Method = "cheque"
)

// Valid reports whether a method is one this build knows.
func (m Method) Valid() bool {
	switch m {
	case Cash, Card, BankTransfer, Cheque:
		return true
	default:
		return false
	}
}

// PaymentStatus is where a payment has got to.
type PaymentStatus string

// The payment statuses.
const (
	PaymentDraft     PaymentStatus = "draft"
	PaymentPosted    PaymentStatus = "posted"
	PaymentCancelled PaymentStatus = "cancelled"
)

// Payment is money going out to a supplier.
type Payment struct {
	ID          id.ID
	CompanyID   id.ID
	BranchID    id.ID
	PartnerID   id.ID
	PartnerName string

	Number      string
	PaymentDate string
	Method      Method
	Reference   string

	CurrencyCode      string
	ExchangeRateMicro int64
	AmountMinor       int64

	Status PaymentStatus
	Notes  string
}

// NewPayment builds a draft payment, or refuses.
func NewPayment(
	identifier, companyID, branchID, partnerID id.ID,
	partnerName, paymentDate, currencyCode string, method Method, amountMinor int64,
) (Payment, error) {
	partnerName = strings.TrimSpace(partnerName)

	if identifier.IsZero() || companyID.IsZero() || branchID.IsZero() {
		return Payment{}, errs.Validation(CodeInvalidPayment,
			"a payment needs an identity, a company, and a branch")
	}
	if partnerID.IsZero() || partnerName == "" {
		// Required, unlike a customer payment's walk-in. Money leaving the business goes to
		// somebody, and a payment with no payee is a hole in the cash position nobody can chase.
		return Payment{}, errs.Validation(CodeNoSupplier,
			"a payment needs to say who is being paid")
	}
	if strings.TrimSpace(paymentDate) == "" {
		return Payment{}, errs.Validation(CodeInvalidPayment, "a payment needs a date")
	}
	if !method.Valid() {
		return Payment{}, errs.Validation(CodeInvalidPayment,
			"that is not a way of paying this build knows").WithParam("method", string(method))
	}
	if amountMinor <= 0 {
		// A refund FROM a supplier is money coming the other way and is its own document. A
		// negative here would make every sum over payments meaningless.
		return Payment{}, errs.Validation(CodeNonPositiveAmount,
			"a payment must be for a positive amount")
	}

	return Payment{
		ID: identifier, CompanyID: companyID, BranchID: branchID,
		PartnerID: partnerID, PartnerName: partnerName,
		PaymentDate: paymentDate, Method: method, AmountMinor: amountMinor,
		CurrencyCode: strings.ToUpper(currencyCode), ExchangeRateMicro: unitScale,
		Status: PaymentDraft,
	}, nil
}

// RequireDraft refuses to change a posted payment.
func (p Payment) RequireDraft() error {
	switch p.Status {
	case PaymentDraft:
		return nil
	case PaymentPosted:
		return errs.Conflict(CodePaymentPosted,
			"this payment has been posted; the money has gone")
	default:
		return errs.Conflict(CodeCancelled, "this payment was cancelled")
	}
}

// PostingAction is the rule key a payment fires.
//
// # The METHOD is part of the action, not a field the rules read
//
// Phase 2 seeded one `supplier_payment` rule crediting CASH whatever the method, so a bank
// transfer to a supplier would have reduced the till. The identical defect existed on the
// receiving side and 5.5 found it there.
//
// The fix is the §20.3 shape: four actions, four seeded rules, and no Go that knows which account
// any of them touches. A business whose cheques clear through a separate account edits a seed
// file.
func (p Payment) PostingAction() string {
	return "purchasing.payment." + string(p.Method) + ".made"
}

// Allocation is a payment settling part of a bill.
type Allocation struct {
	BillID      id.ID
	AmountMinor int64
}

// RequireAllocatable checks that a payment can cover what it is being asked to settle.
//
// # Over-allocation is refused, under-allocation is not
//
// Allocating more than the payment is arithmetic that cannot be true: the money does not exist.
// Allocating LESS is a prepayment with a balance still to assign, which is ordinary — a business
// paying a round figure against a statement leaves a few units unallocated, and forcing them to
// balance would mean inventing an allocation.
func RequireAllocatable(paymentMinor int64, allocations []Allocation) error {
	amounts := make([]settle.Allocation, 0, len(allocations))
	for _, allocation := range allocations {
		amounts = append(amounts, settle.Allocation{AmountMinor: allocation.AmountMinor})
	}

	// The kernel decides what the rule IS; this decides what a user is told. The sentinel comes
	// back as a coded error because a code is an i18n key and a screen renders it.
	switch err := settle.RequireAllocatable(paymentMinor, amounts); {
	case errors.Is(err, settle.ErrNonPositive):
		return errs.Validation(CodeNonPositiveAmount,
			"an allocation must be for a positive amount")
	case errors.Is(err, settle.ErrOverAllocated):
		return errs.Validation(CodeOverAllocated,
			"that allocates more than the payment is worth").
			WithParam("payment", settle.Itoa(paymentMinor)).
			WithParam("allocated", settle.Itoa(settle.Total(amounts)))
	case err != nil:
		return errs.Validation(CodeOverAllocated, err.Error())
	}
	return nil
}
