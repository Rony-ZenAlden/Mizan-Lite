package bindings

import (
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/modules/partner"
	partnerdomain "github.com/mizan-erp/mizan/internal/modules/partner/domain"
)

// NewPartnerInput is what the "new customer" and "new supplier" forms send.
//
// # Two fields required, and a phone that matters more than it looks
//
// The service accepts sixteen. A code and a name are what a shop actually knows when it writes
// somebody down.
//
// The phone is offered on the first screen rather than behind "more", because it is how a shop
// finds a returning customer — faster and more reliably than by name, in a country where four
// customers share one. That is a decision about the ROOM, not about the schema.
type NewPartnerInput struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// Customer and Supplier: a partner can be both, and 7.4's balance work keeps the two sides
	// apart precisely because that is ordinary.
	Customer bool   `json:"isCustomer"`
	Supplier bool   `json:"isSupplier"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	// TaxNumber is free text: formats differ by country and a validation rule welded in becomes
	// a reason a customer cannot be entered at all.
	TaxNumber string `json:"taxNumber"`
	// PaymentTermsDays decides when their invoices become overdue, which the ageing report is
	// built on. Zero means immediate.
	PaymentTermsDays int `json:"paymentTermsDays"`
	// CreditLimitMinor is in MINOR units, like every amount crossing this boundary.
	CreditLimitMinor string `json:"creditLimitMinor"`
}

// CreateCustomer registers somebody the business sells to.
func (p *Partners) CreateCustomer(in NewPartnerInput) envelope.Result[PartnerRowDTO] {
	in.Customer = true
	return p.create("CreateCustomer", in)
}

// CreateSupplier registers somebody the business buys from.
func (p *Partners) CreateSupplier(in NewPartnerInput) envelope.Result[PartnerRowDTO] {
	in.Supplier = true
	return p.create("CreateSupplier", in)
}

// create is the shared body.
//
// # Why the façade had no create method until now
//
// `partner.CreatePartner` has existed since Phase 3 and the CSV importer has used it since 9.4.
// The Partners façade was read-only — `Customers`, `Suppliers`, `Customer`, `Supplier`,
// `Statement`, `VerifyBalances` — so a shop could import a customer list and could not add the
// customer standing at the counter.
//
// This is the gap `TestEveryWriteBindingHasAFrontEndCaller` cannot catch: that test finds
// bindings with no caller, and a binding that was never written has nothing to find.
func (p *Partners) create(method string, in NewPartnerInput) envelope.Result[PartnerRowDTO] {
	ctx, app, err := p.guard(method)
	if err != nil {
		return envelope.Fail[PartnerRowDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[PartnerRowDTO](err)
	}

	limit, err := parseScaled(in.CreditLimitMinor)
	if err != nil {
		return envelope.Fail[PartnerRowDTO](err)
	}

	created, err := app.Partner.CreatePartner(ctx, partner.NewPartnerInput{
		CompanyID: companyID,
		Code:      in.Code,
		Name:      in.Name,
		// A company unless somebody says otherwise: a shop's customers are mostly businesses in
		// the wholesale case and mostly people in the retail one, and neither default is wrong
		// enough to justify asking on the first screen.
		Type:             partnerdomain.TypeCompany,
		IsCustomer:       in.Customer,
		IsSupplier:       in.Supplier,
		Phone:            in.Phone,
		Email:            in.Email,
		TaxNumber:        in.TaxNumber,
		PaymentTermsDays: in.PaymentTermsDays,
		CreditLimitMinor: limit,
	})
	if err != nil {
		return envelope.Fail[PartnerRowDTO](err)
	}

	return envelope.Ok(PartnerRowDTO{
		ID: string(created.ID), Code: created.Code, Name: created.Name,
		IsCustomer: created.IsCustomer, IsSupplier: created.IsSupplier,
		Phone: created.Phone, Email: created.Email, Active: created.IsActive,
	})
}
