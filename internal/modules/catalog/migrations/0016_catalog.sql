-- 0016_catalog — product categories, products, variants, attributes (§A).
--
-- Owned by the catalog module (§10.3), which owns 0015 already.
--
-- This migration writes down the decision the whole phase turns on, and it is worth stating in
-- the schema rather than only in a design document:
--
--   EVERY PRODUCT HAS AT LEAST ONE VARIANT.
--
-- A bag of cement has one, created automatically and never shown. Every downstream table —
-- stock_levels, stock_movements, sales_document_lines, barcodes, price_list_items — carries
-- `variant_id NOT NULL`. The alternative, a nullable variant_id meaning "the product itself",
-- forces every query, every stock lookup, every price resolution, and every report in five
-- later phases to handle two cases forever. The cost of this decision is one row per simple
-- product; the benefit is that such a product can later grow real variants without migrating a
-- single line of its transaction history — its default simply becomes the first of many.

-- ─────────────────────────────────────────────────────────────────────────────
-- product_categories — hierarchical, unlimited depth
-- ─────────────────────────────────────────────────────────────────────────────
--
-- The same materialised-path shape the chart of accounts uses (0010), and for the same reason:
-- "everything under Beverages" is a LIKE '/DRINK/%' scan rather than a recursive CTE, and
-- recursive CTEs are exactly what behaves differently on the engines §8 says this schema must
-- survive moving to.

CREATE TABLE product_categories (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  company_id   CHAR(36)     NOT NULL REFERENCES companies(id),

  -- The stable key. Imports match on it, seeds name it, an operator recognises it.
  code         VARCHAR(40)  NOT NULL,
  name         VARCHAR(200) NOT NULL,
  name_key     VARCHAR(120),

  parent_id    CHAR(36)     REFERENCES product_categories(id),
  -- Root-first, slash-separated ancestor codes: '/FOOD/DAIRY/'. Maintained by the domain on
  -- insert and on re-parent, exactly as accounts.path is.
  path         VARCHAR(400) NOT NULL,
  depth        INTEGER      NOT NULL DEFAULT 0,

  -- Where this category's sales, purchases, and stock post, when a product does not say
  -- otherwise. Phase 2 built table-driven posting (§20.3); this is the hook a category uses to
  -- override the company default without any accounting logic living in the catalog.
  income_account_id     CHAR(36) REFERENCES accounts(id),
  expense_account_id    CHAR(36) REFERENCES accounts(id),
  inventory_account_id  CHAR(36) REFERENCES accounts(id),

  sort_order   INTEGER      NOT NULL DEFAULT 0,
  is_active    INTEGER      NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),

  row_version  INTEGER      NOT NULL DEFAULT 1,
  created_at   VARCHAR(32)  NOT NULL,
  updated_at   VARCHAR(32)  NOT NULL,

  CONSTRAINT ck_product_category_not_own_parent CHECK (parent_id IS NULL OR parent_id <> id)
);

CREATE UNIQUE INDEX ux_product_categories_code ON product_categories (company_id, code);
CREATE INDEX ix_product_categories_parent ON product_categories (parent_id);
CREATE INDEX ix_product_categories_path ON product_categories (company_id, path);

-- ─────────────────────────────────────────────────────────────────────────────
-- attributes — the global dictionary (§A.3)
-- ─────────────────────────────────────────────────────────────────────────────
--
-- "Colour" is defined once and reused by every product that has one. What it MEANS for a given
-- product — a variant dimension or a specification — is not stored here; see product_attributes
-- below. That split is §A.2 and it is the difference between 12 SKUs and 72.

CREATE TABLE attributes (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  company_id   CHAR(36)     NOT NULL REFERENCES companies(id),

  code         VARCHAR(40)  NOT NULL,
  name         VARCHAR(200) NOT NULL,
  name_key     VARCHAR(120),

  -- How a value is stored and rendered. `list` means the value must be one of
  -- attribute_values below; the rest are free entry, typed for sorting and filtering.
  value_type   VARCHAR(20)  NOT NULL DEFAULT 'list'
               CHECK (value_type IN ('list', 'text', 'number', 'boolean', 'date')),

  -- Only a `list` attribute can define variants: a Cartesian product needs an enumerable set,
  -- and "every possible number" is not one. Enforced in the domain, which can say so in a
  -- sentence; the CHECK here would need a join.
  sort_order   INTEGER      NOT NULL DEFAULT 0,
  is_active    INTEGER      NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),

  row_version  INTEGER      NOT NULL DEFAULT 1,
  created_at   VARCHAR(32)  NOT NULL,
  updated_at   VARCHAR(32)  NOT NULL
);

CREATE UNIQUE INDEX ux_attributes_code ON attributes (company_id, code);

CREATE TABLE attribute_values (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  attribute_id CHAR(36)     NOT NULL REFERENCES attributes(id) ON DELETE CASCADE,

  code         VARCHAR(40)  NOT NULL,
  name         VARCHAR(200) NOT NULL,
  name_key     VARCHAR(120),

  -- Optional presentation hint: a hex colour, a swatch. The catalog stores it; nothing in the
  -- domain interprets it.
  display_hint VARCHAR(40),

  sort_order   INTEGER      NOT NULL DEFAULT 0,
  is_active    INTEGER      NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),

  row_version  INTEGER      NOT NULL DEFAULT 1,
  created_at   VARCHAR(32)  NOT NULL,
  updated_at   VARCHAR(32)  NOT NULL
);

CREATE UNIQUE INDEX ux_attribute_values_code ON attribute_values (attribute_id, code);

-- ─────────────────────────────────────────────────────────────────────────────
-- products
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE products (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  company_id   CHAR(36)     NOT NULL REFERENCES companies(id),

  code         VARCHAR(40)  NOT NULL,
  name         VARCHAR(200) NOT NULL,
  name_key     VARCHAR(120),
  description  TEXT,

  category_id  CHAR(36)     REFERENCES product_categories(id),

  -- goods are stocked and costed; a service is neither. The distinction drives whether Phase 4
  -- creates stock rows at all, so it is a column rather than a flag pair that can contradict
  -- itself.
  product_type VARCHAR(20)  NOT NULL DEFAULT 'goods'
               CHECK (product_type IN ('goods', 'service')),

  -- THREE UNITS (§B.3). Buy cable in rolls, stock it in metres, sell it in metres — the thing
  -- that makes real trade work and that a single-uom schema cannot express without a hundred
  -- conversion hacks at the document line.
  --
  -- All three must belong to ONE category, enforced in the domain: converting a purchase in
  -- rolls into a stock movement in kilograms is not arithmetic, it is a mistake.
  stock_uom_id    CHAR(36)  NOT NULL REFERENCES units_of_measure(id),
  sales_uom_id    CHAR(36)  NOT NULL REFERENCES units_of_measure(id),
  purchase_uom_id CHAR(36)  NOT NULL REFERENCES units_of_measure(id),

  -- Whether stock is tracked at all, and how finely. `none` is a service or a consumable
  -- nobody counts; `lot` and `serial` are Phase 4's concern and are declared here so that
  -- turning one on later is a data change rather than a migration.
  tracking     VARCHAR(20)  NOT NULL DEFAULT 'quantity'
               CHECK (tracking IN ('none', 'quantity', 'lot', 'serial')),

  -- Tax treatment. NULL means "whatever the company's default group says" — resolution is
  -- §19.3's eight-level priority, already built in Phase 2, and the catalog only supplies one
  -- of its candidates.
  tax_group_id CHAR(36)     REFERENCES tax_groups(id),

  -- Account overrides, most specific wins over the category's, which wins over the company's.
  income_account_id     CHAR(36) REFERENCES accounts(id),
  expense_account_id    CHAR(36) REFERENCES accounts(id),
  inventory_account_id  CHAR(36) REFERENCES accounts(id),

  -- Set the moment the first variant of this product is referenced by anything that has moved
  -- or been documented. Phase 4 sets it; the domain reads it to refuse a stock_uom change,
  -- because changing the unit stock is held in silently restates every historical quantity and
  -- every cost derived from one.
  has_history  INTEGER      NOT NULL DEFAULT 0 CHECK (has_history IN (0, 1)),

  is_active    INTEGER      NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  is_sold      INTEGER      NOT NULL DEFAULT 1 CHECK (is_sold IN (0, 1)),
  is_purchased INTEGER      NOT NULL DEFAULT 1 CHECK (is_purchased IN (0, 1)),

  row_version  INTEGER      NOT NULL DEFAULT 1,
  created_at   VARCHAR(32)  NOT NULL,
  updated_at   VARCHAR(32)  NOT NULL
);

CREATE UNIQUE INDEX ux_products_code ON products (company_id, code);
CREATE INDEX ix_products_category ON products (company_id, category_id);
CREATE INDEX ix_products_active ON products (company_id, is_active);

-- ─────────────────────────────────────────────────────────────────────────────
-- product_attributes — what an attribute MEANS for this product (§A.2)
-- ─────────────────────────────────────────────────────────────────────────────
--
-- The flag lives here and not on the attribute, because material defines variants for a sofa
-- and is a specification for a screwdriver. Putting it on the attribute would force a business
-- selling both to define "material" twice.

CREATE TABLE product_attributes (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  product_id   CHAR(36)     NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  attribute_id CHAR(36)     NOT NULL REFERENCES attributes(id),

  -- 1: every value combination becomes a variant. 0: it is a specification shown on the
  -- product, filtered on, and printed — but it multiplies nothing.
  is_variant_defining INTEGER NOT NULL DEFAULT 0
               CHECK (is_variant_defining IN (0, 1)),

  sort_order   INTEGER      NOT NULL DEFAULT 0,

  row_version  INTEGER      NOT NULL DEFAULT 1,
  created_at   VARCHAR(32)  NOT NULL,
  updated_at   VARCHAR(32)  NOT NULL
);

CREATE UNIQUE INDEX ux_product_attributes ON product_attributes (product_id, attribute_id);

-- Which values of that attribute this product actually offers. A shirt comes in three of the
-- dictionary's twenty colours; without this the Cartesian product would use all twenty.
CREATE TABLE product_attribute_values (
  id                   CHAR(36) NOT NULL PRIMARY KEY,
  product_attribute_id CHAR(36) NOT NULL
                       REFERENCES product_attributes(id) ON DELETE CASCADE,
  attribute_value_id   CHAR(36) NOT NULL REFERENCES attribute_values(id),

  -- For a descriptive attribute whose value_type is not `list`, the value is free text stored
  -- here instead of pointing at the dictionary. A variant-defining attribute never uses this —
  -- the domain refuses, because a Cartesian product needs an enumerable set.
  raw_value            VARCHAR(400),

  sort_order           INTEGER  NOT NULL DEFAULT 0,
  created_at           VARCHAR(32) NOT NULL
);

CREATE UNIQUE INDEX ux_product_attribute_values
  ON product_attribute_values (product_attribute_id, attribute_value_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- product_variants — the row everything downstream points at
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE product_variants (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  product_id   CHAR(36)     NOT NULL REFERENCES products(id) ON DELETE CASCADE,

  -- The SKU is what a warehouse, a supplier, and a stock count all say out loud. Unique per
  -- company, and generated from the product code plus the value codes when the user does not
  -- supply one ("SHIRT-RED-L").
  sku          VARCHAR(60)  NOT NULL,
  -- Overrides the product's name when set. A simple product's default variant leaves it empty
  -- and inherits, which is why a bag of cement never shows a variant anywhere.
  name         VARCHAR(200),

  -- THE DEFAULT VARIANT (§A.1).
  --
  -- Exactly one per product, guaranteed by ux_variant_one_default below. For a simple product
  -- it is the only one and the UI never mentions it; for a product with real variants it is
  -- the one a document line defaults to.
  is_default   INTEGER      NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),

  -- The combination this variant represents, as a sorted, canonical string of value codes:
  -- 'COLOUR:RED|SIZE:L'. Empty for a default variant with no dimensions.
  --
  -- Denormalised deliberately, like accounts.path: generation diffs what should exist against
  -- what does, and doing that by comparing one string per variant is a single indexed scan
  -- instead of a join per candidate over product_variant_values. The join table below remains
  -- the source of truth; this is the key generation matches on.
  combination  VARCHAR(400) NOT NULL DEFAULT '',

  barcode_hint VARCHAR(60),

  -- Set when this variant appears on any document or movement. Phase 4 and 5 set it; the
  -- domain reads it to refuse deletion, because deleting a variant that appears on a two-year
  -- old invoice breaks document reprinting and every historical report (§A.4).
  has_history  INTEGER      NOT NULL DEFAULT 0 CHECK (has_history IN (0, 1)),

  is_active    INTEGER      NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),

  row_version  INTEGER      NOT NULL DEFAULT 1,
  created_at   VARCHAR(32)  NOT NULL,
  updated_at   VARCHAR(32)  NOT NULL
);

CREATE UNIQUE INDEX ux_product_variants_sku ON product_variants (sku);
CREATE UNIQUE INDEX ux_product_variants_combination
  ON product_variants (product_id, combination);

-- EXACTLY ONE DEFAULT PER PRODUCT.
--
-- The same partial-unique-index shape 0015 uses for a category's reference unit, and for the
-- same reason: "there is always exactly one" is a claim the database can keep, and a claim only
-- application code keeps is one a bad import breaks quietly.
CREATE UNIQUE INDEX ux_variant_one_default
  ON product_variants (product_id) WHERE is_default = 1;

CREATE INDEX ix_product_variants_product ON product_variants (product_id, is_active);

-- Which attribute values this variant is. Source of truth for `combination` above.
CREATE TABLE product_variant_values (
  id                 CHAR(36) NOT NULL PRIMARY KEY,
  variant_id         CHAR(36) NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
  attribute_id       CHAR(36) NOT NULL REFERENCES attributes(id),
  attribute_value_id CHAR(36) NOT NULL REFERENCES attribute_values(id),

  created_at         VARCHAR(32) NOT NULL
);

CREATE UNIQUE INDEX ux_product_variant_values ON product_variant_values (variant_id, attribute_id);
CREATE INDEX ix_product_variant_values_value ON product_variant_values (attribute_value_id);
