package domain

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable codes for debts.
const (
	CodeInvalidDebt      = "expenses.invalid_debt"
	CodeDebtPosted       = "expenses.debt_posted"
	CodeUnknownDebtKind  = "expenses.unknown_debt_kind"
	CodeDebtNotBalancing = "expenses.debt_not_balancing"
)

// Direction is which way the money went.
//
// Not a sign on the amount. A signed amount makes SUM(amount) meaningless and every query a
// minefield — the same reasoning that keeps stock movement quantities positive (§4.1) and payment
// amounts positive.
type Direction string

// The directions.
const (
	Received Direction = "received"
	Paid     Direction = "paid"
)

// Kind is what sort of obligation moved.
//
// A CLOSED set of three, deliberately. Expense categories are open-ended and therefore carry
// their own account; these are not, so they go through mappings like everything else — using
// 7.1's document-named exception here would be naming accounts in Go with extra steps.
type Kind string

// The debt kinds.
const (
	// LoanPayable is money the business borrowed, and owes back.
	LoanPayable Kind = "loan_payable"
	// LoanReceivable is money the business lent, and is owed — an advance against wages, a float
	// given to a driver.
	LoanReceivable Kind = "loan_receivable"
	// OwnerEquity is the owner putting capital in or taking it out. Not a debt in the legal
	// sense, and it shares this document because the money movement is identical and a business
	// owner making one of these is doing the same act.
	OwnerEquity Kind = "owner_equity"
)

// Valid reports whether a kind is one this build knows.
func (k Kind) Valid() bool {
	switch k {
	case LoanPayable, LoanReceivable, OwnerEquity:
		return true
	default:
		return false
	}
}

// AmountKey is the posting amount this kind fills in.
//
// Exactly one of the three is ever non-zero and the rest resolve to nothing, which Phase 2 skips.
// The same shape as the variance split (6.3) and cash short/over (5.6), for the same reason: the
// posting engine refuses negative amounts, so a rule that must move money in either direction
// gets separate lines rather than a sign.
func (k Kind) AmountKey() string {
	return "document." + string(k)
}

// Debt is money in or out with no trade document behind it.
type Debt struct {
	ID        id.ID
	CompanyID id.ID
	BranchID  id.ID

	Status    Status
	Number    string
	Direction Direction
	Kind      Kind

	PartnerID        id.ID
	CounterpartyName string

	DebtDate    string
	DueDate     string
	Method      Method
	Reference   string
	Description string

	CurrencyCode string
	AmountMinor  int64
}

// NewDebt builds a draft debt, or refuses.
func NewDebt(
	identifier, companyID, branchID id.ID,
	counterpartyName, debtDate, currencyCode string,
	direction Direction, kind Kind, method Method, amountMinor int64,
) (Debt, error) {
	counterpartyName = strings.TrimSpace(counterpartyName)

	if identifier.IsZero() || companyID.IsZero() || branchID.IsZero() {
		return Debt{}, errs.Validation(CodeInvalidDebt,
			"a debt needs an identity, a company, and a branch")
	}
	if counterpartyName == "" {
		// A NAME, not a partner. The owner is usually not a partner record and a loan from a
		// relative rarely is either — but money that moved to or from nobody is a hole in the
		// cash position nobody can explain.
		return Debt{}, errs.Validation(CodeInvalidDebt,
			"a debt needs to say who the money came from or went to").
			WithField("counterparty_name", CodeInvalidDebt, "required")
	}
	if strings.TrimSpace(debtDate) == "" {
		return Debt{}, errs.Validation(CodeInvalidDebt, "a debt needs a date")
	}
	if direction != Received && direction != Paid {
		return Debt{}, errs.Validation(CodeInvalidDebt,
			"a debt must say which way the money went").
			WithParam("direction", string(direction))
	}
	if !kind.Valid() {
		return Debt{}, errs.Validation(CodeUnknownDebtKind,
			"that is not a kind of obligation this build knows").
			WithParam("kind", string(kind))
	}
	if !method.Valid() {
		return Debt{}, errs.Validation(CodeInvalidDebt,
			"a debt must say how the money moved").WithParam("method", string(method))
	}
	if amountMinor <= 0 {
		return Debt{}, errs.Validation(CodeNonPositiveAmount,
			"a debt must be for a positive amount")
	}

	return Debt{
		ID: identifier, CompanyID: companyID, BranchID: branchID,
		Status: Draft, Direction: direction, Kind: kind,
		CounterpartyName: counterpartyName, DebtDate: debtDate, Method: method,
		CurrencyCode: strings.ToUpper(currencyCode), AmountMinor: amountMinor,
	}, nil
}

// RequireDraft refuses to change a posted debt.
func (d Debt) RequireDraft() error {
	switch d.Status {
	case Draft:
		return nil
	case Posted:
		return errs.Conflict(CodeDebtPosted,
			"this debt has been posted; correct it with a movement the other way")
	default:
		return errs.Conflict(CodeCancelled, "this debt was cancelled")
	}
}

// PostingAction is the rule key this debt fires.
//
// Direction and METHOD, not kind: the kind decides which obligation account moves, and that is an
// AMOUNT the rule reads. Putting the kind in the action too would give twenty-four rules where
// eight say the same thing.
func (d Debt) PostingAction() string {
	return "debts." + string(d.Direction) + "." + string(d.Method)
}

// KindAmounts is the posting amounts for a debt: its own kind, and zero for the others.
//
// All three keys are present, and that is deliberate. A missing key and a zero one behave
// identically to Phase 2 today — both skip the line — but a caller reading this map should see
// the shape rather than infer it, and a fourth kind added later fails loudly here rather than
// silently posting nothing.
func (d Debt) KindAmounts() map[string]int64 {
	amounts := map[string]int64{
		LoanPayable.AmountKey():    0,
		LoanReceivable.AmountKey(): 0,
		OwnerEquity.AmountKey():    0,
	}
	amounts[d.Kind.AmountKey()] = d.AmountMinor
	return amounts
}

// RequireBalances checks that exactly one obligation line carries the whole amount.
//
// # Why this is checked rather than assumed
//
// The posting has one money line at the document's total and three obligation lines of which one
// should carry the same total. If two ever carried an amount, the entry would be out of balance
// and Phase 2 would refuse it — but it would refuse it with "this entry does not balance", which
// says nothing about which document or why.
//
// Checked here, the message names the debt.
func (d Debt) RequireBalances() error {
	var total int64
	for _, amount := range d.KindAmounts() {
		total += amount
	}
	if total != d.AmountMinor {
		return errs.Internal(CodeDebtNotBalancing,
			"a debt's obligation lines do not come to its amount").
			WithParam("amount", itoa(d.AmountMinor)).WithParam("lines", itoa(total))
	}
	return nil
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	var digits [20]byte
	i := len(digits)
	for v > 0 {
		i--
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
