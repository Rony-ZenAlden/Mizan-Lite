// Package domain holds the accounting module's business rules (§20).
//
// Nothing here touches a database, a clock, or a context. The rules that make a general ledger
// defensible — a leaf-only posting surface, a normal balance derived from the account type, a
// hierarchy that cannot form a cycle — are decisions, not queries, and they belong where they
// can be tested without one.
package domain

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeInvalidAccount     = "accounting.invalid_account"
	CodeAccountNotPostable = "accounting.account_not_postable"
	CodeAccountCycle       = "accounting.account_cycle"
	CodeParentNotFound     = "accounting.parent_not_found"
	CodeSystemAccount      = "accounting.system_account"
	CodeUnknownMapping     = "accounting.unknown_mapping"
)

// AccountType is the top-level classification every account has (§20.1).
type AccountType string

const (
	Asset     AccountType = "asset"
	Liability AccountType = "liability"
	Equity    AccountType = "equity"
	Revenue   AccountType = "revenue"
	Expense   AccountType = "expense"
)

// Side is which column an amount lands in.
type Side string

const (
	Debit  Side = "debit"
	Credit Side = "credit"
)

// NormalBalance is the side on which an account type carries a positive balance.
//
// This is accounting's one piece of universally settled arithmetic — assets and expenses
// increase on the debit side, everything else on the credit side — and it is the same in every
// country, which is why it is one of the very few things in this product that IS in code.
func NormalBalance(t AccountType) Side {
	switch t {
	case Asset, Expense:
		return Debit
	default:
		return Credit
	}
}

// ValidType reports whether a string names a real account type.
func ValidType(t AccountType) bool {
	switch t {
	case Asset, Liability, Equity, Revenue, Expense:
		return true
	}
	return false
}

// Account is one line of the chart of accounts.
type Account struct {
	ID        id.ID
	CompanyID id.ID
	Code      string
	Name      string
	NameKey   string

	ParentID id.ID
	Path     string
	Depth    int

	Type    AccountType
	Subtype string
	Normal  Side

	CurrencyCode string
	BranchID     id.ID

	IsPostable bool
	IsSystem   bool
	IsActive   bool

	Description string
}

// NewAccount builds an account beneath an optional parent.
//
// The parent is passed as a VALUE rather than an id, because two of the rules here need to read
// it: the path is built from the parent's path, and the type must match. A constructor that
// took an id would have to trust the caller had already checked both.
func NewAccount(
	identifier, companyID id.ID, code, name string, accountType AccountType, parent *Account,
) (Account, error) {
	code = strings.TrimSpace(code)
	name = strings.TrimSpace(name)

	if identifier.IsZero() || companyID.IsZero() {
		return Account{}, errs.Validation(CodeInvalidAccount, "an account needs an identity and a company")
	}
	if code == "" {
		return Account{}, errs.Validation(CodeInvalidAccount, "an account needs a code").
			WithField("code", CodeInvalidAccount, "required")
	}
	if name == "" {
		return Account{}, errs.Validation(CodeInvalidAccount, "an account needs a name").
			WithField("name", CodeInvalidAccount, "required")
	}
	if !ValidType(accountType) {
		return Account{}, errs.Validation(CodeInvalidAccount, "that is not an account type").
			WithParam("type", string(accountType))
	}

	account := Account{
		ID: identifier, CompanyID: companyID, Code: code, Name: name,
		Type: accountType, Normal: NormalBalance(accountType),
		// New accounts are postable and non-system by default. A parent stops being postable
		// the moment it acquires a child (see Adopt) — which is the only way a roll-up account
		// can come into existence, and it means the caller never has to remember the rule.
		IsPostable: true, IsActive: true,
		Path: "/" + code + "/",
	}

	if parent != nil {
		if parent.CompanyID != companyID {
			return Account{}, errs.Validation(CodeInvalidAccount,
				"an account cannot sit under a parent in another company")
		}
		// A revenue account under an asset parent would roll up into the wrong statement, and
		// the totals would still add up — the silent-wrongness failure this phase is shaped
		// around (§1.5).
		if parent.Type != accountType {
			return Account{}, errs.Validation(CodeInvalidAccount,
				"an account must have the same type as its parent").
				WithParam("parent", string(parent.Type)).WithParam("child", string(accountType))
		}
		account.ParentID = parent.ID
		account.Depth = parent.Depth + 1
		account.Path = parent.Path + code + "/"
	}

	return account, nil
}

// Adopt marks an account as a parent: it gains a child and therefore stops accepting postings.
//
// Returned rather than mutated in place so the caller must persist the result — a rule that
// only takes effect if somebody remembers to save is not a rule.
func (a Account) Adopt() Account {
	a.IsPostable = false
	return a
}

// RequirePostable refuses a posting to an account that cannot take one.
//
// Called by the journal aggregate for every line. Both conditions matter and they fail for
// different reasons: a roll-up account would double-count into its own ancestors, and an
// inactive account is one somebody deliberately took out of use.
func (a Account) RequirePostable() error {
	if !a.IsPostable {
		return errs.Validation(CodeAccountNotPostable,
			"this account is a heading and cannot take postings").WithParam("account", a.Code)
	}
	if !a.IsActive {
		return errs.Validation(CodeAccountNotPostable,
			"this account is no longer in use").WithParam("account", a.Code)
	}
	return nil
}

// IsUnder reports whether the account sits anywhere beneath a prefix path.
//
// The materialised path earning its keep: "every asset account" is a string prefix rather than
// a recursive walk (§20.1).
func (a Account) IsUnder(path string) bool {
	return strings.HasPrefix(a.Path, path)
}

// The mapping keys the posting layer resolves through (§20.3).
//
// Constants rather than free strings because a posting rule naming `TAX_PAYBLE` would resolve
// to nothing and post a half-entry — and the failure would appear in a trial balance weeks
// later rather than at the typo.
const (
	MappingAR             = "AR"
	MappingAP             = "AP"
	MappingInventory      = "INVENTORY"
	MappingCOGS           = "COGS"
	MappingSales          = "SALES"
	MappingTaxPayable     = "TAX_PAYABLE"
	MappingTaxReceivable  = "TAX_RECEIVABLE"
	MappingCash           = "CASH"
	MappingBank           = "BANK"
	MappingFXGainLoss     = "FX_GAIN_LOSS"
	MappingRounding       = "ROUNDING_DIFF"
	MappingRetained       = "RETAINED_EARNINGS"
	MappingOpeningBalance = "OPENING_BALANCE"
)

// RequiredMappings are the keys a chart of accounts must supply to be usable.
//
// Checked when a chart is loaded rather than when the first invoice posts. The alternative is
// discovering that a customer's imported chart has no receivables account at the moment they
// try to sell something — which is both the worst time and the hardest to explain.
func RequiredMappings() []string {
	return []string{
		MappingAR, MappingAP, MappingSales, MappingCOGS, MappingInventory,
		MappingTaxPayable, MappingTaxReceivable, MappingCash,
		MappingFXGainLoss, MappingRounding, MappingRetained, MappingOpeningBalance,
	}
}
