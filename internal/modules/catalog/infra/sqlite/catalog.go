package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/catalog/domain"
)

// ── categories ──────────────────────────────────────────────────────────────────

// InsertCategory writes a category.
func (r *Repos) InsertCategory(ctx context.Context, companyID id.ID, c domain.Category) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO product_categories (
			id, company_id, code, name, name_key, parent_id, path, depth,
			income_account_id, expense_account_id, inventory_account_id,
			sort_order, is_active, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(c.ID), string(companyID), c.Code, c.Name, nullable(c.NameKey),
		nullableID(c.ParentID), c.Path, c.Depth,
		nullableID(c.IncomeAccountID), nullableID(c.ExpenseAccountID),
		nullableID(c.InventoryAccountID),
		c.SortOrder, boolToInt(c.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting a product category")
	}
	return nil
}

const categoryColumns = `
	id, code, name, name_key, parent_id, path, depth,
	income_account_id, expense_account_id, inventory_account_id, sort_order, is_active`

// Categories lists a company's categories in tree order.
//
// Ordered by `path`, which is why the path is materialised: a hierarchy renders correctly from a
// flat ORDER BY, with no recursive query and no sorting in Go.
func (r *Repos) Categories(ctx context.Context, companyID id.ID) ([]domain.Category, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+categoryColumns+` FROM product_categories
		 WHERE company_id = ? ORDER BY path`, string(companyID))
	if err != nil {
		return nil, r.wrap(err, "listing product categories")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Category, 0, 16)
	for rows.Next() {
		c, scanErr := scanCategory(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "reading a product category")
		}
		out = append(out, c)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing product categories")
	}
	return out, nil
}

// CategoryByCode finds one category. Missing is (zero, false, nil), not an error: the caller
// decides whether absence is a problem, and only the caller knows.
func (r *Repos) CategoryByCode(
	ctx context.Context, companyID id.ID, code string,
) (domain.Category, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+categoryColumns+` FROM product_categories WHERE company_id = ? AND code = ?`,
		string(companyID), code)

	c, err := scanCategory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Category{}, false, nil
	}
	if err != nil {
		return domain.Category{}, false, r.wrap(err, "reading a product category")
	}
	return c, true, nil
}

func scanCategory(s scanner) (domain.Category, error) {
	var (
		c                          domain.Category
		nameKey, parentID          any
		income, expense, inventory any
		active                     int
	)
	if err := s.Scan(&c.ID, &c.Code, &c.Name, &nameKey, &parentID, &c.Path, &c.Depth,
		&income, &expense, &inventory, &c.SortOrder, &active); err != nil {
		return domain.Category{}, err
	}
	c.NameKey = text(nameKey)
	c.ParentID = id.ID(text(parentID))
	c.IncomeAccountID = id.ID(text(income))
	c.ExpenseAccountID = id.ID(text(expense))
	c.InventoryAccountID = id.ID(text(inventory))
	c.IsActive = active == 1
	return domain.AdoptCategory(c), nil
}

// ── products and variants ───────────────────────────────────────────────────────

// InsertProduct writes a product AND its default variant.
//
// One method, taking both, because the domain constructor produces both and no caller should be
// able to persist half of what it returned. §A.1's guarantee — every product has at least one
// variant — survives a partial write only if there is no way to perform one.
//
// The caller supplies the transaction; both statements run inside it.
func (r *Repos) InsertProduct(
	ctx context.Context, companyID id.ID, p domain.Product, defaultVariant domain.Variant,
) error {
	now := r.now()
	if _, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO products (
			id, company_id, code, name, name_key, description, category_id, product_type,
			stock_uom_id, sales_uom_id, purchase_uom_id, tracking, tax_group_id,
			income_account_id, expense_account_id, inventory_account_id,
			has_history, is_active, is_sold, is_purchased, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(p.ID), string(companyID), p.Code, p.Name, nullable(p.NameKey),
		nullableID(p.CategoryID), string(p.Type),
		string(p.StockUnitID), string(p.SalesUnitID), string(p.PurchaseUnitID),
		string(p.Tracking), nullableID(p.TaxGroupID),
		nullableID(p.IncomeAccountID), nullableID(p.ExpenseAccountID),
		nullableID(p.InventoryAccountID),
		boolToInt(p.HasHistory), boolToInt(p.IsActive),
		boolToInt(p.IsSold), boolToInt(p.IsPurchased), now, now); err != nil {
		return r.wrap(err, "inserting a product")
	}
	return r.InsertVariant(ctx, defaultVariant)
}

// InsertVariant writes one variant.
func (r *Repos) InsertVariant(ctx context.Context, v domain.Variant) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO product_variants (
			id, product_id, sku, name, is_default, combination, barcode_hint,
			has_history, is_active, row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, 1, ?, ?)`,
		string(v.ID), string(v.ProductID), v.SKU, nullable(v.Name),
		boolToInt(v.IsDefault), v.Combination,
		boolToInt(v.HasHistory), boolToInt(v.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting a product variant")
	}
	return nil
}

const productColumns = `
	id, code, name, name_key, category_id, product_type,
	stock_uom_id, sales_uom_id, purchase_uom_id, tracking, tax_group_id,
	income_account_id, expense_account_id, inventory_account_id,
	has_history, is_active, is_sold, is_purchased`

// Products lists a company's products by code.
func (r *Repos) Products(ctx context.Context, companyID id.ID) ([]domain.Product, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+productColumns+` FROM products WHERE company_id = ? ORDER BY code`,
		string(companyID))
	if err != nil {
		return nil, r.wrap(err, "listing products")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Product, 0, 32)
	for rows.Next() {
		p, scanErr := scanProduct(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "reading a product")
		}
		out = append(out, p)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing products")
	}
	return out, nil
}

// ProductByCode finds one product.
func (r *Repos) ProductByCode(
	ctx context.Context, companyID id.ID, code string,
) (domain.Product, bool, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+productColumns+` FROM products WHERE company_id = ? AND code = ?`,
		string(companyID), code)

	p, err := scanProduct(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Product{}, false, nil
	}
	if err != nil {
		return domain.Product{}, false, r.wrap(err, "reading a product")
	}
	return p, true, nil
}

func scanProduct(s scanner) (domain.Product, error) {
	var (
		p                                domain.Product
		nameKey, categoryID, taxGroup    any
		income, expense, inventory       any
		productType, tracking            string
		history, active, sold, purchased int
	)
	if err := s.Scan(&p.ID, &p.Code, &p.Name, &nameKey, &categoryID, &productType,
		&p.StockUnitID, &p.SalesUnitID, &p.PurchaseUnitID, &tracking, &taxGroup,
		&income, &expense, &inventory,
		&history, &active, &sold, &purchased); err != nil {
		return domain.Product{}, err
	}
	p.NameKey = text(nameKey)
	p.CategoryID = id.ID(text(categoryID))
	p.Type = domain.ProductType(productType)
	p.Tracking = domain.Tracking(tracking)
	p.TaxGroupID = id.ID(text(taxGroup))
	p.IncomeAccountID = id.ID(text(income))
	p.ExpenseAccountID = id.ID(text(expense))
	p.InventoryAccountID = id.ID(text(inventory))
	p.HasHistory = history == 1
	p.IsActive = active == 1
	p.IsSold = sold == 1
	p.IsPurchased = purchased == 1
	return domain.AdoptProduct(p), nil
}

const variantColumns = `id, product_id, sku, name, is_default, combination, has_history, is_active`

// Variants lists a product's variants, the default first.
func (r *Repos) Variants(ctx context.Context, productID id.ID) ([]domain.Variant, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+variantColumns+` FROM product_variants
		 WHERE product_id = ? ORDER BY is_default DESC, sku`, string(productID))
	if err != nil {
		return nil, r.wrap(err, "listing product variants")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Variant, 0, 8)
	for rows.Next() {
		var (
			v       domain.Variant
			name    any
			def     int
			history int
			active  int
		)
		if err = rows.Scan(&v.ID, &v.ProductID, &v.SKU, &name, &def, &v.Combination,
			&history, &active); err != nil {
			return nil, r.wrap(err, "reading a product variant")
		}
		v.Name = text(name)
		v.IsDefault = def == 1
		v.HasHistory = history == 1
		v.IsActive = active == 1
		out = append(out, domain.AdoptVariant(v))
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing product variants")
	}
	return out, nil
}

// MarkVariantHistory records that a variant now appears on a document.
//
// Phase 4 and Phase 5 call this; the domain reads the flag to refuse deletion. It exists now so
// that the rule it feeds can be tested now rather than trusted until then.
func (r *Repos) MarkVariantHistory(ctx context.Context, variantID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE product_variants SET has_history = 1, row_version = row_version + 1,
			updated_at = ? WHERE id = ?`, r.now(), string(variantID))
	if err != nil {
		return r.wrap(err, "marking a variant as having history")
	}
	// The product carries the same flag, because a stock_uom change is refused per PRODUCT.
	if _, err = r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE products SET has_history = 1, row_version = row_version + 1, updated_at = ?
		WHERE id = (SELECT product_id FROM product_variants WHERE id = ?)`,
		r.now(), string(variantID)); err != nil {
		return r.wrap(err, "marking a product as having history")
	}
	return nil
}

// ── attributes ──────────────────────────────────────────────────────────────────

// InsertAttribute writes a dictionary attribute.
func (r *Repos) InsertAttribute(
	ctx context.Context, companyID id.ID, a domain.Attribute,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO attributes (
			id, company_id, code, name, name_key, value_type, sort_order, is_active,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(a.ID), string(companyID), a.Code, a.Name, nullable(a.NameKey),
		a.ValueType, a.SortOrder, boolToInt(a.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting an attribute")
	}
	return nil
}

// InsertAttributeValue writes one option of a list attribute.
func (r *Repos) InsertAttributeValue(ctx context.Context, v domain.AttributeValue) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO attribute_values (
			id, attribute_id, code, name, name_key, display_hint, sort_order, is_active,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		string(v.ID), string(v.AttributeID), v.Code, v.Name, nullable(v.NameKey),
		nullable(v.DisplayHint), v.SortOrder, boolToInt(v.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting an attribute value")
	}
	return nil
}

// AttributeByCode finds one dictionary attribute.
func (r *Repos) AttributeByCode(
	ctx context.Context, companyID id.ID, code string,
) (domain.Attribute, bool, error) {
	var (
		a       domain.Attribute
		nameKey any
		active  int
	)
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT id, code, name, name_key, value_type, sort_order, is_active
		FROM attributes WHERE company_id = ? AND code = ?`, string(companyID), code).
		Scan(&a.ID, &a.Code, &a.Name, &nameKey, &a.ValueType, &a.SortOrder, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Attribute{}, false, nil
	}
	if err != nil {
		return domain.Attribute{}, false, r.wrap(err, "reading an attribute")
	}
	a.NameKey = text(nameKey)
	a.IsActive = active == 1
	return a, true, nil
}

// AttributeValues lists an attribute's options.
func (r *Repos) AttributeValues(
	ctx context.Context, attributeID id.ID,
) ([]domain.AttributeValue, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, attribute_id, code, name, name_key, display_hint, sort_order, is_active
		FROM attribute_values WHERE attribute_id = ? ORDER BY sort_order, code`,
		string(attributeID))
	if err != nil {
		return nil, r.wrap(err, "listing attribute values")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.AttributeValue, 0, 8)
	for rows.Next() {
		var (
			v             domain.AttributeValue
			nameKey, hint any
			active        int
		)
		if err = rows.Scan(&v.ID, &v.AttributeID, &v.Code, &v.Name, &nameKey, &hint,
			&v.SortOrder, &active); err != nil {
			return nil, r.wrap(err, "reading an attribute value")
		}
		v.NameKey = text(nameKey)
		v.DisplayHint = text(hint)
		v.IsActive = active == 1
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing attribute values")
	}
	return out, nil
}

// LinkAttribute attaches an attribute to a product with its meaning for that product.
//
// `variantDefining` is the §A.2 flag, and it lives on the LINK: material defines variants for a
// sofa and describes a screwdriver, and a dictionary shared by both cannot say which.
func (r *Repos) LinkAttribute(
	ctx context.Context, linkID, productID, attributeID id.ID, variantDefining bool, order int,
) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO product_attributes (
			id, product_id, attribute_id, is_variant_defining, sort_order,
			row_version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		string(linkID), string(productID), string(attributeID),
		boolToInt(variantDefining), order, now, now)
	if err != nil {
		return r.wrap(err, "linking an attribute to a product")
	}
	return nil
}

// OfferValue records that a product offers one of an attribute's values.
func (r *Repos) OfferValue(ctx context.Context, rowID, linkID, valueID id.ID, order int) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO product_attribute_values (
			id, product_attribute_id, attribute_value_id, raw_value, sort_order, created_at
		) VALUES (?, ?, ?, NULL, ?, ?)`,
		string(rowID), string(linkID), string(valueID), order, r.now())
	if err != nil {
		return r.wrap(err, "recording an offered attribute value")
	}
	return nil
}

// ProductAttribute is a product's link to a dictionary attribute, with the values it offers.
type ProductAttribute struct {
	LinkID          id.ID
	Attribute       domain.Attribute
	VariantDefining bool
	SortOrder       int
	Values          []domain.AttributeValue
}

// ProductAttributes lists a product's attributes with the values it offers.
//
// Variant-defining ones first and in `sort_order`, because that order IS the order of the SKU
// suffix and of the Cartesian product: "SHIRT-RED-L" rather than "SHIRT-L-RED" is a decision
// made once, here, rather than by whichever query happened to run.
func (r *Repos) ProductAttributes(
	ctx context.Context, productID id.ID,
) ([]ProductAttribute, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT pa.id, pa.is_variant_defining, pa.sort_order,
		       a.id, a.code, a.name, a.name_key, a.value_type, a.sort_order, a.is_active
		FROM product_attributes pa
		JOIN attributes a ON a.id = pa.attribute_id
		WHERE pa.product_id = ?
		ORDER BY pa.is_variant_defining DESC, pa.sort_order, a.code`, string(productID))
	if err != nil {
		return nil, r.wrap(err, "listing a product's attributes")
	}
	defer func() { _ = rows.Close() }()

	out := make([]ProductAttribute, 0, 4)
	for rows.Next() {
		var (
			link     ProductAttribute
			defining int
			nameKey  any
			active   int
		)
		if err = rows.Scan(&link.LinkID, &defining, &link.SortOrder,
			&link.Attribute.ID, &link.Attribute.Code, &link.Attribute.Name, &nameKey,
			&link.Attribute.ValueType, &link.Attribute.SortOrder, &active); err != nil {
			return nil, r.wrap(err, "reading a product attribute")
		}
		link.Attribute.NameKey = text(nameKey)
		link.Attribute.IsActive = active == 1
		link.VariantDefining = defining == 1
		out = append(out, link)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing a product's attributes")
	}

	// The offered values, one query per link. A product has a handful of attributes, so this is
	// bounded and readable; a single join returning the cross-product would need de-duplicating
	// in Go, which is the trade the other direction.
	for i := range out {
		values, valErr := r.offeredValues(ctx, out[i].LinkID)
		if valErr != nil {
			return nil, valErr
		}
		out[i].Values = values
	}
	return out, nil
}

func (r *Repos) offeredValues(
	ctx context.Context, linkID id.ID,
) ([]domain.AttributeValue, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT v.id, v.attribute_id, v.code, v.name, v.name_key, v.display_hint,
		       pav.sort_order, v.is_active
		FROM product_attribute_values pav
		JOIN attribute_values v ON v.id = pav.attribute_value_id
		WHERE pav.product_attribute_id = ?
		ORDER BY pav.sort_order, v.code`, string(linkID))
	if err != nil {
		return nil, r.wrap(err, "listing offered attribute values")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.AttributeValue, 0, 8)
	for rows.Next() {
		var (
			v             domain.AttributeValue
			nameKey, hint any
			active        int
		)
		if err = rows.Scan(&v.ID, &v.AttributeID, &v.Code, &v.Name, &nameKey, &hint,
			&v.SortOrder, &active); err != nil {
			return nil, r.wrap(err, "reading an offered attribute value")
		}
		v.NameKey = text(nameKey)
		v.DisplayHint = text(hint)
		v.IsActive = active == 1
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, r.wrap(err, "listing offered attribute values")
	}
	return out, nil
}

func nullableID(v id.ID) any {
	if v.IsZero() {
		return nil
	}
	return string(v)
}

// UnitByID finds one unit by identity, for the paths that already hold an id rather than a code.
func (r *Repos) UnitByID(ctx context.Context, unitID id.ID) (domain.Unit, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+unitColumns+` FROM units_of_measure WHERE id = ?`, string(unitID))
	unit, err := scanUnit(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Unit{}, errs.NotFound(domain.CodeInvalidUnit, "no such unit").
			WithParam("id", string(unitID))
	}
	if err != nil {
		return domain.Unit{}, r.wrap(err, "reading a unit")
	}
	return unit, nil
}

// UpdateStockUnit changes the unit a product's stock is held in.
//
// The refusal that matters lives in the domain, not here: this writes what the domain already
// agreed to. A guard in both would be two rules to keep in step, and the one in SQL cannot say
// why it refused.
func (r *Repos) UpdateStockUnit(ctx context.Context, productID, unitID id.ID) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE products SET stock_uom_id = ?, row_version = row_version + 1, updated_at = ?
		WHERE id = ?`, string(unitID), r.now(), string(productID))
	if err != nil {
		return r.wrap(err, "changing a product's stock unit")
	}
	return nil
}

// ProductsByIDs reads products by identity.
func (r *Repos) ProductsByIDs(
	ctx context.Context, productID id.ID,
) ([]domain.Product, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+productColumns+` FROM products WHERE id = ?`, string(productID))
	if err != nil {
		return nil, r.wrap(err, "reading a product")
	}
	defer func() { _ = rows.Close() }()

	out := make([]domain.Product, 0, 1)
	for rows.Next() {
		p, scanErr := scanProduct(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "reading a product")
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
