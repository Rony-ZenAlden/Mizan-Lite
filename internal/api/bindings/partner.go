package bindings

import (
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
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
		"Customers":      policy.Requires(partner.PermCustomerView),
		"Suppliers":      policy.Requires(partner.PermSupplierView),
		"Customer":       policy.Requires(partner.PermCustomerView),
		"Supplier":       policy.Requires(partner.PermSupplierView),
		"Statement":      policy.Requires(partner.PermBalanceView),
		"VerifyBalances": policy.Requires(partner.PermBalanceView),

		// The write half (10.13), declared beside the reads so one map holds the façade.
		"CreateCustomer": policy.Requires(partner.PermCustomerManage),
		"CreateSupplier": policy.Requires(partner.PermSupplierManage),
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

// ── balances and statements (7.4) ───────────────────────────────────────────────

// PartnerBalanceDTO is what a partner owes and is owed.
type PartnerBalanceDTO struct {
	PartnerID   string `json:"partnerId"`
	PartnerName string `json:"partnerName"`

	ReceivableMinor string `json:"receivableMinor"`
	PayableMinor    string `json:"payableMinor"`
	// NetMinor is SIGNED: positive means they owe the business on balance.
	//
	// Both halves are sent alongside it, because netting them away hides the case that matters
	// most — a partner who is both customer and supplier, owing 5,000 and owed 4,900, is not the
	// same risk as one who simply owes 100.
	NetMinor string `json:"netMinor"`
}

// OpenItemDTO is one document a partner still owes something on.
type OpenItemDTO struct {
	DocumentType   string `json:"documentType"`
	DocumentID     string `json:"documentId"`
	DocumentNumber string `json:"documentNumber"`
	Date           string `json:"date"`
	DueDate        string `json:"dueDate"`

	TotalMinor       string `json:"totalMinor"`
	SettledMinor     string `json:"settledMinor"`
	OutstandingMinor string `json:"outstandingMinor"`
}

// AgeBandsDTO groups what is outstanding by how overdue it is.
type AgeBandsDTO struct {
	CurrentMinor string `json:"currentMinor"`
	Days30Minor  string `json:"days30Minor"`
	Days60Minor  string `json:"days60Minor"`
	Days90Minor  string `json:"days90Minor"`
	OlderMinor   string `json:"olderMinor"`
}

// PartnerStatementDTO is a partner's open items and their ageing.
type PartnerStatementDTO struct {
	Balance PartnerBalanceDTO `json:"balance"`
	// Receivable and Payable are kept apart rather than merged into one signed list: a statement
	// sent to a customer shows what they owe, one sent to a supplier shows what is owed to them,
	// and a merged list is a document nobody can send to either.
	Receivable []OpenItemDTO `json:"receivable"`
	Payable    []OpenItemDTO `json:"payable"`

	ReceivableAgeing AgeBandsDTO `json:"receivableAgeing"`
}

// BalanceDiscrepancyDTO is one partner whose documents disagree with the ledger.
type BalanceDiscrepancyDTO struct {
	PartnerID   string `json:"partnerId"`
	PartnerName string `json:"partnerName"`
	Side        string `json:"side"`

	DocumentsMinor  string `json:"documentsMinor"`
	LedgerMinor     string `json:"ledgerMinor"`
	DifferenceMinor string `json:"differenceMinor"`
}

// Statement reads one partner's open items, ageing, and balance.
func (p *Partners) Statement(partnerID, asAt string) envelope.Result[PartnerStatementDTO] {
	ctx, app, err := p.guard("Statement")
	if err != nil {
		return envelope.Fail[PartnerStatementDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[PartnerStatementDTO](err)
	}

	statement, err := app.Partner.StatementFor(ctx, companyID, id.ID(partnerID))
	if err != nil {
		return envelope.Fail[PartnerStatementDTO](err)
	}

	// The ageing date comes from the CALLER, so a statement printed for a month end says what it
	// said at that month end. Defaulting to today only when nothing was asked for.
	if asAt == "" {
		asAt = clock.FormatDate(clock.System().Now())
	}

	out := PartnerStatementDTO{
		Balance: PartnerBalanceDTO{
			PartnerID:       string(statement.Balance.PartnerID),
			PartnerName:     statement.Balance.PartnerName,
			ReceivableMinor: minor(statement.Balance.ReceivableMinor),
			PayableMinor:    minor(statement.Balance.PayableMinor),
			NetMinor:        minor(statement.Balance.NetMinor),
		},
		Receivable: openItemRows(statement.Receivable),
		Payable:    openItemRows(statement.Payable),
	}

	bands := partner.Age(statement.Receivable, asAt)
	out.ReceivableAgeing = AgeBandsDTO{
		CurrentMinor: minor(bands.CurrentMinor), Days30Minor: minor(bands.Days30Minor),
		Days60Minor: minor(bands.Days60Minor), Days90Minor: minor(bands.Days90Minor),
		OlderMinor: minor(bands.OlderMinor),
	}
	return envelope.Ok(out)
}

// VerifyBalances checks every partner's documents against the general ledger (§7.13 invariant 5).
func (p *Partners) VerifyBalances() envelope.Result[[]BalanceDiscrepancyDTO] {
	ctx, app, err := p.guard("VerifyBalances")
	if err != nil {
		return envelope.Fail[[]BalanceDiscrepancyDTO](err)
	}
	companyID, err := app.Org.CurrentCompanyID(ctx)
	if err != nil {
		return envelope.Fail[[]BalanceDiscrepancyDTO](err)
	}

	discrepancies, err := app.Partner.VerifyBalances(ctx, companyID)
	if err != nil {
		return envelope.Fail[[]BalanceDiscrepancyDTO](err)
	}

	out := make([]BalanceDiscrepancyDTO, 0, len(discrepancies))
	for _, discrepancy := range discrepancies {
		out = append(out, BalanceDiscrepancyDTO{
			PartnerID: string(discrepancy.PartnerID), PartnerName: discrepancy.PartnerName,
			Side:            discrepancy.Side,
			DocumentsMinor:  minor(discrepancy.DocumentsMinor),
			LedgerMinor:     minor(discrepancy.LedgerMinor),
			DifferenceMinor: minor(discrepancy.DifferenceMinor),
		})
	}
	return envelope.Ok(out)
}

func openItemRows(items []partner.OpenItem) []OpenItemDTO {
	out := make([]OpenItemDTO, 0, len(items))
	for _, item := range items {
		out = append(out, OpenItemDTO{
			DocumentType: item.DocumentType, DocumentID: string(item.DocumentID),
			DocumentNumber: item.DocumentNumber, Date: item.Date, DueDate: item.DueDate,
			TotalMinor: minor(item.TotalMinor), SettledMinor: minor(item.SettledMinor),
			OutstandingMinor: minor(item.OutstandingMinor),
		})
	}
	return out
}
