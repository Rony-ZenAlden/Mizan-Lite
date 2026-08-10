// Package sqlite is the tax module's persistence.
//
// Note what is absent: any UPDATE of a rate. §19.2 — "editing a rate in place would silently
// falsify past documents" — is enforced by there being no method that could, the same way the
// audit trail (1.6) and the posted ledger (2.2) are append-only. A rate change inserts a
// version and closes the previous one's window.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/tax/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeStorage is the stable code for a persistence failure.
const CodeStorage = "tax.storage"

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

func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ── groups ──────────────────────────────────────────────────────────────────────

// GroupByID loads a group with its taxes, in application order.
func (r *Repos) GroupByID(ctx context.Context, groupID id.ID) (domain.Group, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT id, code, name, is_price_inclusive FROM tax_groups WHERE id = ? AND is_active = 1`,
		string(groupID))

	var (
		group     domain.Group
		inclusive int
	)
	if err := row.Scan(&group.ID, &group.Code, &group.Name, &inclusive); err != nil {
		return domain.Group{}, err // sql.ErrNoRows passes through
	}
	group.PriceInclusive = inclusive == 1

	items, err := r.groupItems(ctx, groupID)
	if err != nil {
		return domain.Group{}, err
	}
	group.Items = items
	return group, nil
}

// DefaultGroup finds the company's default group, if one is set.
func (r *Repos) DefaultGroup(ctx context.Context, companyID id.ID) (id.ID, bool, error) {
	var groupID id.ID
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT id FROM tax_groups
		 WHERE company_id = ? AND is_default = 1 AND is_active = 1 LIMIT 1`,
		string(companyID)).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		// No default is the SHIPPED state: v1 seeds no tax at all, so "no group" is normal
		// rather than exceptional.
		return id.ID(""), false, nil
	}
	if err != nil {
		// Any other failure is reported. A first draft swallowed everything here, which meant a
		// database fault would read as "this company charges no tax" — the same answer as the
		// shipped configuration, and therefore invisible. golangci-lint's nilerr caught it.
		return id.ID(""), false, r.wrap(err, "reading the default tax group")
	}
	return groupID, true, nil
}

func (r *Repos) groupItems(ctx context.Context, groupID id.ID) ([]domain.GroupItem, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT t.id, t.code, t.name, t.tax_type, t.calculation,
		       t.is_compound, t.is_recoverable, t.is_active,
		       i.sequence, i.is_compound_on_previous
		  FROM tax_group_items i
		  JOIN taxes t ON t.id = i.tax_id
		 WHERE i.tax_group_id = ? AND t.is_active = 1
		 ORDER BY i.sequence`, string(groupID))
	if err != nil {
		return nil, r.wrap(err, "reading a tax group")
	}
	defer func() { _ = rows.Close() }()

	var out []domain.GroupItem
	for rows.Next() {
		var (
			item                          domain.GroupItem
			kind, calculation             string
			compound, recoverable, active int
			compoundOn                    int
		)
		if scanErr := rows.Scan(
			&item.Tax.ID, &item.Tax.Code, &item.Tax.Name, &kind, &calculation,
			&compound, &recoverable, &active, &item.Sequence, &compoundOn,
		); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a tax")
		}
		item.Tax.Kind = domain.Kind(kind)
		item.Tax.Calculation = domain.Calculation(calculation)
		item.Tax.IsCompound = compound == 1
		item.Tax.IsRecoverable = recoverable == 1
		item.Tax.IsActive = active == 1
		item.CompoundOn = compoundOn == 1
		out = append(out, item)
	}
	return out, rows.Err()
}

// ── rates ───────────────────────────────────────────────────────────────────────

// RatesOn loads the version in force on a date for every tax in a group.
//
// One query for the whole group rather than one per tax: a document line resolves its taxes
// together, and a per-tax round trip would put a query count on the till's critical path.
func (r *Repos) RatesOn(
	ctx context.Context, groupID id.ID, on time.Time,
) (map[id.ID]domain.Version, error) {
	day := clock.FormatDate(on)
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT v.tax_id, v.rate_micro, v.fixed_minor, v.effective_from, v.effective_to
		  FROM tax_versions v
		  JOIN tax_group_items i ON i.tax_id = v.tax_id
		 WHERE i.tax_group_id = ?
		   AND v.effective_from <= ?
		   AND (v.effective_to IS NULL OR v.effective_to >= ?)`,
		string(groupID), day, day)
	if err != nil {
		return nil, r.wrap(err, "reading tax rates")
	}
	defer func() { _ = rows.Close() }()

	out := map[id.ID]domain.Version{}
	for rows.Next() {
		var (
			version domain.Version
			from    string
			to      any
		)
		if scanErr := rows.Scan(&version.TaxID, &version.RateMicro, &version.FixedMinor,
			&from, &to); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a tax rate")
		}
		version.From, _ = clock.ParseDate(from)
		if closed := text(to); closed != "" {
			version.To, _ = clock.ParseDate(closed)
		}
		out[version.TaxID] = version
	}
	return out, rows.Err()
}

// ── exemptions ──────────────────────────────────────────────────────────────────

// IsExempt reports whether a partner is exempt on a date, and why.
//
// The reason is returned because §19.3 stores it on the document: "we did not charge this
// customer tax" is a claim an authority asks about years later, and the answer must be a
// certificate rather than somebody's memory.
func (r *Repos) IsExempt(
	ctx context.Context, companyID, partnerID id.ID, on time.Time,
) (string, bool, error) {
	if partnerID.IsZero() {
		return "", false, nil
	}
	day := clock.FormatDate(on)
	var reason string
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT reason_code FROM tax_exemptions
		 WHERE company_id = ? AND partner_id = ?
		   AND valid_from <= ? AND (valid_to IS NULL OR valid_to >= ?)
		 LIMIT 1`, string(companyID), string(partnerID), day, day).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil // not exempt is the normal case
	}
	if err != nil {
		// Reported, not swallowed. Reading a fault as "not exempt" would CHARGE an exempt
		// customer tax — the expensive direction of the two, and one nobody would notice until
		// the customer did.
		return "", false, r.wrap(err, "reading tax exemptions")
	}
	return reason, true, nil
}

// ── writes ──────────────────────────────────────────────────────────────────────

// InsertJurisdiction writes a jurisdiction.
func (r *Repos) InsertJurisdiction(
	ctx context.Context, identifier, companyID id.ID, country, region, name string,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO tax_jurisdictions (
			id, company_id, country_code, region_code, name, is_active,
			created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, ?, 1, ?, ?, 1)`,
		string(identifier), string(companyID), country, nullable(region), name, now, now)
	if err != nil {
		return r.wrap(err, "inserting a tax jurisdiction")
	}
	return nil
}

// InsertTax writes a tax.
func (r *Repos) InsertTax(ctx context.Context, companyID id.ID, tax domain.Tax) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO taxes (
			id, company_id, jurisdiction_id, code, name, name_key, tax_type, calculation,
			is_compound, is_recoverable, is_active, created_at, updated_at, row_version
		) VALUES (?, ?, NULL, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, 1)`,
		string(tax.ID), string(companyID), tax.Code, tax.Name,
		string(tax.Kind), string(tax.Calculation),
		boolToInt(tax.IsCompound), boolToInt(tax.IsRecoverable), boolToInt(tax.IsActive),
		now, now)
	if err != nil {
		return r.wrap(err, "inserting a tax")
	}
	return nil
}

// InsertVersion adds a rate version.
//
// The ONLY way a rate changes. There is deliberately no update: a new version with an effective
// date leaves every document already issued resolving what it was charged at (§19.2).
func (r *Repos) InsertVersion(ctx context.Context, identifier id.ID, v domain.Version) error {
	var to any
	if !v.To.IsZero() {
		to = clock.FormatDate(v.To)
	}
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO tax_versions (
			id, tax_id, rate_micro, fixed_minor, effective_from, effective_to, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(identifier), string(v.TaxID), v.RateMicro, v.FixedMinor,
		clock.FormatDate(v.From), to, r.now())
	if err != nil {
		return r.wrap(err, "inserting a tax rate version")
	}
	return nil
}

// CloseOpenVersions ends every still-open window for a tax, so a later version takes over.
//
// The ONLY statement in this module that touches an existing rate row, and it changes WHEN a
// rate applied — never WHAT it was. `effective_to` is the difference between "this rate is
// history" and "this rate was always something else", and only the first is honest.
func (r *Repos) CloseOpenVersions(ctx context.Context, taxID id.ID, until time.Time) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE tax_versions SET effective_to = ?
		 WHERE tax_id = ? AND effective_to IS NULL AND effective_from <= ?`,
		clock.FormatDate(until), string(taxID), clock.FormatDate(until))
	if err != nil {
		return r.wrap(err, "closing a tax rate version")
	}
	return nil
}

// InsertGroup writes a tax group.
func (r *Repos) InsertGroup(
	ctx context.Context, companyID id.ID, group domain.Group, isDefault bool,
) error {
	now := r.now()
	if _, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO tax_groups (
			id, company_id, code, name, name_key, is_price_inclusive, is_default, is_active,
			created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, NULL, ?, ?, 1, ?, ?, 1)`,
		string(group.ID), string(companyID), group.Code, group.Name,
		boolToInt(group.PriceInclusive), boolToInt(isDefault), now, now); err != nil {
		return r.wrap(err, "inserting a tax group")
	}

	for _, item := range group.Items {
		itemID, err := id.New()
		if err != nil {
			return err
		}
		if _, err = r.db.Writer(ctx).ExecContext(ctx, `
			INSERT INTO tax_group_items (
				id, tax_group_id, tax_id, sequence, is_compound_on_previous, created_at
			) VALUES (?, ?, ?, ?, ?, ?)`,
			string(itemID), string(group.ID), string(item.Tax.ID),
			item.Sequence, boolToInt(item.CompoundOn), now); err != nil {
			return r.wrap(err, "adding a tax to a group")
		}
	}
	return nil
}

// InsertExemption records why a partner is not charged.
func (r *Repos) InsertExemption(
	ctx context.Context, identifier, companyID, partnerID id.ID,
	reason, certificate string, from time.Time,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO tax_exemptions (
			id, company_id, partner_id, tax_id, reason_code, certificate_number,
			valid_from, valid_to, evidence_ref, created_at, updated_at, row_version
		) VALUES (?, ?, ?, NULL, ?, ?, ?, NULL, NULL, ?, ?, 1)`,
		string(identifier), string(companyID), string(partnerID),
		reason, nullable(certificate), clock.FormatDate(from), now, now)
	if err != nil {
		return r.wrap(err, "inserting a tax exemption")
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
