package domain

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/settle"
)

// Stable codes for payments.
const (
	CodeInvalidPayment  = "sales.invalid_payment"
	CodeUnknownMethod   = "sales.unknown_payment_method"
	CodeOverAllocated   = "sales.payment_over_allocated"
	CodeOverSettled     = "sales.invoice_over_settled"
	CodePaymentNotDraft = "sales.payment_not_draft"
	CodePaymentPosted   = "sales.payment_already_posted"
	CodeNothingToSettle = "sales.nothing_to_settle"
)

// Method is how the money arrived.
//
// It decides WHERE the money lands — cash to the till, a card or transfer to the bank — but not
// by naming an account. The method becomes part of the posting ACTION, and the rules decide the
// rest (§20.3).
type Method string

// The payment methods.
const (
	Cash         Method = "cash"
	Card         Method = "card"
	BankTransfer Method = "bank_transfer"
	Cheque       Method = "cheque"
	// OnCredit is not a payment of money at all: it records that the customer will pay later. It
	// exists as a method so a till can close a sale without one, and it settles nothing.
	OnCredit Method = "credit"
)

// SettlesImmediately reports whether a method moves money now.
//
// `credit` does not: it is a promise, and the invoice stays outstanding. Treating it as a payment
// would show a shop as having been paid for everything it had ever sold.
func (m Method) SettlesImmediately() bool { return m != OnCredit }

// Payment is money received.
type Payment struct {
	ID          id.ID
	BranchID    id.ID
	PartnerID   id.ID
	PartnerName string

	Number    string
	Date      string
	Method    Method
	Reference string

	CurrencyCode string
	RateMicro    int64
	// AmountMinor is always POSITIVE. A refund is a payment in the other direction, not a
	// negative one — the same reasoning that keeps stock movement quantities positive.
	AmountMinor int64

	Status   Status
	Notes    string
	PostedAt string
}

// NewPayment builds a payment in draft, or refuses.
func NewPayment(
	identifier, branchID id.ID, method Method, date, currencyCode string, amountMinor int64,
) (Payment, error) {
	if identifier.IsZero() || branchID.IsZero() {
		return Payment{}, errs.Validation(CodeInvalidPayment,
			"a payment needs an identity and a branch")
	}
	switch method {
	case Cash, Card, BankTransfer, Cheque, OnCredit:
	default:
		return Payment{}, errs.Validation(CodeUnknownMethod,
			"that is not a way of paying").WithParam("method", string(method))
	}
	if strings.TrimSpace(date) == "" {
		return Payment{}, errs.Validation(CodeInvalidPayment, "a payment needs a date")
	}
	if amountMinor <= 0 {
		// Zero settles nothing and would still print a receipt. Negative is a refund, which is
		// its own document rather than a payment wearing a minus sign.
		return Payment{}, errs.Validation(CodeInvalidPayment,
			"a payment must be for a positive amount")
	}

	return Payment{
		ID: identifier, BranchID: branchID, Method: method, Date: date,
		CurrencyCode: strings.ToUpper(currencyCode), RateMicro: 1_000_000,
		AmountMinor: amountMinor, Status: Draft,
	}, nil
}

// RequireDraft refuses to change a payment that is no longer editable.
func (p Payment) RequireDraft() error {
	switch p.Status {
	case Posted:
		return errs.Conflict(CodePaymentPosted,
			"this payment is posted and cannot be changed").WithParam("number", p.Number)
	case Cancelled:
		return errs.Conflict(CodePaymentNotDraft, "this payment was cancelled")
	}
	return nil
}

// Allocation is one payment settling one document.
type Allocation struct {
	ID          id.ID
	DocumentID  id.ID
	AmountMinor int64
}

// AllocatedTotal is what a set of allocations comes to.
func AllocatedTotal(allocations []Allocation) int64 {
	var total int64
	for _, allocation := range allocations {
		total += allocation.AmountMinor
	}
	return total
}

// RequireAllocatable checks that a payment can cover what it is being asked to settle.
//
// # Unallocated is legitimate
//
// A deposit against nothing yet is a real thing a shop takes, so a payment allocated to less than
// its amount is fine. What is not fine is allocating MORE than was received — that is settling
// invoices with money nobody handed over, and it would leave the receivables ledger showing
// customers as paid up while the till is short.
func (p Payment) RequireAllocatable(allocations []Allocation) error {
	total := AllocatedTotal(allocations)
	if total > p.AmountMinor {
		return errs.Validation(CodeOverAllocated,
			"more is being settled than was paid").
			WithParam("allocated", itoa(total)).WithParam("paid", itoa(p.AmountMinor))
	}
	for _, allocation := range allocations {
		if allocation.AmountMinor <= 0 {
			return errs.Validation(CodeInvalidPayment,
				"an allocation must be for a positive amount")
		}
	}
	return nil
}

// Outstanding is what a document still owes.
//
// Derived from the document's total less what has been allocated to it — never a maintained
// column. A `paid_minor` field would drift from the allocations that justify it, and the drift
// would show up as a customer chased for money they had paid.
func Outstanding(totalMinor, allocatedMinor int64) int64 {
	return settle.Outstanding(totalMinor, allocatedMinor)
}
