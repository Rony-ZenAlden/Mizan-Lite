// Package sqlite is the catalog module's persistence.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/catalog/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeStorage is the stable code for a persistence failure.
const CodeStorage = "catalog.storage"

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

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// UnitCategoryRow is a category as stored.
type UnitCategoryRow struct {
	ID      id.ID
	Code    string
	Name    string
	NameKey string
}

// EnsureUnitCategory returns the category's id, creating it if it is new.
//
// Idempotent by CODE, so applying a unit set twice is safe and an upgrade that adds a category
// leaves the existing ones — including any an administrator renamed — untouched. The same rule
// the metadata seeder follows (0.5): a non-system row belongs to the person who made it.
func (r *Repos) EnsureUnitCategory(ctx context.Context, row UnitCategoryRow) (id.ID, error) {
	var existing id.ID
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT id FROM uom_categories WHERE code = ?`, row.Code).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return id.ID(""), r.wrap(err, "reading a unit category")
	}

	identifier, err := id.New()
	if err != nil {
		return id.ID(""), err
	}
	now := r.now()
	if _, err = r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO uom_categories (
			id, code, name, name_key, is_system, is_active, created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, 1, 1, ?, ?, 1)`,
		string(identifier), row.Code, row.Name, nullable(row.NameKey), now, now); err != nil {
		return id.ID(""), r.wrap(err, "inserting a unit category")
	}
	return identifier, nil
}

// EnsureUnit creates a unit if its code is new, and reports whether it did.
func (r *Repos) EnsureUnit(ctx context.Context, unit domain.Unit) (bool, error) {
	var existing string
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT id FROM units_of_measure WHERE code = ?`, unit.Code).Scan(&existing)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, r.wrap(err, "reading a unit")
	}

	now := r.now()
	if _, err = r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO units_of_measure (
			id, uom_category_id, code, name, name_key, symbol,
			factor_to_reference, is_reference, allows_fractional,
			rounding_precision, display_decimals, is_system, is_active,
			created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		string(unit.ID), string(unit.CategoryID), unit.Code, unit.Name,
		nullable(unit.NameKey), unit.Symbol,
		unit.FactorNano, boolToInt(unit.IsReference), boolToInt(unit.AllowsFractional),
		unit.RoundingPrecision, unit.DisplayDecimals,
		boolToInt(unit.IsSystem), boolToInt(unit.IsActive), now, now); err != nil {
		return false, r.wrap(err, "inserting a unit")
	}
	return true, nil
}

const unitColumns = `
	id, uom_category_id, code, name, name_key, symbol,
	factor_to_reference, is_reference, allows_fractional,
	rounding_precision, display_decimals, is_system, is_active`

// Units lists every unit, ordered by category then code.
func (r *Repos) Units(ctx context.Context) ([]domain.Unit, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+unitColumns+` FROM units_of_measure ORDER BY uom_category_id, code`)
	if err != nil {
		return nil, r.wrap(err, "listing units")
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Unit
	for rows.Next() {
		unit, scanErr := scanUnit(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a unit")
		}
		out = append(out, unit)
	}
	return out, rows.Err()
}

// UnitByCode finds one unit.
func (r *Repos) UnitByCode(ctx context.Context, code string) (domain.Unit, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+unitColumns+` FROM units_of_measure WHERE code = ?`, code)
	unit, err := scanUnit(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Unit{}, errs.NotFound(domain.CodeInvalidUnit, "no such unit").
			WithParam("code", code)
	}
	if err != nil {
		return domain.Unit{}, r.wrap(err, "reading a unit")
	}
	return unit, nil
}

// UnitsOfCategory lists one category's units, for the invariant check.
func (r *Repos) UnitsOfCategory(ctx context.Context, categoryID id.ID) ([]domain.Unit, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+unitColumns+` FROM units_of_measure WHERE uom_category_id = ? ORDER BY code`,
		string(categoryID))
	if err != nil {
		return nil, r.wrap(err, "listing a category's units")
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Unit
	for rows.Next() {
		unit, scanErr := scanUnit(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a unit")
		}
		out = append(out, unit)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanUnit(s scanner) (domain.Unit, error) {
	var (
		unit                                domain.Unit
		nameKey                             any
		reference, fractional, system, live int
	)
	if err := s.Scan(
		&unit.ID, &unit.CategoryID, &unit.Code, &unit.Name, &nameKey, &unit.Symbol,
		&unit.FactorNano, &reference, &fractional,
		&unit.RoundingPrecision, &unit.DisplayDecimals, &system, &live,
	); err != nil {
		return domain.Unit{}, err
	}
	unit.NameKey = text(nameKey)
	unit.IsReference = reference == 1
	unit.AllowsFractional = fractional == 1
	unit.IsSystem = system == 1
	unit.IsActive = live == 1
	return unit, nil
}
