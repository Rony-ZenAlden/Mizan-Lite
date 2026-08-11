// Package sqlite is the pricing module's persistence.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/pricing/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeStorage is the stable code for a persistence failure.
const CodeStorage = "pricing.storage"

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

// ── lists ───────────────────────────────────────────────────────────────────────

// InsertList writes a price list.
func (r *Repos) InsertList(ctx context.Context, companyID id.ID, l domain.List) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO price_lists (
			id, company_id, code, name, name_key, currency_code, is_default, direction,
			valid_from, valid_to, is_active, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(l.ID), string(companyID), l.Code, l.Name, nullable(l.NameKey),
		l.CurrencyCode, boolToInt(l.IsDefault), string(l.Direction),
		nullable(l.ValidFrom), nullable(l.ValidTo), boolToInt(l.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting a price list")
	}
	return nil
}

const listColumns = `
	id, code, name, name_key, currency_code, is_default, direction,
	valid_from, valid_to, is_active`

// Lists returns a company's price lists for one direction.
func (r *Repos) Lists(
	ctx context.Context, companyID id.ID, direction domain.Direction,
) ([]domain.List, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+listColumns+` FROM price_lists
		 WHERE company_id = ? AND direction = ? ORDER BY is_default DESC, code`,
		string(companyID), string(direction))
	if err != nil {
		return nil, r.wrap(err, "listing price lists")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.List, 0, 8)
	for rows.Next() {
		l, scanErr := scanList(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "reading a price list")
		}
		out = append(out, l)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing price lists")
	}
	return out, nil
}

// ListByCode finds one price list.
func (r *Repos) ListByCode(
	ctx context.Context, companyID id.ID, code string,
) (domain.List, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+listColumns+` FROM price_lists WHERE company_id = ? AND code = ?`,
		string(companyID), code)

	l, err := scanList(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.List{}, false, nil
	}
	if err != nil {
		return domain.List{}, false, r.wrap(err, "reading a price list")
	}
	return l, true, nil
}

// DefaultList finds the company's default list for a direction — resolution's last stop.
func (r *Repos) DefaultList(
	ctx context.Context, companyID id.ID, direction domain.Direction,
) (domain.List, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+listColumns+` FROM price_lists
		 WHERE company_id = ? AND direction = ? AND is_default = 1`,
		string(companyID), string(direction))

	l, err := scanList(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.List{}, false, nil
	}
	if err != nil {
		return domain.List{}, false, r.wrap(err, "reading the default price list")
	}
	return l, true, nil
}

func scanList(s scanner) (domain.List, error) {
	var (
		l                           domain.List
		nameKey, validFrom, validTo any
		direction                   string
		isDefault, active           int
	)
	if err := s.Scan(&l.ID, &l.Code, &l.Name, &nameKey, &l.CurrencyCode,
		&isDefault, &direction, &validFrom, &validTo, &active); err != nil {
		return domain.List{}, err
	}
	l.NameKey = text(nameKey)
	l.IsDefault = isDefault == 1
	l.Direction = domain.Direction(direction)
	l.ValidFrom = text(validFrom)
	l.ValidTo = text(validTo)
	l.IsActive = active == 1
	return l, nil
}

// ── items ───────────────────────────────────────────────────────────────────────

// InsertItem writes a price.
func (r *Repos) InsertItem(ctx context.Context, i domain.Item) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO price_list_items (
			id, price_list_id, product_id, variant_id, price_minor, min_quantity_micro,
			is_active, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(i.ID), string(i.ListID), nullableID(i.ProductID), nullableID(i.VariantID),
		i.PriceMinor, i.MinQuantity, boolToInt(i.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting a price")
	}
	return nil
}

// ItemsFor loads one list's prices for a variant and for its product, in two slices.
//
// Two slices rather than one, because the resolution rule treats them differently: a
// variant-level price beats a product-level one within the same list. Returning them mixed would
// push that distinction into the caller, which is exactly where §2.6 says it must not live.
func (r *Repos) ItemsFor(
	ctx context.Context, listID, productID, variantID id.ID,
) (variantItems, productItems []domain.Item, err error) {
	variantItems, err = r.items(ctx,
		`SELECT id, price_list_id, product_id, variant_id, price_minor,
		        min_quantity_micro, is_active
		 FROM price_list_items WHERE price_list_id = ? AND variant_id = ?`,
		string(listID), string(variantID))
	if err != nil {
		return nil, nil, err
	}
	productItems, err = r.items(ctx,
		`SELECT id, price_list_id, product_id, variant_id, price_minor,
		        min_quantity_micro, is_active
		 FROM price_list_items WHERE price_list_id = ? AND product_id = ?`,
		string(listID), string(productID))
	if err != nil {
		return nil, nil, err
	}
	return variantItems, productItems, nil
}

func (r *Repos) items(ctx context.Context, query string, args ...any) ([]domain.Item, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, r.wrap(err, "listing prices")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Item, 0, 4)
	for rows.Next() {
		var (
			i                domain.Item
			product, variant any
			active           int
		)
		if err = rows.Scan(&i.ID, &i.ListID, &product, &variant, &i.PriceMinor,
			&i.MinQuantity, &active); err != nil {
			return nil, r.wrap(err, "reading a price")
		}
		i.ProductID = id.ID(text(product))
		i.VariantID = id.ID(text(variant))
		i.IsActive = active == 1
		out = append(out, i)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing prices")
	}
	return out, nil
}

// ListItems returns every price in a list, for a maintenance screen.
func (r *Repos) ListItems(ctx context.Context, listID id.ID) ([]domain.Item, error) {
	return r.items(ctx,
		`SELECT id, price_list_id, product_id, variant_id, price_minor,
		        min_quantity_micro, is_active
		 FROM price_list_items WHERE price_list_id = ?
		 ORDER BY COALESCE(product_id, variant_id), min_quantity_micro`,
		string(listID))
}

// ── assignments ─────────────────────────────────────────────────────────────────

// AssignToPartner gives a partner a price list for one direction.
func (r *Repos) AssignToPartner(
	ctx context.Context, partnerID, listID id.ID, direction domain.Direction,
) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO partner_price_lists (partner_id, price_list_id, direction, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (partner_id, direction) DO UPDATE SET
			price_list_id = excluded.price_list_id, created_at = excluded.created_at`,
		string(partnerID), string(listID), string(direction), r.now())
	if err != nil {
		return r.wrap(err, "assigning a price list to a partner")
	}
	return nil
}

// AssignToBranch gives a branch a price list for one direction.
func (r *Repos) AssignToBranch(
	ctx context.Context, branchID, listID id.ID, direction domain.Direction,
) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO branch_price_lists (branch_id, price_list_id, direction, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (branch_id, direction) DO UPDATE SET
			price_list_id = excluded.price_list_id, created_at = excluded.created_at`,
		string(branchID), string(listID), string(direction), r.now())
	if err != nil {
		return r.wrap(err, "assigning a price list to a branch")
	}
	return nil
}

// PartnerList returns the list assigned to a partner, if any.
func (r *Repos) PartnerList(
	ctx context.Context, partnerID id.ID, direction domain.Direction,
) (domain.List, bool, error) {
	return r.assignedList(ctx,
		`SELECT `+prefixed(listColumns, "l")+`
		 FROM partner_price_lists a JOIN price_lists l ON l.id = a.price_list_id
		 WHERE a.partner_id = ? AND a.direction = ?`,
		string(partnerID), string(direction))
}

// BranchList returns the list assigned to a branch, if any.
func (r *Repos) BranchList(
	ctx context.Context, branchID id.ID, direction domain.Direction,
) (domain.List, bool, error) {
	return r.assignedList(ctx,
		`SELECT `+prefixed(listColumns, "l")+`
		 FROM branch_price_lists a JOIN price_lists l ON l.id = a.price_list_id
		 WHERE a.branch_id = ? AND a.direction = ?`,
		string(branchID), string(direction))
}

func (r *Repos) assignedList(
	ctx context.Context, query string, args ...any,
) (domain.List, bool, error) {
	l, err := scanList(r.db.Reader(ctx).QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		// No assignment. Not an error — most partners have none, and resolution simply moves on
		// to the next candidate.
		return domain.List{}, false, nil
	}
	if err != nil {
		return domain.List{}, false, r.wrap(err, "reading an assigned price list")
	}
	return l, true, nil
}

// prefixed qualifies a column list with a table alias, so the one definition of `listColumns`
// serves both the plain selects and the joins rather than being written out twice and drifting.
func prefixed(columns, alias string) string {
	out := make([]byte, 0, len(columns)*2)
	field := make([]byte, 0, 32)
	flush := func() {
		if len(field) == 0 {
			return
		}
		out = append(out, alias...)
		out = append(out, '.')
		out = append(out, field...)
		field = field[:0]
	}
	for i := 0; i < len(columns); i++ {
		switch c := columns[i]; c {
		case ',':
			flush()
			out = append(out, ", "...)
		case ' ', '\n', '\t':
			flush()
		default:
			field = append(field, c)
		}
	}
	flush()
	return string(out)
}
