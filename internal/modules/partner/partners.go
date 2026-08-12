package partner

import (
	"context"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/partner/domain"
)

// NewPartnerInput is what creating a partner needs.
type NewPartnerInput struct {
	CompanyID id.ID
	Code      string
	Name      string
	LegalName string
	Type      domain.PartnerType

	// At least one must be true. Both is not merely allowed but expected — it is the whole
	// reason customers and suppliers share a table.
	IsCustomer bool
	IsSupplier bool

	TaxNumber       string
	TaxGroupID      id.ID
	IsTaxExempt     bool
	TaxExemptReason string

	CurrencyCode     string
	PaymentTermsDays int
	CreditLimitMinor int64

	Phone string
	Email string
	Notes string
}

// CreatePartner adds a customer, a supplier, or both.
func (s *Service) CreatePartner(
	ctx context.Context, in NewPartnerInput,
) (domain.Partner, error) {
	var created domain.Partner

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		if _, found, err := s.repos.PartnerByCode(
			txCtx, in.CompanyID, upper(in.Code)); err != nil {
			return err
		} else if found {
			return errs.Conflict(CodeDuplicateCode,
				"a partner with this code already exists").WithParam("code", upper(in.Code))
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		built, err := domain.NewPartner(
			identifier, in.Code, in.Name, in.IsCustomer, in.IsSupplier)
		if err != nil {
			return err
		}
		if built, err = built.SetCreditLimit(in.CreditLimitMinor); err != nil {
			return err
		}

		built.LegalName = strings.TrimSpace(in.LegalName)
		if in.Type != "" {
			built.Type = in.Type
		}
		built.TaxNumber = strings.TrimSpace(in.TaxNumber)
		built.TaxGroupID = in.TaxGroupID
		built.IsTaxExempt = in.IsTaxExempt
		built.TaxExemptReason = strings.TrimSpace(in.TaxExemptReason)
		built.CurrencyCode = strings.ToUpper(strings.TrimSpace(in.CurrencyCode))
		built.PaymentTermsDays = in.PaymentTermsDays
		built.Phone = strings.TrimSpace(in.Phone)
		built.Email = strings.TrimSpace(in.Email)
		built.Notes = in.Notes

		if err = s.repos.InsertPartner(txCtx, in.CompanyID, built); err != nil {
			return err
		}
		created = built

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionPartnerCreated, EntityType: EntityPartner, EntityID: built.ID,
			After: map[string]any{
				"code": built.Code, "name": built.Name,
				"customer": built.IsCustomer, "supplier": built.IsSupplier,
			},
		})
	})
	if err != nil {
		return domain.Partner{}, err
	}
	return created, nil
}

// Partners lists a company's partners.
//
// One method with a role filter rather than Customers() and Suppliers(), because they are two
// views of one table and a partner who is both must appear in each. Two methods would invite an
// implementation where they do not.
func (s *Service) Partners(
	ctx context.Context, companyID id.ID, filter Filter,
) ([]domain.Partner, error) {
	return s.repos.Partners(ctx, companyID, filter)
}

// PartnerByCode finds one partner.
func (s *Service) PartnerByCode(
	ctx context.Context, companyID id.ID, code string,
) (domain.Partner, error) {
	partner, found, err := s.repos.PartnerByCode(ctx, companyID, upper(code))
	if err != nil {
		return domain.Partner{}, err
	}
	if !found {
		return domain.Partner{}, errs.NotFound(CodeUnknownPartner,
			"there is no partner with that code").WithParam("code", upper(code))
	}
	return partner, nil
}

// SetRoles changes which roles a partner holds, or refuses.
//
// The refusal is the point: a supplier who has been billed cannot stop being a supplier, because
// their payable sits in the accounts and clearing the flag would hide them from the supplier
// list while the balance remains. Deactivation is what a business actually wants there.
func (s *Service) SetRoles(
	ctx context.Context, companyID id.ID, code string, isCustomer, isSupplier bool,
) (domain.Partner, error) {
	var updated domain.Partner

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		partner, found, err := s.repos.PartnerByCode(txCtx, companyID, upper(code))
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownPartner,
				"there is no partner with that code").WithParam("code", upper(code))
		}

		usage, err := s.repos.RoleUsage(txCtx, partner.ID)
		if err != nil {
			return err
		}
		changed, err := partner.SetRoles(isCustomer, isSupplier, usage)
		if err != nil {
			return err
		}
		if err = s.repos.UpdatePartner(txCtx, changed); err != nil {
			return err
		}
		updated = changed

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionPartnerUpdated, EntityType: EntityPartner, EntityID: partner.ID,
			Before: map[string]any{
				"customer": partner.IsCustomer, "supplier": partner.IsSupplier,
			},
			After: map[string]any{
				"customer": changed.IsCustomer, "supplier": changed.IsSupplier,
			},
		})
	})
	if err != nil {
		return domain.Partner{}, err
	}
	return updated, nil
}

// SetCreditLimit changes how much a customer may owe. Zero means no limit.
func (s *Service) SetCreditLimit(
	ctx context.Context, companyID id.ID, code string, minor int64,
) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		partner, found, err := s.repos.PartnerByCode(txCtx, companyID, upper(code))
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownPartner,
				"there is no partner with that code").WithParam("code", upper(code))
		}
		if !partner.IsCustomer {
			// A credit limit on a pure supplier is a field nothing will ever read, and silently
			// storing it would let someone believe a control exists that does not.
			return errs.Validation(CodeNotACustomer,
				"a credit limit applies to a customer").WithParam("code", partner.Code)
		}

		changed, err := partner.SetCreditLimit(minor)
		if err != nil {
			return err
		}
		if err = s.repos.UpdatePartner(txCtx, changed); err != nil {
			return err
		}

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionPartnerUpdated, EntityType: EntityPartner, EntityID: partner.ID,
			Before: map[string]any{"credit_limit_minor": partner.CreditLimitMinor},
			After:  map[string]any{"credit_limit_minor": changed.CreditLimitMinor},
		})
	})
}

// Deactivate retires a partner, leaving every document and balance intact.
func (s *Service) Deactivate(ctx context.Context, companyID id.ID, code string) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		partner, found, err := s.repos.PartnerByCode(txCtx, companyID, upper(code))
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownPartner,
				"there is no partner with that code").WithParam("code", upper(code))
		}

		retired := partner.Deactivate()
		if err = s.repos.UpdatePartner(txCtx, retired); err != nil {
			return err
		}

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionPartnerUpdated, EntityType: EntityPartner, EntityID: partner.ID,
			Before: map[string]any{"active": true}, After: map[string]any{"active": false},
		})
	})
}

// DeletePartner removes a partner that has never appeared on a document.
func (s *Service) DeletePartner(ctx context.Context, companyID id.ID, code string) error {
	return s.db.Do(ctx, func(txCtx context.Context) error {
		partner, found, err := s.repos.PartnerByCode(txCtx, companyID, upper(code))
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownPartner,
				"there is no partner with that code").WithParam("code", upper(code))
		}
		// Deleting a supplier who appears on a two-year-old bill breaks that document and every
		// report that groups by them. Deactivation is the answer, and the error says so.
		if err = partner.RequireDeletable(); err != nil {
			return err
		}
		if err = s.repos.DeletePartner(txCtx, partner.ID); err != nil {
			return err
		}

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionPartnerDeleted, EntityType: EntityPartner, EntityID: partner.ID,
			Before: map[string]any{"code": partner.Code, "name": partner.Name},
		})
	})
}

// CheckCredit reports whether a further amount would breach a customer's limit.
//
// Phase 5 calls this before confirming a sale. The rule itself lives in the domain, so that what
// zero means has exactly one home.
func (s *Service) CheckCredit(
	ctx context.Context, companyID id.ID, code string, outstandingMinor, additionalMinor int64,
) error {
	partner, err := s.PartnerByCode(ctx, companyID, code)
	if err != nil {
		return err
	}
	return partner.CheckCredit(outstandingMinor, additionalMinor)
}

// RoleUsage reports which of a partner's roles have been used.
func (s *Service) RoleUsage(ctx context.Context, partnerID id.ID) (domain.RoleUsage, error) {
	return s.repos.RoleUsage(ctx, partnerID)
}

// ── addresses and contacts ──────────────────────────────────────────────────────

// NewAddressInput describes one of a partner's addresses.
type NewAddressInput struct {
	CompanyID   id.ID
	PartnerCode string
	Label       string
	Type        domain.AddressType
	Line1       string
	Line2       string
	City        string
	Region      string
	PostalCode  string
	CountryCode string

	ContactName  string
	ContactPhone string
	IsDefault    bool
}

// AddAddress attaches an address to a partner.
func (s *Service) AddAddress(ctx context.Context, in NewAddressInput) (domain.Address, error) {
	var created domain.Address

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		partner, found, err := s.repos.PartnerByCode(txCtx, in.CompanyID, upper(in.PartnerCode))
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownPartner,
				"there is no partner with that code").WithParam("code", upper(in.PartnerCode))
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		address, err := domain.NewAddress(
			identifier, partner.ID, in.Label, in.Line1, in.Type)
		if err != nil {
			return err
		}
		address.Line2 = strings.TrimSpace(in.Line2)
		address.City = strings.TrimSpace(in.City)
		address.Region = strings.TrimSpace(in.Region)
		address.PostalCode = strings.TrimSpace(in.PostalCode)
		address.CountryCode = strings.ToUpper(strings.TrimSpace(in.CountryCode))
		address.ContactName = strings.TrimSpace(in.ContactName)
		address.ContactPhone = strings.TrimSpace(in.ContactPhone)
		address.IsDefault = in.IsDefault

		if err = s.repos.InsertAddress(txCtx, address); err != nil {
			return err
		}
		created = address

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionAddressAdded, EntityType: EntityPartner, EntityID: partner.ID,
			After: map[string]any{
				"partner": partner.Code, "label": address.Label, "type": string(address.Type),
			},
		})
	})
	if err != nil {
		return domain.Address{}, err
	}
	return created, nil
}

// Addresses lists a partner's addresses, the default first.
func (s *Service) Addresses(ctx context.Context, partnerID id.ID) ([]domain.Address, error) {
	return s.repos.Addresses(ctx, partnerID)
}

// NewContactInput describes a person at a partner.
type NewContactInput struct {
	CompanyID   id.ID
	PartnerCode string
	Name        string
	Role        string
	Phone       string
	Email       string
	IsPrimary   bool
}

// AddContact attaches a person to a partner.
func (s *Service) AddContact(ctx context.Context, in NewContactInput) (domain.Contact, error) {
	var created domain.Contact

	err := s.db.Do(ctx, func(txCtx context.Context) error {
		partner, found, err := s.repos.PartnerByCode(txCtx, in.CompanyID, upper(in.PartnerCode))
		if err != nil {
			return err
		}
		if !found {
			return errs.NotFound(CodeUnknownPartner,
				"there is no partner with that code").WithParam("code", upper(in.PartnerCode))
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		contact, err := domain.NewContact(identifier, partner.ID, in.Name)
		if err != nil {
			return err
		}
		contact.Role = strings.TrimSpace(in.Role)
		contact.Phone = strings.TrimSpace(in.Phone)
		contact.Email = strings.TrimSpace(in.Email)
		contact.IsPrimary = in.IsPrimary

		if err = s.repos.InsertContact(txCtx, contact); err != nil {
			return err
		}
		created = contact

		return s.audit(txCtx, auditc.Auditable{
			Action: ActionContactAdded, EntityType: EntityPartner, EntityID: partner.ID,
			After: map[string]any{"partner": partner.Code, "name": contact.Name},
		})
	})
	if err != nil {
		return domain.Contact{}, err
	}
	return created, nil
}

// Contacts lists a partner's people, the primary first.
func (s *Service) Contacts(ctx context.Context, partnerID id.ID) ([]domain.Contact, error) {
	return s.repos.Contacts(ctx, partnerID)
}

// upper normalises a code the way every code in this module is stored.
func upper(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// PartnerByID finds one partner by identity.
//
// Exists for the composition root's sales port: a posting holds a partner id and needs to check a
// credit limit. Looking it up by CODE would mean the caller carrying a code it does not have.
func (s *Service) PartnerByID(
	ctx context.Context, companyID, partnerID id.ID,
) (domain.Partner, error) {
	found, ok, err := s.repos.PartnerByID(ctx, companyID, partnerID)
	if err != nil {
		return domain.Partner{}, err
	}
	if !ok {
		return domain.Partner{}, errs.NotFound(CodeUnknownPartner,
			"there is no partner with that identity").WithParam("id", string(partnerID))
	}
	return found, nil
}
