-- 0015_uom — units of measure (§B).
--
-- Owned by the catalog module. Tax owns 0014, so catalog owns 0015.
--
-- # Quantities are integers scaled by 10⁶ (§7.2, §B.1)
--
-- Six decimal places covers every case the brief listed — 0.001 kg, 0.005 m, 0.25 litre — and
-- int64 at that scale reaches ~9.2 × 10¹² units, far beyond any physical inventory. Never a
-- float: the kernel exists so quantities are exact, and stock is where an error compounds
-- silently through every valuation.

CREATE TABLE uom_categories (
  id          CHAR(36)     NOT NULL PRIMARY KEY,
  code        VARCHAR(40)  NOT NULL,      -- WEIGHT | VOLUME | LENGTH | AREA | COUNT | TIME
  name        VARCHAR(120) NOT NULL,
  name_key    VARCHAR(120),
  is_system   INTEGER      NOT NULL DEFAULT 0,
  is_active   INTEGER      NOT NULL DEFAULT 1,

  created_at  CHAR(24)     NOT NULL,
  updated_at  CHAR(24)     NOT NULL,
  row_version INTEGER      NOT NULL DEFAULT 1,

  CONSTRAINT ux_uom_categories_code   UNIQUE (code),
  CONSTRAINT ck_uom_categories_system CHECK (is_system IN (0, 1)),
  CONSTRAINT ck_uom_categories_active CHECK (is_active IN (0, 1))
);

CREATE TABLE units_of_measure (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  uom_category_id     CHAR(36)     NOT NULL REFERENCES uom_categories(id),

  code                VARCHAR(20)  NOT NULL,   -- 'KG', 'G', 'M', 'CM', 'L', 'PCS'
  name                VARCHAR(80)  NOT NULL,
  name_key            VARCHAR(120),
  symbol              VARCHAR(12)  NOT NULL,   -- 'kg' — what a receipt prints

  -- The conversion factor relative to the category's reference unit, ×10⁹.
  --
  -- Nano rather than micro because a factor multiplies a quantity that is already micro-scaled,
  -- and the extra headroom is what keeps `g → tonne` exact rather than nearly exact.
  factor_to_reference BIGINT       NOT NULL,
  -- Exactly one per category, enforced by a unique partial index below. Conversion goes
  -- THROUGH it, so a category with two references — or none — makes every conversion in that
  -- category either ambiguous or impossible.
  is_reference        INTEGER      NOT NULL DEFAULT 0,

  -- §B.4. You can sell 1.5 kg of timber; you cannot sell 0.5 of a chair.
  --
  -- Per-unit rather than a global setting because one business legitimately has both, and a
  -- setting would force it to choose which half of its catalog to get wrong.
  allows_fractional   INTEGER      NOT NULL DEFAULT 1,
  -- ×10⁶. Applied ON ENTRY, so a scale reading of 1.4372381 kg becomes 1.437 kg once, at the
  -- boundary — not repeatedly and inconsistently in later calculations.
  rounding_precision  BIGINT       NOT NULL DEFAULT 1000,
  display_decimals    INTEGER      NOT NULL DEFAULT 3,

  is_system           INTEGER      NOT NULL DEFAULT 0,
  is_active           INTEGER      NOT NULL DEFAULT 1,

  created_at          CHAR(24)     NOT NULL,
  updated_at          CHAR(24)     NOT NULL,
  row_version         INTEGER      NOT NULL DEFAULT 1,

  CONSTRAINT ux_uom_code            UNIQUE (code),
  CONSTRAINT ck_uom_factor          CHECK (factor_to_reference > 0),
  CONSTRAINT ck_uom_reference       CHECK (is_reference IN (0, 1)),
  CONSTRAINT ck_uom_fractional      CHECK (allows_fractional IN (0, 1)),
  CONSTRAINT ck_uom_precision       CHECK (rounding_precision > 0),
  CONSTRAINT ck_uom_decimals        CHECK (display_decimals >= 0 AND display_decimals <= 6),
  CONSTRAINT ck_uom_system          CHECK (is_system IN (0, 1)),
  CONSTRAINT ck_uom_active          CHECK (is_active IN (0, 1)),
  -- The reference unit's factor is 1 by definition. A reference with any other factor would
  -- make every conversion in the category wrong by that factor, consistently, which is the
  -- hardest kind of wrong to notice.
  CONSTRAINT ck_uom_reference_one   CHECK (is_reference = 0 OR factor_to_reference = 1000000000)
);

CREATE INDEX ix_uom_category ON units_of_measure (uom_category_id, is_active);

-- Exactly one reference per category. A partial unique index rather than a CHECK, because the
-- rule is about a SET of rows and no row constraint can see its siblings.
CREATE UNIQUE INDEX ux_uom_one_reference
  ON units_of_measure (uom_category_id) WHERE is_reference = 1;
