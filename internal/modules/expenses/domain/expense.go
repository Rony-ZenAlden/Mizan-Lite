// Package domain holds the expenses module's rules, with no knowledge of storage.
package domain

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable codes, doubling as i18n keys.
const (
	CodeInvalidExpense    = "expenses.invalid_expense"
	CodeInvalidLine       = "expenses.invalid_line"
	CodeInvalidCategory   = "expenses.invalid_category"
	CodeNotDraft          = "expenses.not_draft"
	CodeAlreadyPosted     = "expenses.already_posted"
	CodeCancelled         = "expenses.cancelled"
	CodeNoLines           = "expenses.no_lines"
	CodeNonPositiveAmount = "expenses.non_positive_amount"
	CodeNoPayee           = "expenses.no_payee"
	CodeMethodMismatch    = "expenses.method_mismatch"
	CodeTemplateNotPosted = "expenses.template_not_posted"
	CodeInvalidPayment    = "expenses.invalid_payment"
	CodeAlreadyPaid       = "expenses.already_paid"
	CodeOverAllocated     = "expenses.over_allocated"
	CodeOverSettled       = "expenses.over_settled"
)

// Settlement is whether an expense is already paid.
type Settlement string

// The settlements.
//
// The one field that gives two documents from one table, and the distinction is a real business
// one: fuel bought with cash is spent and gone; rent invoiced monthly is owed until paid. The same
// fact at different moments.
const (
	Immediate Settlement = "immediate"
	OnAccount Settlement = "on_account"
)

// Method is how an immediate expense was paid.
type Method string

// The payment methods. The same four purchasing uses, for the same reason: the method becomes
// part of the posting ACTION and the seeded rules decide which account the money left (§20.3).
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

// Status is where an expense has got to.
type Status string

// The statuses.
const (
	Draft     Status = "draft"
	Posted    Status = "posted"
	Cancelled Status = "cancelled"
)

// Category is a kind of spending, and where it lands.
type Category struct {
	ID             id.ID
	CompanyID      id.ID
	Code           string
	Name           string
	NameKey        string
	AccountID      id.ID
	TaxRecoverable bool
	ParentID       id.ID
	SortOrder      int
	IsActive       bool
}

// NewCategory builds a category, or refuses.
func NewCategory(
	identifier, companyID, accountID id.ID, code, name string,
) (Category, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	name = strings.TrimSpace(name)

	if identifier.IsZero() || companyID.IsZero() {
		return Category{}, errs.Validation(CodeInvalidCategory,
			"a category needs an identity and a company")
	}
	if code == "" {
		return Category{}, errs.Validation(CodeInvalidCategory, "a category needs a code").
			WithField("code", CodeInvalidCategory, "required")
	}
	if name == "" {
		return Category{}, errs.Validation(CodeInvalidCategory, "a category needs a name")
	}
	if accountID.IsZero() {
		// Without an account there is nowhere for the spending to land, and an expense against
		// this category could be entered and then fail at posting — after somebody has typed it.
		return Category{}, errs.Validation(CodeInvalidCategory,
			"a category needs an account to post to").WithParam("code", code)
	}

	return Category{
		ID: identifier, CompanyID: companyID, AccountID: accountID,
		Code: code, Name: name, TaxRecoverable: true, IsActive: true,
	}, nil
}

// Expense is money out that buys no stock.
type Expense struct {
	ID        id.ID
	CompanyID id.ID
	BranchID  id.ID

	Status Status
	Number string

	// PartnerID is usually zero. The electricity company is a partner if you want a statement
	// from them and a name on a report if you do not.
	PartnerID   id.ID
	PayeeName   string
	ExpenseDate string
	Reference   string
	Description string

	Settlement Settlement
	PaidMethod Method
	DueDate    string

	CurrencyCode      string
	ExchangeRateMicro int64

	NetMinor   int64
	TaxMinor   int64
	TotalMinor int64

	IsTemplate        bool
	RecursEveryMonths int
}

// NewExpense builds a draft expense, or refuses.
func NewExpense(
	identifier, companyID, branchID id.ID,
	payeeName, expenseDate, currencyCode string,
	settlement Settlement, method Method,
) (Expense, error) {
	payeeName = strings.TrimSpace(payeeName)

	if identifier.IsZero() || companyID.IsZero() || branchID.IsZero() {
		return Expense{}, errs.Validation(CodeInvalidExpense,
			"an expense needs an identity, a company, and a branch")
	}
	if payeeName == "" {
		// A payee NAME, not a partner. Money leaving the business went to somebody, and "cash,
		// no record of who" is how a cash position stops being defensible — but forcing a
		// partner record for every taxi fare is how a partner list becomes unusable.
		return Expense{}, errs.Validation(CodeNoPayee,
			"an expense needs to say who was paid").
			WithField("payee_name", CodeNoPayee, "required")
	}
	if strings.TrimSpace(expenseDate) == "" {
		return Expense{}, errs.Validation(CodeInvalidExpense, "an expense needs a date")
	}
	if err := requireMethodMatches(settlement, method); err != nil {
		return Expense{}, err
	}

	return Expense{
		ID: identifier, CompanyID: companyID, BranchID: branchID,
		Status: Draft, PayeeName: payeeName, ExpenseDate: expenseDate,
		Settlement: settlement, PaidMethod: method,
		CurrencyCode: strings.ToUpper(currencyCode), ExchangeRateMicro: 1_000_000,
	}, nil
}

// requireMethodMatches keeps the settlement and the method consistent.
//
// An expense paid immediately must say HOW, because the method decides which account the money
// left. One on account must NOT, because it has not been paid — naming a method would assert
// something that has not happened, and a report of "what went out of the bank" would count it.
func requireMethodMatches(settlement Settlement, method Method) error {
	switch settlement {
	case Immediate:
		if !method.Valid() {
			return errs.Validation(CodeMethodMismatch,
				"an expense paid now must say how it was paid").
				WithField("paid_method", CodeMethodMismatch, "required")
		}
	case OnAccount:
		if method != "" {
			return errs.Validation(CodeMethodMismatch,
				"an expense on account has not been paid, so it cannot name a method")
		}
	default:
		return errs.Validation(CodeInvalidExpense,
			"that is not a way of settling an expense").
			WithParam("settlement", string(settlement))
	}
	return nil
}

// RequireDraft refuses to change a posted expense.
func (e Expense) RequireDraft() error {
	switch e.Status {
	case Draft:
		return nil
	case Posted:
		return errs.Conflict(CodeAlreadyPosted,
			"this expense has been posted; correct it with a reversal")
	default:
		return errs.Conflict(CodeCancelled, "this expense was cancelled")
	}
}

// RequirePostable refuses to post a template.
//
// A template is a form to fill in, not a document. Posting one would put a specimen rent payment
// in the books every time somebody opened the screen.
func (e Expense) RequirePostable() error {
	if e.IsTemplate {
		return errs.Conflict(CodeTemplateNotPosted,
			"a recurring template is a form to fill in, not an expense")
	}
	return e.RequireDraft()
}

// PostingAction is the rule key this expense fires.
//
// # The settlement and the method are part of the ACTION
//
// An expense paid in cash credits the till; one paid by transfer credits the bank; one on account
// credits payables and touches no money at all. Three different entries, and none of them decided
// here — the action names what HAPPENED and the seeded rules decide where it lands (§20.3).
//
// The same shape 6.7 proved twice, and 5.5 before it.
func (e Expense) PostingAction() string {
	if e.Settlement == OnAccount {
		return "expenses.expense.on_account"
	}
	return "expenses.expense.paid." + string(e.PaidMethod)
}

// Line is one thing spent on.
type Line struct {
	ID         id.ID
	LineNumber int

	CategoryID id.ID
	// AccountID and CategoryName are SNAPSHOTTED (§9.3). A category re-pointed at a different
	// account next year must not move where a posted expense landed — the journal entry would
	// say one thing and the document another.
	AccountID    id.ID
	CategoryName string

	Description string

	NetMinor       int64
	TaxRateMicro   int64
	TaxCode        string
	TaxAmountMinor int64
	TaxRecoverable bool
	TotalMinor     int64
}

// NewLine builds an expense line, or refuses.
func NewLine(
	identifier, categoryID, accountID id.ID,
	lineNumber int, categoryName string, netMinor int64,
) (Line, error) {
	if identifier.IsZero() || categoryID.IsZero() {
		return Line{}, errs.Validation(CodeInvalidLine,
			"a line needs an identity and a category")
	}
	if accountID.IsZero() {
		return Line{}, errs.Validation(CodeInvalidLine,
			"a line needs the account its category posts to")
	}
	if netMinor <= 0 {
		// Spending nothing is not an expense, and a NEGATIVE one is a refund — which is money
		// coming back and belongs in a document that says so.
		return Line{}, errs.Validation(CodeNonPositiveAmount,
			"an expense line must be for a positive amount")
	}

	return Line{
		ID: identifier, LineNumber: lineNumber,
		CategoryID: categoryID, AccountID: accountID, CategoryName: categoryName,
		NetMinor: netMinor, TaxRecoverable: true, TotalMinor: netMinor,
	}, nil
}

// Tax applies a resolved tax to a line.
//
// # Recoverable is per LINE, and that is not pedantry
//
// Most jurisdictions disallow reclaiming tax on entertainment and allow it on fuel, and a single
// expense claim routinely carries both. Deciding it per document would force somebody to split
// one receipt into two, which is how the rule gets ignored.
func (l Line) Tax(amountMinor, rateMicro int64, code string, recoverable bool) Line {
	l.TaxAmountMinor = amountMinor
	l.TaxRateMicro = rateMicro
	l.TaxCode = code
	l.TaxRecoverable = recoverable
	l.TotalMinor = l.NetMinor + amountMinor
	return l
}

// RecoverableTaxMinor is the part of a line's tax that may be reclaimed.
//
// Tax on a non-recoverable line is not lost — it becomes part of what the thing COST, so it lands
// in the expense account rather than in the recoverable-tax asset. A business that claimed it
// anyway would be making a claim a revenue authority disallows.
func (l Line) RecoverableTaxMinor() int64 {
	if !l.TaxRecoverable {
		return 0
	}
	return l.TaxAmountMinor
}

// CostMinor is what this line actually cost, including any tax that cannot be reclaimed.
func (l Line) CostMinor() int64 {
	return l.NetMinor + l.TaxAmountMinor - l.RecoverableTaxMinor()
}

// Totals recomputes an expense's money from its lines.
//
// Summed from the LINES, never accumulated as they are added: an accumulated total drifts the
// moment a line is edited, and the drift is invisible because the document still balances against
// itself.
func Totals(lines []Line) (net, tax, total, recoverable int64) {
	for _, line := range lines {
		net += line.NetMinor
		tax += line.TaxAmountMinor
		total += line.TotalMinor
		recoverable += line.RecoverableTaxMinor()
	}
	return net, tax, total, recoverable
}

// CostByAccount groups what each account should be debited.
//
// One expense can spread across several categories — a garage invoice with parts and labour — and
// each lands in its own account. Grouped here rather than posted line by line, so a document with
// five lines against one category produces one journal line rather than five.
func CostByAccount(lines []Line) map[id.ID]int64 {
	out := make(map[id.ID]int64, len(lines))
	for _, line := range lines {
		out[line.AccountID] += line.CostMinor()
	}
	return out
}

// ── settling what is owed ───────────────────────────────────────────────────────

// PaymentStatus is where a settlement has got to.
type PaymentStatus string

// The settlement statuses.
const (
	PaymentDraft     PaymentStatus = "draft"
	PaymentPosted    PaymentStatus = "posted"
	PaymentCancelled PaymentStatus = "cancelled"
)

// Payment is money paying off an expense recorded on account.
type Payment struct {
	ID        id.ID
	CompanyID id.ID
	BranchID  id.ID

	PartnerID id.ID
	PayeeName string

	Number      string
	PaymentDate string
	Method      Method
	Reference   string

	CurrencyCode string
	AmountMinor  int64

	Status PaymentStatus
}

// NewPayment builds a settlement, or refuses.
func NewPayment(
	identifier, companyID, branchID id.ID,
	payeeName, paymentDate, currencyCode string, method Method, amountMinor int64,
) (Payment, error) {
	payeeName = strings.TrimSpace(payeeName)

	if identifier.IsZero() || companyID.IsZero() || branchID.IsZero() {
		return Payment{}, errs.Validation(CodeInvalidPayment,
			"a settlement needs an identity, a company, and a branch")
	}
	if payeeName == "" {
		return Payment{}, errs.Validation(CodeNoPayee,
			"a settlement needs to say who was paid")
	}
	if strings.TrimSpace(paymentDate) == "" {
		return Payment{}, errs.Validation(CodeInvalidPayment, "a settlement needs a date")
	}
	if !method.Valid() {
		return Payment{}, errs.Validation(CodeInvalidPayment,
			"that is not a way of paying this build knows").WithParam("method", string(method))
	}
	if amountMinor <= 0 {
		return Payment{}, errs.Validation(CodeNonPositiveAmount,
			"a settlement must be for a positive amount")
	}

	return Payment{
		ID: identifier, CompanyID: companyID, BranchID: branchID,
		PayeeName: payeeName, PaymentDate: paymentDate, Method: method,
		CurrencyCode: strings.ToUpper(currencyCode), AmountMinor: amountMinor,
		Status: PaymentDraft,
	}, nil
}

// PostingAction is the rule key a settlement fires.
//
// The METHOD again, for the same reason: which account the money left is a seed-file decision.
func (p Payment) PostingAction() string {
	return "expenses.settlement." + string(p.Method)
}

// RequireSettleable refuses to settle an expense that owes nothing.
//
// Only an expense recorded ON ACCOUNT can be settled. One paid immediately is already gone, and
// settling it again would credit the bank twice for one payment.
func (e Expense) RequireSettleable() error {
	if e.Status != Posted {
		return errs.Conflict(CodeNotDraft,
			"only a recorded expense can be settled")
	}
	if e.Settlement != OnAccount {
		return errs.Conflict(CodeAlreadyPaid,
			"this expense was paid when it was recorded")
	}
	return nil
}
