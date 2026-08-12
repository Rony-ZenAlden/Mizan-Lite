// Package sqlite is the partner module's persistence.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/partner/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeStorage is the stable code for a persistence failure.
const CodeStorage = "partner.storage"

// Repos is the module's repository set.
type Repos struct {
	db  database.DB
	clk clock.Clock
}

// New builds the repositories.
func New(db database.DB, clk clock.Clock) *Repos {
	if clk == nil {
		clk = clock.System()
	}
	return &Repos{db: db, clk: clk}
}

func (r *Repos) now() string { return clock.Format(r.clk.Now()) }

func (r *Repos) wrap(err error, what string) error {
	return errs.Wrap(r.db.Dialect().TranslateError(err), errs.CategoryInternal, CodeStorage, what)
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableID(v id.ID) any {
	if v.IsZero() {
		return nil
	}
	return string(v)
}

func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type scanner interface{ Scan(dest ...any) error }

// Filter narrows a partner listing.
//
// Roles are a filter rather than two separate list methods, because "customers" and "suppliers"
// are two views of one table and a partner who is both must appear in each.
type Filter struct {
	CustomersOnly bool
	SuppliersOnly bool
	ActiveOnly    bool
	Search        string
}

// InsertPartner writes a partner.
func (r *Repos) InsertPartner(ctx context.Context, companyID id.ID, p domain.Partner) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO partners (
			id, company_id, code, name, legal_name, is_customer, is_supplier, partner_type,
			tax_number, tax_group_id, is_tax_exempt, tax_exempt_reason,
			currency_code, payment_terms_days, credit_limit_minor,
			receivable_account_id, payable_account_id,
			phone, email, website, notes, is_active, has_history,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(p.ID), string(companyID), p.Code, p.Name, nullable(p.LegalName),
		boolToInt(p.IsCustomer), boolToInt(p.IsSupplier), string(p.Type),
		nullable(p.TaxNumber), nullableID(p.TaxGroupID),
		boolToInt(p.IsTaxExempt), nullable(p.TaxExemptReason),
		nullable(p.CurrencyCode), p.PaymentTermsDays, p.CreditLimitMinor,
		nullableID(p.ReceivableAccountID), nullableID(p.PayableAccountID),
		nullable(p.Phone), nullable(p.Email), nullable(p.Website), nullable(p.Notes),
		boolToInt(p.IsActive), boolToInt(p.HasHistory), now, now)
	if err != nil {
		return r.wrap(err, "inserting a partner")
	}
	return nil
}

// UpdatePartner writes a partner's mutable fields.
func (r *Repos) UpdatePartner(ctx context.Context, p domain.Partner) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE partners SET
			name = ?, legal_name = ?, is_customer = ?, is_supplier = ?, partner_type = ?,
			tax_number = ?, tax_group_id = ?, is_tax_exempt = ?, tax_exempt_reason = ?,
			currency_code = ?, payment_terms_days = ?, credit_limit_minor = ?,
			receivable_account_id = ?, payable_account_id = ?,
			phone = ?, email = ?, website = ?, notes = ?, is_active = ?,
			row_version = row_version + 1, updated_at = ?
		WHERE id = ?`,
		p.Name, nullable(p.LegalName), boolToInt(p.IsCustomer), boolToInt(p.IsSupplier),
		string(p.Type), nullable(p.TaxNumber), nullableID(p.TaxGroupID),
		boolToInt(p.IsTaxExempt), nullable(p.TaxExemptReason),
		nullable(p.CurrencyCode), p.PaymentTermsDays, p.CreditLimitMinor,
		nullableID(p.ReceivableAccountID), nullableID(p.PayableAccountID),
		nullable(p.Phone), nullable(p.Email), nullable(p.Website), nullable(p.Notes),
		boolToInt(p.IsActive), r.now(), string(p.ID))
	if err != nil {
		return r.wrap(err, "updating a partner")
	}
	return nil
}

const partnerColumns = `
	id, code, name, legal_name, is_customer, is_supplier, partner_type,
	tax_number, tax_group_id, is_tax_exempt, tax_exempt_reason,
	currency_code, payment_terms_days, credit_limit_minor,
	receivable_account_id, payable_account_id,
	phone, email, website, notes, is_active, has_history`

// Partners lists a company's partners.
func (r *Repos) Partners(
	ctx context.Context, companyID id.ID, filter Filter,
) ([]domain.Partner, error) {
	query := `SELECT ` + partnerColumns + ` FROM partners WHERE company_id = ?`
	args := []any{string(companyID)}

	// A partner who is both appears under each role, which is the whole point of one table.
	if filter.CustomersOnly {
		query += ` AND is_customer = 1`
	}
	if filter.SuppliersOnly {
		query += ` AND is_supplier = 1`
	}
	if filter.ActiveOnly {
		query += ` AND is_active = 1`
	}
	if filter.Search != "" {
		query += ` AND (name LIKE ? OR code LIKE ? OR COALESCE(tax_number, '') LIKE ?)`
		like := "%" + filter.Search + "%"
		args = append(args, like, like, like)
	}
	query += ` ORDER BY name`

	rows, err := r.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, r.wrap(err, "listing partners")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Partner, 0, 32)
	for rows.Next() {
		partner, scanErr := scanPartner(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "reading a partner")
		}
		out = append(out, partner)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing partners")
	}
	return out, nil
}

// PartnerByCode finds one partner.
func (r *Repos) PartnerByCode(
	ctx context.Context, companyID id.ID, code string,
) (domain.Partner, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+partnerColumns+` FROM partners WHERE company_id = ? AND code = ?`,
		string(companyID), code)

	partner, err := scanPartner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Partner{}, false, nil
	}
	if err != nil {
		return domain.Partner{}, false, r.wrap(err, "reading a partner")
	}
	return partner, true, nil
}

func scanPartner(s scanner) (domain.Partner, error) {
	var (
		p                              domain.Partner
		legalName, taxNumber, taxGroup any
		exemptReason, currency         any
		receivable, payable            any
		phone, email, website, notes   any
		partnerType                    string
		customer, supplier, exempt     int
		active, history                int
	)
	if err := s.Scan(&p.ID, &p.Code, &p.Name, &legalName, &customer, &supplier, &partnerType,
		&taxNumber, &taxGroup, &exempt, &exemptReason,
		&currency, &p.PaymentTermsDays, &p.CreditLimitMinor,
		&receivable, &payable,
		&phone, &email, &website, &notes, &active, &history); err != nil {
		return domain.Partner{}, err
	}
	p.LegalName = text(legalName)
	p.Type = domain.PartnerType(partnerType)
	p.IsCustomer = customer == 1
	p.IsSupplier = supplier == 1
	p.TaxNumber = text(taxNumber)
	p.TaxGroupID = id.ID(text(taxGroup))
	p.IsTaxExempt = exempt == 1
	p.TaxExemptReason = text(exemptReason)
	p.CurrencyCode = text(currency)
	p.ReceivableAccountID = id.ID(text(receivable))
	p.PayableAccountID = id.ID(text(payable))
	p.Phone = text(phone)
	p.Email = text(email)
	p.Website = text(website)
	p.Notes = text(notes)
	p.IsActive = active == 1
	p.HasHistory = history == 1
	return domain.Adopt(p), nil
}

// MarkHistory records that a partner now appears on a document.
//
// Phase 5 and Phase 6 call this. It exists now because it is what RequireDeletable and SetRoles
// READ, and a rule whose trigger does not exist yet is a rule no test can exercise.
func (r *Repos) MarkHistory(ctx context.Context, partnerID id.ID, sold, purchased bool) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE partners SET has_history = 1, row_version = row_version + 1, updated_at = ?
		WHERE id = ?`, r.now(), string(partnerID))
	if err != nil {
		return r.wrap(err, "marking a partner as having history")
	}
	if err = r.recordUsage(ctx, partnerID, sold, purchased); err != nil {
		return err
	}
	return nil
}

// recordUsage stores which ROLES have been used, not merely that the partner has history.
//
// Two separate facts, because the rules differ: `has_history` refuses deletion, while per-role
// usage refuses removing that one role. A supplier who has been billed cannot stop being a
// supplier, but may perfectly well stop being a customer if nothing was ever sold to them.
func (r *Repos) recordUsage(ctx context.Context, partnerID id.ID, sold, purchased bool) error {
	if !sold && !purchased {
		return nil
	}
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO partner_role_usage (partner_id, sold_to, purchased_from, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (partner_id) DO UPDATE SET
			sold_to = sold_to | excluded.sold_to,
			purchased_from = purchased_from | excluded.purchased_from,
			updated_at = excluded.updated_at`,
		string(partnerID), boolToInt(sold), boolToInt(purchased), now)
	if err != nil {
		return r.wrap(err, "recording a partner's role usage")
	}
	return nil
}

// RoleUsage reports which of a partner's roles have actually been used.
func (r *Repos) RoleUsage(ctx context.Context, partnerID id.ID) (domain.RoleUsage, error) {
	var sold, purchased int
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT sold_to, purchased_from FROM partner_role_usage WHERE partner_id = ?`,
		string(partnerID)).Scan(&sold, &purchased)
	if errors.Is(err, sql.ErrNoRows) {
		// Never traded with. Not an error — a brand-new partner is the ordinary case.
		return domain.RoleUsage{}, nil
	}
	if err != nil {
		return domain.RoleUsage{}, r.wrap(err, "reading a partner's role usage")
	}
	return domain.RoleUsage{SoldTo: sold == 1, PurchasedFrom: purchased == 1}, nil
}

// DeletePartner removes a partner.
//
// The refusal that protects history lives in the domain; this is only reached for a partner the
// service already checked. A second guard here would be a second rule to keep in step, and the
// one in SQL could not say why it refused.
func (r *Repos) DeletePartner(ctx context.Context, partnerID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx,
		`DELETE FROM partners WHERE id = ?`, string(partnerID))
	if err != nil {
		return r.wrap(err, "deleting a partner")
	}
	return nil
}

// ── addresses ───────────────────────────────────────────────────────────────────

// InsertAddress writes one of a partner's addresses.
func (r *Repos) InsertAddress(ctx context.Context, a domain.Address) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO partner_addresses (
			id, partner_id, label, address_type, line1, line2, city, region,
			postal_code, country_code, contact_name, contact_phone,
			is_default, is_active, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(a.ID), string(a.PartnerID), a.Label, string(a.Type), a.Line1,
		nullable(a.Line2), nullable(a.City), nullable(a.Region),
		nullable(a.PostalCode), nullable(a.CountryCode),
		nullable(a.ContactName), nullable(a.ContactPhone),
		boolToInt(a.IsDefault), boolToInt(a.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting a partner address")
	}
	return nil
}

// Addresses lists a partner's addresses, the default first.
func (r *Repos) Addresses(ctx context.Context, partnerID id.ID) ([]domain.Address, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, partner_id, label, address_type, line1, line2, city, region,
		       postal_code, country_code, contact_name, contact_phone, is_default, is_active
		FROM partner_addresses WHERE partner_id = ?
		ORDER BY is_default DESC, label`, string(partnerID))
	if err != nil {
		return nil, r.wrap(err, "listing partner addresses")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Address, 0, 4)
	for rows.Next() {
		var (
			a                               domain.Address
			line2, city, region, postal, cc any
			contactName, contactPhone       any
			addressType                     string
			isDefault, active               int
		)
		if err = rows.Scan(&a.ID, &a.PartnerID, &a.Label, &addressType, &a.Line1,
			&line2, &city, &region, &postal, &cc, &contactName, &contactPhone,
			&isDefault, &active); err != nil {
			return nil, r.wrap(err, "reading a partner address")
		}
		a.Type = domain.AddressType(addressType)
		a.Line2 = text(line2)
		a.City = text(city)
		a.Region = text(region)
		a.PostalCode = text(postal)
		a.CountryCode = text(cc)
		a.ContactName = text(contactName)
		a.ContactPhone = text(contactPhone)
		a.IsDefault = isDefault == 1
		a.IsActive = active == 1
		out = append(out, a)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing partner addresses")
	}
	return out, nil
}

// ── contacts ────────────────────────────────────────────────────────────────────

// InsertContact writes a person at a partner.
func (r *Repos) InsertContact(ctx context.Context, c domain.Contact) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO partner_contacts (
			id, partner_id, name, role, phone, email, is_primary, is_active,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(c.ID), string(c.PartnerID), c.Name, nullable(c.Role),
		nullable(c.Phone), nullable(c.Email),
		boolToInt(c.IsPrimary), boolToInt(c.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting a partner contact")
	}
	return nil
}

// Contacts lists a partner's people, the primary first.
func (r *Repos) Contacts(ctx context.Context, partnerID id.ID) ([]domain.Contact, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, partner_id, name, role, phone, email, is_primary, is_active
		FROM partner_contacts WHERE partner_id = ? ORDER BY is_primary DESC, name`,
		string(partnerID))
	if err != nil {
		return nil, r.wrap(err, "listing partner contacts")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Contact, 0, 4)
	for rows.Next() {
		var (
			c                  domain.Contact
			role, phone, email any
			primary, active    int
		)
		if err = rows.Scan(&c.ID, &c.PartnerID, &c.Name, &role, &phone, &email,
			&primary, &active); err != nil {
			return nil, r.wrap(err, "reading a partner contact")
		}
		c.Role = text(role)
		c.Phone = text(phone)
		c.Email = text(email)
		c.IsPrimary = primary == 1
		c.IsActive = active == 1
		out = append(out, c)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing partner contacts")
	}
	return out, nil
}

// PartnerByID finds one partner by identity.
func (r *Repos) PartnerByID(
	ctx context.Context, companyID, partnerID id.ID,
) (domain.Partner, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+partnerColumns+` FROM partners WHERE company_id = ? AND id = ?`,
		string(companyID), string(partnerID))

	partner, err := scanPartner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Partner{}, false, nil
	}
	if err != nil {
		return domain.Partner{}, false, r.wrap(err, "reading a partner")
	}
	return partner, true, nil
}
