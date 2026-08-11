-- 0019_pricing — price lists and resolution (§2.6, D1).
--
-- Owned by the pricing module (§10.3). Partner owns 0018, so pricing owns 0019.
--
-- # Two departures from the §2.6 sketch, both for the same reason
--
-- The design doc listed five resolution levels, the last two being "the variant's own price"
-- and "the product's price". Those would be COLUMNS on `product_variants` and `products` —
-- tables the catalog module owns. A module adding a column to another module's table breaks the
-- ownership rule that makes a modular monolith hold together for ten years, and the alternative
-- (catalog carrying a price column for a feature it knows nothing about) is worse.
--
-- So a price_list_item keys on EITHER a variant or a product. A product-level item prices every
-- variant of it; a variant-level item overrides that for one. The five levels become three
-- lists × two granularities, which is the same expressive power, one mechanism instead of two,
-- and no column crossing a module boundary.
--
-- Likewise the partner's list: 0018 said the column would be added here. It is a table instead,
-- for the same reason. The join a one-to-one costs is a smaller price than a module reaching
-- into another module's schema.

-- ─────────────────────────────────────────────────────────────────────────────
-- price_lists
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE price_lists (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  company_id   CHAR(36)     NOT NULL REFERENCES companies(id),

  code         VARCHAR(40)  NOT NULL,
  name         VARCHAR(200) NOT NULL,
  name_key     VARCHAR(120),

  -- The currency the prices in this list are stated in. A wholesale list in USD alongside a
  -- retail list in SYP is ordinary in the countries §1 serves, and §18 already owns the
  -- conversion; this only records which currency the numbers ARE.
  currency_code CHAR(3)     NOT NULL REFERENCES currencies(code),

  -- The company default: the list that answers when nothing more specific does. Exactly one per
  -- company, kept by the partial index below — resolution's last stop must exist and must be
  -- unambiguous, or a sale has no price and no explanation.
  is_default   INTEGER      NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),

  -- Sales or purchases. A supplier's price list and a customer's are both price lists, and
  -- separating them by table would duplicate every column and both resolution functions.
  direction    VARCHAR(10)  NOT NULL DEFAULT 'sale'
               CHECK (direction IN ('sale', 'purchase')),

  -- A seasonal list. NULL at either end means open — a list with no dates is simply always in
  -- force, which is what a shop with one price list wants and never has to think about.
  valid_from   VARCHAR(10),
  valid_to     VARCHAR(10),

  is_active    INTEGER      NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),

  row_version  INTEGER      NOT NULL DEFAULT 1,
  created_at   VARCHAR(32)  NOT NULL,
  updated_at   VARCHAR(32)  NOT NULL,

  CONSTRAINT ck_price_list_dates CHECK (valid_to IS NULL OR valid_from IS NULL
                                        OR valid_to >= valid_from)
);

CREATE UNIQUE INDEX ux_price_lists_code ON price_lists (company_id, code);

-- EXACTLY ONE DEFAULT PER COMPANY AND DIRECTION.
--
-- The same partial-unique-index shape 0015's reference unit, 0016's default variant, and 0018's
-- default address use. Resolution's last stop must be unambiguous: two defaults would mean a
-- price that depends on which row the database returned first.
CREATE UNIQUE INDEX ux_price_lists_one_default
  ON price_lists (company_id, direction) WHERE is_default = 1;

-- ─────────────────────────────────────────────────────────────────────────────
-- price_list_items
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE price_list_items (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  price_list_id CHAR(36)    NOT NULL REFERENCES price_lists(id) ON DELETE CASCADE,

  -- EXACTLY ONE of these is set. A product-level item prices every variant of that product; a
  -- variant-level item overrides it for one. That is how "XXL costs more but every colour costs
  -- the same" is expressed in two rows rather than twenty.
  product_id    CHAR(36)    REFERENCES products(id) ON DELETE CASCADE,
  variant_id    CHAR(36)    REFERENCES product_variants(id) ON DELETE CASCADE,

  -- Minor units of the LIST's currency, as a string of digits everywhere it crosses a boundary
  -- (§17). Never a float.
  price_minor   INTEGER     NOT NULL CHECK (price_minor >= 0),

  -- The quantity break: this price applies from this quantity upward, ×10⁶. 0 is the base
  -- price. "Ten or more, cheaper" needs no second concept and no second table — resolution
  -- picks the highest break the line qualifies for.
  min_quantity_micro INTEGER NOT NULL DEFAULT 0 CHECK (min_quantity_micro >= 0),

  is_active     INTEGER     NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),

  row_version   INTEGER     NOT NULL DEFAULT 1,
  created_at    VARCHAR(32) NOT NULL,
  updated_at    VARCHAR(32) NOT NULL,

  -- A row pricing neither, or both, is unresolvable: the first prices nothing, and the second
  -- would have two answers with no rule to choose between them.
  CONSTRAINT ck_price_item_targets_one CHECK (
    (product_id IS NOT NULL AND variant_id IS NULL) OR
    (product_id IS NULL AND variant_id IS NOT NULL)
  )
);

-- One price per target per quantity break. Two rows for the same variant at the same break
-- would make the price depend on row order.
CREATE UNIQUE INDEX ux_price_list_items_variant
  ON price_list_items (price_list_id, variant_id, min_quantity_micro)
  WHERE variant_id IS NOT NULL;
CREATE UNIQUE INDEX ux_price_list_items_product
  ON price_list_items (price_list_id, product_id, min_quantity_micro)
  WHERE product_id IS NOT NULL;

CREATE INDEX ix_price_list_items_lookup
  ON price_list_items (price_list_id, is_active);

-- ─────────────────────────────────────────────────────────────────────────────
-- assignments
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Who gets which list. Two tables rather than columns on `partners` and `locations`, because
-- those belong to other modules — see the header.

CREATE TABLE partner_price_lists (
  partner_id    CHAR(36)    NOT NULL REFERENCES partners(id) ON DELETE CASCADE,
  price_list_id CHAR(36)    NOT NULL REFERENCES price_lists(id) ON DELETE CASCADE,
  -- A partner may hold one SALE list and one PURCHASE list: the merchant we sell brackets to at
  -- wholesale is the same merchant we buy steel from at their agreed rate.
  direction     VARCHAR(10) NOT NULL CHECK (direction IN ('sale', 'purchase')),
  created_at    VARCHAR(32) NOT NULL,

  PRIMARY KEY (partner_id, direction)
);

-- A BRANCH, not a warehouse. A price is what a shop charges; a warehouse holds stock and
-- charges nobody. Assigning a list to a warehouse would be a setting with no reader.
CREATE TABLE branch_price_lists (
  branch_id     CHAR(36)    NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  price_list_id CHAR(36)    NOT NULL REFERENCES price_lists(id) ON DELETE CASCADE,
  direction     VARCHAR(10) NOT NULL CHECK (direction IN ('sale', 'purchase')),
  created_at    VARCHAR(32) NOT NULL,

  PRIMARY KEY (branch_id, direction)
);
