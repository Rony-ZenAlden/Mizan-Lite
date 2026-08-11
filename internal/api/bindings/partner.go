package bindings

import (
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/modules/partner"
)

// PartnerRowDTO is one partner in the list.
//
// Both role flags cross, not one "type" string, because a partner who is both is ordinary and
// collapsing them to a single value would force the screen to invent a third label for the case
// the whole design exists to serve.
type PartnerRowDTO struct {
	ID         string `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	LegalName  string `json:"legalName"`
	Type       string `json:"partnerType"`
	IsCustomer bool   `json:"isCustomer"`
	IsSupplier bool   `json:"isSupplier"`

	TaxNumber string `json:"taxNumber"`
	Exempt    bool   `json:"isTaxExempt"`

	Currency string `json:"currency"`
	Terms    int    `json:"paymentTermsDays"`
	// CreditLimitMinor crosses as a string of minor units, like every other amount (§17).
	// "0" means NO LIMIT, and the screen must say so rather than printing a zero that reads as
	// "no credit allowed".
	CreditLimitMinor string `json:"creditLimitMinor"`

	Phone  string `json:"phone"`
	Email  string `json:"email"`
	Active bool   `json:"isActive"`
	// HasHistory tells the screen to offer "deactivate" rather than "delete", instead of
	// offering a delete button that always refuses.
	HasHistory bool `json:"hasHistory"`
}

// PartnerAddressDTO is one of a partner's addresses.
type PartnerAddressDTO struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Type        string `json:"addressType"`
	Line1       string `json:"line1"`
	Line2       string `json:"line2"`
	City        string `json:"city"`
	Region      string `json:"region"`
	PostalCode  string `json:"postalCode"`
	CountryCode string `json:"countryCode"`
	Default     bool   `json:"isDefault"`
}

// PartnerContactDTO is a person at a partner.
type PartnerContactDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Phone   string `json:"phone"`
	Email   string `json:"email"`
	Primary bool   `json:"isPrimary"`
}

// PartnerDetailDTO is everything the detail screen shows.
type PartnerDetailDTO struct {
	Partner   PartnerRowDTO       `json:"partner"`
	Addresses []PartnerAddressDTO `json:"addresses"`
	Contacts  []PartnerContactDTO `json:"contacts"`
	// RoleLocked reports which roles cannot be removed, so the screen explains rather than
	// offering a control that refuses.
	CustomerRoleLocked bool `json:"customerRoleLocked"`
	SupplierRoleLocked bool `json:"supplierRoleLocked"`
}

// Partners is the customer and supplier surface.
//
// Read-only for the same reason Catalog is: Phase 3 builds the master data and the screens that
// READ it. The forms come with the release that needs them.
type Partners struct{ graph }

// partnerPolicies declares what each method requires.
//
// # Why the detail method is split in two
//
// A partner is one row, but reading it as a CUSTOMER and reading it as a SUPPLIER are different
// permissions — a salesperson has no business seeing a supplier's payment terms. A single
// Partner(code) method would need "either permission", and the policy model deliberately holds
// one permission per method so that what a method requires is a fact of the declaration rather
// than something evaluated at call time.
//
// Widening the model for this would be inventing a seam for one caller. Two methods, each
// requiring exactly one permission, say the same thing without it — and the screens are two
// screens anyway.
func partnerPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"Customers": policy.Requires(partner.PermCustomerView),
		"Suppliers": policy.Requires(partner.PermSupplierView),
		"Customer":  policy.Requires(partner.PermCustomerView),
		"Supplier":  policy.Requires(partner.PermSupplierView),
	}
}

// Customers lists the partners who are customers.
func (p *Partners) Customers(search string) envelope.Result[[]PartnerRowDTO] {
	return p.list("Customers", partner.Filter{CustomersOnly: true, Search: search})
}

// Suppliers lists the partners who are suppliers.
func (p *Partners) Suppliers(search string) envelope.Result[[]PartnerRowDTO] {
	return p.list("Suppliers", partner.Filter{SuppliersOnly: true, Search: search})
}

func (p *Partners) list(method string, filter partner.Filter) envelope.Result[[]PartnerRowDTO] {
	ctx, app, err := p.guard(method)
	if err != nil {
		return envelope.Fail[[]PartnerRowDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]PartnerRowDTO](err)
	}
	partners, err := app.Partner.Partners(ctx, companyID, filter)
	if err != nil {
		return envelope.Fail[[]PartnerRowDTO](err)
	}

	out := make([]PartnerRowDTO, 0, len(partners))
	for _, row := range partners {
		out = append(out, partnerRow(row))
	}
	return envelope.Ok(out)
}

// Customer reads one customer with its addresses and contacts.
func (p *Partners) Customer(code string) envelope.Result[PartnerDetailDTO] {
	return p.detail("Customer", code)
}

// Supplier reads one supplier with its addresses and contacts.
func (p *Partners) Supplier(code string) envelope.Result[PartnerDetailDTO] {
	return p.detail("Supplier", code)
}

func (p *Partners) detail(method, code string) envelope.Result[PartnerDetailDTO] {
	ctx, app, err := p.guard(method)
	if err != nil {
		return envelope.Fail[PartnerDetailDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[PartnerDetailDTO](err)
	}
	found, err := app.Partner.PartnerByCode(ctx, companyID, code)
	if err != nil {
		return envelope.Fail[PartnerDetailDTO](err)
	}

	addresses, err := app.Partner.Addresses(ctx, found.ID)
	if err != nil {
		return envelope.Fail[PartnerDetailDTO](err)
	}
	addressDTOs := make([]PartnerAddressDTO, 0, len(addresses))
	for _, address := range addresses {
		addressDTOs = append(addressDTOs, PartnerAddressDTO{
			ID: string(address.ID), Label: address.Label, Type: string(address.Type),
			Line1: address.Line1, Line2: address.Line2, City: address.City,
			Region: address.Region, PostalCode: address.PostalCode,
			CountryCode: address.CountryCode, Default: address.IsDefault,
		})
	}

	contacts, err := app.Partner.Contacts(ctx, found.ID)
	if err != nil {
		return envelope.Fail[PartnerDetailDTO](err)
	}
	contactDTOs := make([]PartnerContactDTO, 0, len(contacts))
	for _, contact := range contacts {
		contactDTOs = append(contactDTOs, PartnerContactDTO{
			ID: string(contact.ID), Name: contact.Name, Role: contact.Role,
			Phone: contact.Phone, Email: contact.Email, Primary: contact.IsPrimary,
		})
	}

	usage, err := app.Partner.RoleUsage(ctx, found.ID)
	if err != nil {
		return envelope.Fail[PartnerDetailDTO](err)
	}

	return envelope.Ok(PartnerDetailDTO{
		Partner: partnerRow(found), Addresses: addressDTOs, Contacts: contactDTOs,
		// A role that has been traded in cannot be removed, so the screen says why rather than
		// offering a toggle that always refuses.
		CustomerRoleLocked: usage.SoldTo,
		SupplierRoleLocked: usage.PurchasedFrom,
	})
}

func partnerRow(row partner.Partner) PartnerRowDTO {
	return PartnerRowDTO{
		ID: string(row.ID), Code: row.Code, Name: row.Name, LegalName: row.LegalName,
		Type: string(row.Type), IsCustomer: row.IsCustomer, IsSupplier: row.IsSupplier,
		TaxNumber: row.TaxNumber, Exempt: row.IsTaxExempt,
		Currency: row.CurrencyCode, Terms: row.PaymentTermsDays,
		CreditLimitMinor: minor(row.CreditLimitMinor),
		Phone:            row.Phone, Email: row.Email,
		Active: row.IsActive, HasHistory: row.HasHistory,
	}
}
