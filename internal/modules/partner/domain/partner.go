// Package domain holds the partner module's business rules.
//
// One type serves customers and suppliers (decision 9). A business that both buys from and sells
// to the same company is ordinary, and two types would make that either a duplicate identity or
// a join nobody remembers to write.
package domain

import (
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeInvalidPartner = "partner.invalid"
	CodeNoRole         = "partner.no_role"
	CodeRoleInUse      = "partner.role_in_use"
	CodePartnerHistory = "partner.has_history"
	CodeInvalidAddress = "partner.invalid_address"
	CodeInvalidContact = "partner.invalid_contact"
	CodeCreditExceeded = "partner.credit_limit_exceeded"
	CodeNegativeCredit = "partner.negative_credit_limit"
)

// PartnerType distinguishes a person from a company.
//
// Recorded rather than inferred from whether a tax number is present: several countries apply
// different rules to the two, and a business may well hold a tax number for a sole trader.
type PartnerType string

// The partner types.
const (
	TypeIndividual PartnerType = "individual"
	TypeCompany    PartnerType = "company"
)

// Partner is a customer, a supplier, or both.
type Partner struct {
	ID        id.ID
	Code      string
	Name      string
	LegalName string
	Type      PartnerType

	// The roles. At least one is always set — see NewPartner.
	IsCustomer bool
	IsSupplier bool

	TaxNumber       string
	TaxGroupID      id.ID
	IsTaxExempt     bool
	TaxExemptReason string

	CurrencyCode        string
	PaymentTermsDays    int
	CreditLimitMinor    int64
	ReceivableAccountID id.ID
	PayableAccountID    id.ID

	Phone   string
	Email   string
	Website string
	Notes   string

	// HasHistory is set the first time the partner appears on a document. The domain only reads
	// it, to refuse deletion.
	HasHistory bool
	IsActive   bool
}

// NewPartner builds a partner, or refuses.
//
// # At least one role
//
// A partner who is neither a customer nor a supplier is a row nothing can ever reference: no
// document can name them, no report can include them, and no screen lists them. It is not a
// harmless empty state — it is a record that looks like data and is not, and the person who
// created it will believe the customer exists until the first invoice cannot find them.
//
// Both roles at once is not merely allowed but expected. That is the whole reason there is one
// type here rather than two.
func NewPartner(
	identifier id.ID, code, name string, isCustomer, isSupplier bool,
) (Partner, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	name = strings.TrimSpace(name)

	if identifier.IsZero() {
		return Partner{}, errs.Validation(CodeInvalidPartner, "a partner needs an identity")
	}
	if code == "" {
		return Partner{}, errs.Validation(CodeInvalidPartner, "a partner needs a code").
			WithField("code", CodeInvalidPartner, "required")
	}
	if name == "" {
		return Partner{}, errs.Validation(CodeInvalidPartner, "a partner needs a name").
			WithField("name", CodeInvalidPartner, "required")
	}
	if !isCustomer && !isSupplier {
		return Partner{}, errs.Validation(CodeNoRole,
			"a partner must be a customer, a supplier, or both").WithParam("code", code)
	}

	return Partner{
		ID: identifier, Code: code, Name: name, Type: TypeCompany,
		IsCustomer: isCustomer, IsSupplier: isSupplier,
		IsActive: true,
	}, nil
}

// Adopt rebuilds a partner from storage without re-running construction rules.
//
// Rows already in the database were valid when written; re-validating on every read would make a
// rule added in a later version retroactively unable to load old data.
func Adopt(p Partner) Partner { return p }

// SetRoles changes which roles a partner holds, or refuses.
//
// # Removing a role a partner has used
//
// Refused. A supplier who has been billed appears on purchase documents and in the payables
// ledger; clearing the flag would hide them from the supplier list while their balance sits in
// the accounts, which is how a payable becomes invisible without becoming settled. Deactivating
// the partner is the answer when they are no longer traded with, and it leaves every document
// and every balance intact.
//
// Adding a role is always allowed: a customer who starts supplying is ordinary, and there is
// nothing to contradict.
func (p Partner) SetRoles(isCustomer, isSupplier bool, usage RoleUsage) (Partner, error) {
	if !isCustomer && !isSupplier {
		return p, errs.Validation(CodeNoRole,
			"a partner must be a customer, a supplier, or both").WithParam("code", p.Code)
	}
	if p.IsCustomer && !isCustomer && usage.SoldTo {
		return p, errs.Conflict(CodeRoleInUse,
			"this partner has been sold to and cannot stop being a customer").
			WithParam("code", p.Code)
	}
	if p.IsSupplier && !isSupplier && usage.PurchasedFrom {
		return p, errs.Conflict(CodeRoleInUse,
			"this partner has been purchased from and cannot stop being a supplier").
			WithParam("code", p.Code)
	}
	p.IsCustomer = isCustomer
	p.IsSupplier = isSupplier
	return p, nil
}

// RoleUsage reports whether each role has actually been used.
//
// A port, satisfied by Phase 5 and Phase 6 when documents exist. Declared here, at the point of
// use, exactly as identity declared its Organisation port before org existed (1.2) — the rule it
// feeds is written and tested now rather than trusted until then.
type RoleUsage struct {
	SoldTo        bool
	PurchasedFrom bool
}

// SetCreditLimit changes how much a customer may owe.
//
// Zero means NO LIMIT, not "no credit". A limit of zero refusing every sale is never what
// leaving the field alone was meant to say, and the two readings differ by every sale the
// business makes.
func (p Partner) SetCreditLimit(minor int64) (Partner, error) {
	if minor < 0 {
		return p, errs.Validation(CodeNegativeCredit,
			"a credit limit cannot be negative").WithParam("code", p.Code)
	}
	p.CreditLimitMinor = minor
	return p, nil
}

// CheckCredit reports whether a further amount would breach the partner's limit.
//
// Read by Phase 5 before confirming a sale. It lives here because the rule — and specifically
// what zero means — must have exactly one home, and a check written again at the point of sale
// is a second place for "0 means unlimited" to be forgotten.
func (p Partner) CheckCredit(outstandingMinor, additionalMinor int64) error {
	if p.CreditLimitMinor == 0 {
		return nil
	}
	if outstandingMinor+additionalMinor > p.CreditLimitMinor {
		return errs.Conflict(CodeCreditExceeded,
			"this sale would take the customer past their credit limit").
			WithParam("code", p.Code)
	}
	return nil
}

// Deactivate retires a partner.
//
// Always available, including for one with history — which is the point. Retiring is what a
// business does to a customer it no longer trades with, and it leaves every past document able
// to reprint and every balance where it is.
func (p Partner) Deactivate() Partner {
	p.IsActive = false
	return p
}

// RequireDeletable refuses to delete a partner that appears on any document.
func (p Partner) RequireDeletable() error {
	if p.HasHistory {
		return errs.Conflict(CodePartnerHistory,
			"this partner appears on documents and can be deactivated but not deleted").
			WithParam("code", p.Code)
	}
	return nil
}

// ── addresses ───────────────────────────────────────────────────────────────────

// AddressType is what an address is used for.
type AddressType string

// The address types. `both` is the common case and the default: most partners have one address
// and it serves for everything.
const (
	AddressBilling  AddressType = "billing"
	AddressShipping AddressType = "shipping"
	AddressBoth     AddressType = "both"
)

// Address is one of a partner's addresses.
//
// Free-form lines rather than street/number/district columns: addresses differ enormously
// between the countries §1 says this must serve, and a schema shaped around one country forces
// every other into the wrong boxes.
type Address struct {
	ID          id.ID
	PartnerID   id.ID
	Label       string
	Type        AddressType
	Line1       string
	Line2       string
	City        string
	Region      string
	PostalCode  string
	CountryCode string

	ContactName  string
	ContactPhone string

	IsDefault bool
	IsActive  bool
}

// NewAddress builds an address, or refuses.
func NewAddress(
	identifier, partnerID id.ID, label, line1 string, addressType AddressType,
) (Address, error) {
	label = strings.TrimSpace(label)
	line1 = strings.TrimSpace(line1)

	if identifier.IsZero() || partnerID.IsZero() {
		return Address{}, errs.Validation(CodeInvalidAddress,
			"an address needs an identity and a partner")
	}
	if line1 == "" {
		return Address{}, errs.Validation(CodeInvalidAddress,
			"an address needs at least one line").WithField("line1", CodeInvalidAddress, "required")
	}
	if label == "" {
		// A partner with three unlabelled addresses gives a delivery driver nothing to choose
		// between, so the label is required rather than defaulted to something meaningless.
		return Address{}, errs.Validation(CodeInvalidAddress,
			"an address needs a label").WithField("label", CodeInvalidAddress, "required")
	}
	if addressType == "" {
		addressType = AddressBoth
	}
	switch addressType {
	case AddressBilling, AddressShipping, AddressBoth:
	default:
		return Address{}, errs.Validation(CodeInvalidAddress,
			"that is not a kind of address").WithParam("type", string(addressType))
	}

	return Address{
		ID: identifier, PartnerID: partnerID, Label: label, Type: addressType,
		Line1: line1, IsActive: true,
	}, nil
}

// ── contacts ────────────────────────────────────────────────────────────────────

// Contact is a person at a partner.
type Contact struct {
	ID        id.ID
	PartnerID id.ID
	Name      string
	Role      string
	Phone     string
	Email     string
	IsPrimary bool
	IsActive  bool
}

// NewContact builds a contact, or refuses.
func NewContact(identifier, partnerID id.ID, name string) (Contact, error) {
	name = strings.TrimSpace(name)

	if identifier.IsZero() || partnerID.IsZero() {
		return Contact{}, errs.Validation(CodeInvalidContact,
			"a contact needs an identity and a partner")
	}
	if name == "" {
		return Contact{}, errs.Validation(CodeInvalidContact, "a contact needs a name").
			WithField("name", CodeInvalidContact, "required")
	}
	return Contact{
		ID: identifier, PartnerID: partnerID, Name: name, IsActive: true,
	}, nil
}
