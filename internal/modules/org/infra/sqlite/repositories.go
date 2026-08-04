// Package sqlite implements the org module's repositories.
//
// The only place in the module that knows SQL — the domain and the service stay portable, and
// the `no-sql` rule added in Step 1.1 now enforces that rather than trusting it.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/org/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Repos bundles the org module's repositories over one database.
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
	return errs.Wrap(r.db.Dialect().TranslateError(err), errs.CategoryInternal,
		domain.CodeCompanyNotFound, what)
}

// ── companies ───────────────────────────────────────────────────────────────────

// CountCompanies reports how many companies exist.
//
// The provisioning invariant reads this rather than "find the company and see if it errors":
// a count expresses "how many are there" directly, and a NotFound would have to be
// distinguished from a genuine read failure.
func (r *Repos) CountCompanies(ctx context.Context) (int, error) {
	var n int
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT COUNT(*) FROM companies`).Scan(&n)
	if err != nil {
		return 0, r.wrap(err, "counting companies")
	}
	return n, nil
}

// InsertCompany writes a company.
func (r *Repos) InsertCompany(ctx context.Context, c domain.Company) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO companies (
			id, code, name, legal_name, tax_number, country_code,
			functional_currency, pricing_currency, logo_ref, is_active,
			created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		string(c.ID), c.Code, c.Name, nullable(c.LegalName), nullable(c.TaxNumber),
		c.CountryCode, c.FunctionalCurrency, nullable(c.PricingCurrency),
		nullable(c.LogoRef), boolToInt(c.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting the company")
	}
	return nil
}

// Company returns the single company.
func (r *Repos) Company(ctx context.Context) (domain.Company, error) {
	var (
		c                         domain.Company
		legal, tax, pricing, logo sql.NullString
		active                    int
	)
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT id, code, name, legal_name, tax_number, country_code,
		       functional_currency, pricing_currency, logo_ref, is_active
		  FROM companies
		 ORDER BY created_at
		 LIMIT 1`).
		Scan(&c.ID, &c.Code, &c.Name, &legal, &tax, &c.CountryCode,
			&c.FunctionalCurrency, &pricing, &logo, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Company{}, errs.NotFound(domain.CodeNotProvisioned,
			"no company exists yet; the setup wizard has not been completed")
	}
	if err != nil {
		return domain.Company{}, r.wrap(err, "reading the company")
	}
	c.LegalName, c.TaxNumber = legal.String, tax.String
	c.PricingCurrency, c.LogoRef = pricing.String, logo.String
	c.IsActive = active == 1
	return c, nil
}

// ── branches ────────────────────────────────────────────────────────────────────

// InsertBranch writes a branch.
func (r *Repos) InsertBranch(ctx context.Context, b domain.Branch) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO branches (
			id, company_id, code, name, is_default, address_json, is_active,
			created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		string(b.ID), string(b.CompanyID), b.Code, b.Name,
		boolToInt(b.IsDefault), nullable(b.Address), boolToInt(b.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting the branch")
	}
	return nil
}

// Branches lists a company's branches, defaults first then by code, so a caller that takes
// the first row gets the default without a second query.
func (r *Repos) Branches(ctx context.Context, companyID id.ID) ([]domain.Branch, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, company_id, code, name, is_default, address_json, is_active
		  FROM branches
		 WHERE company_id = ?
		 ORDER BY is_default DESC, code`, string(companyID))
	if err != nil {
		return nil, r.wrap(err, "listing branches")
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Branch
	for rows.Next() {
		var (
			b               domain.Branch
			address         sql.NullString
			isDefault, live int
		)
		if scanErr := rows.Scan(&b.ID, &b.CompanyID, &b.Code, &b.Name,
			&isDefault, &address, &live); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a branch")
		}
		b.Address, b.IsDefault, b.IsActive = address.String, isDefault == 1, live == 1
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, r.wrap(err, "listing branches")
	}
	return out, nil
}

// CountActiveBranches supports the last-active-branch guard.
func (r *Repos) CountActiveBranches(ctx context.Context, companyID id.ID) (int, error) {
	var n int
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT COUNT(*) FROM branches WHERE company_id = ? AND is_active = 1`,
		string(companyID)).Scan(&n)
	if err != nil {
		return 0, r.wrap(err, "counting active branches")
	}
	return n, nil
}

// SetBranchActive switches a branch on or off.
func (r *Repos) SetBranchActive(ctx context.Context, branchID id.ID, active bool) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE branches
		   SET is_active = ?, updated_at = ?, row_version = row_version + 1
		 WHERE id = ?`, boolToInt(active), r.now(), string(branchID))
	if err != nil {
		return r.wrap(err, "updating the branch")
	}
	return nil
}

// ── warehouses ──────────────────────────────────────────────────────────────────

// InsertWarehouse writes a warehouse.
func (r *Repos) InsertWarehouse(ctx context.Context, w domain.Warehouse) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO warehouses (
			id, branch_id, code, name, is_default, allows_negative_stock, is_active,
			created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		string(w.ID), string(w.BranchID), w.Code, w.Name,
		boolToInt(w.IsDefault), boolToInt(w.AllowsNegativeStock),
		boolToInt(w.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting the warehouse")
	}
	return nil
}

// Warehouses lists a branch's warehouses, defaults first.
func (r *Repos) Warehouses(ctx context.Context, branchID id.ID) ([]domain.Warehouse, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, branch_id, code, name, is_default, allows_negative_stock, is_active
		  FROM warehouses
		 WHERE branch_id = ?
		 ORDER BY is_default DESC, code`, string(branchID))
	if err != nil {
		return nil, r.wrap(err, "listing warehouses")
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Warehouse
	for rows.Next() {
		var w domain.Warehouse
		var isDefault, negative, live int
		if scanErr := rows.Scan(&w.ID, &w.BranchID, &w.Code, &w.Name,
			&isDefault, &negative, &live); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a warehouse")
		}
		w.IsDefault, w.AllowsNegativeStock, w.IsActive = isDefault == 1, negative == 1, live == 1
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, r.wrap(err, "listing warehouses")
	}
	return out, nil
}

// CountActiveWarehouses supports the last-active-warehouse guard.
func (r *Repos) CountActiveWarehouses(ctx context.Context, branchID id.ID) (int, error) {
	var n int
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT COUNT(*) FROM warehouses WHERE branch_id = ? AND is_active = 1`,
		string(branchID)).Scan(&n)
	if err != nil {
		return 0, r.wrap(err, "counting active warehouses")
	}
	return n, nil
}

// SetWarehouseActive switches a warehouse on or off.
func (r *Repos) SetWarehouseActive(ctx context.Context, warehouseID id.ID, active bool) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE warehouses
		   SET is_active = ?, updated_at = ?, row_version = row_version + 1
		 WHERE id = ?`, boolToInt(active), r.now(), string(warehouseID))
	if err != nil {
		return r.wrap(err, "updating the warehouse")
	}
	return nil
}

// ── fiscal calendar ─────────────────────────────────────────────────────────────

// InsertFiscalYear writes a fiscal year and all of its periods.
//
// One method rather than two, because a year without its periods is never a legitimate state
// and splitting them would invite a caller to create one.
func (r *Repos) InsertFiscalYear(ctx context.Context, fy domain.FiscalYear) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO fiscal_years (
			id, company_id, code, start_date, end_date, status,
			created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		string(fy.ID), string(fy.CompanyID), fy.Code,
		clock.FormatDate(fy.Start), clock.FormatDate(fy.End), fy.Status, now, now)
	if err != nil {
		return r.wrap(err, "inserting the fiscal year")
	}

	for _, p := range fy.Periods {
		_, err = r.db.Writer(ctx).ExecContext(ctx, `
			INSERT INTO fiscal_periods (
				id, fiscal_year_id, sequence, start_date, end_date, status,
				created_at, updated_at, row_version
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)`,
			string(p.ID), string(p.FiscalYearID), p.Sequence,
			clock.FormatDate(p.Start), clock.FormatDate(p.End), p.Status, now, now)
		if err != nil {
			return r.wrap(err, "inserting a fiscal period")
		}
	}
	return nil
}

// FiscalYears lists a company's fiscal years with their periods, oldest first.
func (r *Repos) FiscalYears(ctx context.Context, companyID id.ID) ([]domain.FiscalYear, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, company_id, code, start_date, end_date, status
		  FROM fiscal_years
		 WHERE company_id = ?
		 ORDER BY start_date`, string(companyID))
	if err != nil {
		return nil, r.wrap(err, "listing fiscal years")
	}
	defer func() { _ = rows.Close() }()

	var out []domain.FiscalYear
	for rows.Next() {
		var (
			fy                 domain.FiscalYear
			startDate, endDate string
		)
		if scanErr := rows.Scan(&fy.ID, &fy.CompanyID, &fy.Code,
			&startDate, &endDate, &fy.Status); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a fiscal year")
		}
		fy.Start, _ = clock.ParseDate(startDate)
		fy.End, _ = clock.ParseDate(endDate)
		out = append(out, fy)
	}
	if err := rows.Err(); err != nil {
		return nil, r.wrap(err, "listing fiscal years")
	}

	for i := range out {
		periods, perr := r.fiscalPeriods(ctx, out[i].ID)
		if perr != nil {
			return nil, perr
		}
		out[i].Periods = periods
	}
	return out, nil
}

func (r *Repos) fiscalPeriods(ctx context.Context, yearID id.ID) ([]domain.FiscalPeriod, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, fiscal_year_id, sequence, start_date, end_date, status
		  FROM fiscal_periods
		 WHERE fiscal_year_id = ?
		 ORDER BY sequence`, string(yearID))
	if err != nil {
		return nil, r.wrap(err, "listing fiscal periods")
	}
	defer func() { _ = rows.Close() }()

	var out []domain.FiscalPeriod
	for rows.Next() {
		var (
			p                  domain.FiscalPeriod
			startDate, endDate string
		)
		if scanErr := rows.Scan(&p.ID, &p.FiscalYearID, &p.Sequence,
			&startDate, &endDate, &p.Status); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a fiscal period")
		}
		p.Start, _ = clock.ParseDate(startDate)
		p.End, _ = clock.ParseDate(endDate)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ── helpers ─────────────────────────────────────────────────────────────────────

// nullable maps "" to NULL, so an absent optional value is stored as absent rather than as an
// empty string that every later query would have to treat as a third case.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// boolToInt encodes the §8.1 SMALLINT boolean contract.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
