package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/catalog/domain"
)

// ── exclusions ──────────────────────────────────────────────────────────────────

// InsertExclusion records a combination a product never makes.
func (r *Repos) InsertExclusion(
	ctx context.Context, productID id.ID, e domain.Exclusion,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO variant_exclusions (
			id, product_id, combination, reason, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, 1, ?, ?)`,
		string(e.ID), string(productID), e.Combination, nullable(e.Reason), now, now)
	if err != nil {
		return r.wrap(err, "recording a variant exclusion")
	}
	return nil
}

// Exclusions lists a product's exclusions.
func (r *Repos) Exclusions(ctx context.Context, productID id.ID) ([]domain.Exclusion, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT id, combination, reason FROM variant_exclusions
		 WHERE product_id = ? ORDER BY combination`, string(productID))
	if err != nil {
		return nil, r.wrap(err, "listing variant exclusions")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Exclusion, 0, 4)
	for rows.Next() {
		var (
			e      domain.Exclusion
			reason any
		)
		if err = rows.Scan(&e.ID, &e.Combination, &reason); err != nil {
			return nil, r.wrap(err, "reading a variant exclusion")
		}
		e.Reason = text(reason)
		out = append(out, e)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing variant exclusions")
	}
	return out, nil
}

// ── applying a generation plan ──────────────────────────────────────────────────

// AssignValue records one attribute value a generated variant carries.
func (r *Repos) AssignValue(
	ctx context.Context, rowID, variantID, attributeID, valueID id.ID,
) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO product_variant_values (
			id, variant_id, attribute_id, attribute_value_id, created_at
		) VALUES (?, ?, ?, ?, ?)`,
		string(rowID), string(variantID), string(attributeID), string(valueID), r.now())
	if err != nil {
		return r.wrap(err, "recording a variant's attribute value")
	}
	return nil
}

// SetVariantActive retires or revives a variant.
func (r *Repos) SetVariantActive(ctx context.Context, variantID id.ID, active bool) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE product_variants SET is_active = ?, row_version = row_version + 1, updated_at = ?
		WHERE id = ?`, boolToInt(active), r.now(), string(variantID))
	if err != nil {
		return r.wrap(err, "changing a variant's active state")
	}
	return nil
}

// DeleteVariant removes a variant.
//
// The refusal that protects history lives in the domain — this is only reached for a variant the
// plan already decided has none. A second guard here would be a second rule to keep in step, and
// the one in SQL could not say why it refused.
func (r *Repos) DeleteVariant(ctx context.Context, variantID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx,
		`DELETE FROM product_variants WHERE id = ?`, string(variantID))
	if err != nil {
		return r.wrap(err, "deleting a variant")
	}
	return nil
}

// ── packagings ──────────────────────────────────────────────────────────────────

// InsertPackaging writes a product-specific bundle.
func (r *Repos) InsertPackaging(ctx context.Context, p domain.Packaging) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO product_packagings (
			id, product_id, code, name, name_key, quantity_micro, is_default, is_active,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(p.ID), string(p.ProductID), p.Code, p.Name, nullable(p.NameKey),
		p.QuantityMicro, boolToInt(p.IsDefault), boolToInt(p.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting a packaging")
	}
	return nil
}

const packagingColumns = `
	id, product_id, code, name, name_key, quantity_micro, is_default, is_active`

// Packagings lists a product's packagings, largest first — which is the order a till offers
// them in, because the box is what a delivery arrives in.
func (r *Repos) Packagings(ctx context.Context, productID id.ID) ([]domain.Packaging, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+packagingColumns+` FROM product_packagings
		 WHERE product_id = ? ORDER BY quantity_micro DESC`, string(productID))
	if err != nil {
		return nil, r.wrap(err, "listing packagings")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Packaging, 0, 4)
	for rows.Next() {
		packaging, scanErr := scanPackaging(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "reading a packaging")
		}
		out = append(out, packaging)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing packagings")
	}
	return out, nil
}

// PackagingByCode finds one of a product's packagings.
func (r *Repos) PackagingByCode(
	ctx context.Context, productID id.ID, code string,
) (domain.Packaging, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+packagingColumns+` FROM product_packagings WHERE product_id = ? AND code = ?`,
		string(productID), code)

	packaging, err := scanPackaging(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Packaging{}, false, nil
	}
	if err != nil {
		return domain.Packaging{}, false, r.wrap(err, "reading a packaging")
	}
	return packaging, true, nil
}

func scanPackaging(s scanner) (domain.Packaging, error) {
	var (
		p               domain.Packaging
		nameKey         any
		isDefault, live int
	)
	if err := s.Scan(&p.ID, &p.ProductID, &p.Code, &p.Name, &nameKey,
		&p.QuantityMicro, &isDefault, &live); err != nil {
		return domain.Packaging{}, err
	}
	p.NameKey = text(nameKey)
	p.IsDefault = isDefault == 1
	p.IsActive = live == 1
	return p, nil
}

// ── barcodes ────────────────────────────────────────────────────────────────────

// InsertBarcode writes a barcode.
func (r *Repos) InsertBarcode(ctx context.Context, b domain.Barcode) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO barcodes (
			id, variant_id, packaging_id, code, barcode_type, is_primary, is_active,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(b.ID), string(b.VariantID), nullableID(b.PackagingID), b.Code, b.Type,
		boolToInt(b.IsPrimary), boolToInt(b.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting a barcode")
	}
	return nil
}

// BarcodeByCode finds a barcode.
//
// No company filter, deliberately, and it is the same reason the unique index has none: a scan
// must resolve to exactly one thing, and scoping the lookup would let two companies in one
// installation print the same code and each get a different answer from the same scanner.
func (r *Repos) BarcodeByCode(ctx context.Context, code string) (domain.Barcode, bool, error) {
	var (
		b             domain.Barcode
		packaging     any
		primary, live int
	)
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT id, variant_id, packaging_id, code, barcode_type, is_primary, is_active
		FROM barcodes WHERE code = ?`, code).
		Scan(&b.ID, &b.VariantID, &packaging, &b.Code, &b.Type, &primary, &live)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Barcode{}, false, nil
	}
	if err != nil {
		return domain.Barcode{}, false, r.wrap(err, "reading a barcode")
	}
	b.PackagingID = id.ID(text(packaging))
	b.IsPrimary = primary == 1
	b.IsActive = live == 1
	return b, true, nil
}

// PackagingByID finds one packaging.
func (r *Repos) PackagingByID(
	ctx context.Context, packagingID id.ID,
) (domain.Packaging, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+packagingColumns+` FROM product_packagings WHERE id = ?`, string(packagingID))

	packaging, err := scanPackaging(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Packaging{}, false, nil
	}
	if err != nil {
		return domain.Packaging{}, false, r.wrap(err, "reading a packaging")
	}
	return packaging, true, nil
}

// Barcodes lists a variant's barcodes.
func (r *Repos) Barcodes(ctx context.Context, variantID id.ID) ([]domain.Barcode, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, variant_id, packaging_id, code, barcode_type, is_primary, is_active
		FROM barcodes WHERE variant_id = ? ORDER BY is_primary DESC, code`, string(variantID))
	if err != nil {
		return nil, r.wrap(err, "listing barcodes")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Barcode, 0, 4)
	for rows.Next() {
		var (
			b             domain.Barcode
			packaging     any
			primary, live int
		)
		if err = rows.Scan(&b.ID, &b.VariantID, &packaging, &b.Code, &b.Type,
			&primary, &live); err != nil {
			return nil, r.wrap(err, "reading a barcode")
		}
		b.PackagingID = id.ID(text(packaging))
		b.IsPrimary = primary == 1
		b.IsActive = live == 1
		out = append(out, b)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing barcodes")
	}
	return out, nil
}
