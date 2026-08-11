-- 0017_variants — exclusions, barcodes, packagings (§A.4, §A.5, §B.5).
--
-- Owned by the catalog module, which owns 0015 and 0016.

-- ─────────────────────────────────────────────────────────────────────────────
-- variant_exclusions — combinations that are never made
-- ─────────────────────────────────────────────────────────────────────────────
--
-- A shirt in four colours and five sizes is twenty variants, but the manufacturer does not make
-- red in XXL. Without exclusions the choice is to generate the variant and let it sit at zero
-- stock forever — polluting every picker, every report, and every stock count — or to drop the
-- whole attribute.
--
-- An exclusion is a PARTIAL combination: 'COLOUR:RED|SIZE:XXL' excludes exactly that one, while
-- 'COLOUR:RED' excludes every red variant regardless of size. Matching is by prefix-free subset,
-- done in the domain, because the rule "every listed pair must be present" is a sentence and the
-- SQL for it is not.

CREATE TABLE variant_exclusions (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  product_id   CHAR(36)     NOT NULL REFERENCES products(id) ON DELETE CASCADE,

  -- Canonical, sorted, the same form product_variants.combination uses:
  -- 'COLOUR:RED|SIZE:XXL'. One form, one comparison.
  combination  VARCHAR(400) NOT NULL,
  reason       VARCHAR(200),

  row_version  INTEGER      NOT NULL DEFAULT 1,
  created_at   VARCHAR(32)  NOT NULL,
  updated_at   VARCHAR(32)  NOT NULL
);

CREATE UNIQUE INDEX ux_variant_exclusions ON variant_exclusions (product_id, combination);

-- ─────────────────────────────────────────────────────────────────────────────
-- product_packagings — "a box of 12" (§B.5)
-- ─────────────────────────────────────────────────────────────────────────────
--
-- NOT a unit of measure, and this is the distinction the whole design rests on: a kilogram is a
-- kilogram for every product in the world, but a box is 12 for one product and 24 for another.
-- Modelling "box of 12" as a UoM produces hundreds of near-duplicate units, breaks conversion
-- (there is no factor from "box" to "box"), and makes the unit dictionary a per-product mess.
--
-- So a packaging belongs to the PRODUCT and carries a quantity in that product's stock unit.

CREATE TABLE product_packagings (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  product_id   CHAR(36)     NOT NULL REFERENCES products(id) ON DELETE CASCADE,

  code         VARCHAR(40)  NOT NULL,
  name         VARCHAR(200) NOT NULL,
  name_key     VARCHAR(120),

  -- How many of the product's STOCK unit this packaging holds, ×10⁶ (the quantity scale used
  -- everywhere). A box of 12 pieces is 12000000.
  --
  -- Stored against the stock unit rather than the sales unit deliberately: stock is the one
  -- quantity every other is derived from, and a packaging defined against a unit that can still
  -- change would silently restate itself.
  quantity_micro INTEGER    NOT NULL CHECK (quantity_micro > 0),

  is_default   INTEGER      NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
  is_active    INTEGER      NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),

  row_version  INTEGER      NOT NULL DEFAULT 1,
  created_at   VARCHAR(32)  NOT NULL,
  updated_at   VARCHAR(32)  NOT NULL
);

CREATE UNIQUE INDEX ux_product_packagings_code ON product_packagings (product_id, code);
CREATE INDEX ix_product_packagings_product ON product_packagings (product_id, is_active);

-- ─────────────────────────────────────────────────────────────────────────────
-- barcodes
-- ─────────────────────────────────────────────────────────────────────────────
--
-- A SCAN MUST RESOLVE TO EXACTLY ONE THING.
--
-- That is the entire requirement, and it is why `code` is globally unique rather than unique
-- per product or per company. A till that scans a barcode and finds two variants has no correct
-- behaviour available to it: ask the cashier to choose, in front of a queue, on the basis of
-- data that should never have been enterable.
--
-- A barcode points at a variant, and OPTIONALLY at a packaging of it. Scanning the box barcode
-- at the till adds 12; scanning the unit barcode adds 1. That is §A.5's "a packaging barcode
-- resolves to a quantity", and it needs no second table.

CREATE TABLE barcodes (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  variant_id   CHAR(36)     NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
  packaging_id CHAR(36)     REFERENCES product_packagings(id) ON DELETE CASCADE,

  code         VARCHAR(60)  NOT NULL,
  -- ean13|upca|code128|qr|internal. Recorded rather than validated: a business with a supplier
  -- whose "EAN-13" has a wrong check digit still has to sell the item, and refusing the scan
  -- helps nobody. Check-digit validation belongs in the UI as a warning.
  barcode_type VARCHAR(20)  NOT NULL DEFAULT 'internal',

  -- The one printed on a label when this variant needs a barcode. One per variant, kept by the
  -- partial index below.
  is_primary   INTEGER      NOT NULL DEFAULT 0 CHECK (is_primary IN (0, 1)),
  is_active    INTEGER      NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),

  row_version  INTEGER      NOT NULL DEFAULT 1,
  created_at   VARCHAR(32)  NOT NULL,
  updated_at   VARCHAR(32)  NOT NULL
);

-- GLOBALLY unique. See above: a scan resolving to two things has no correct behaviour.
CREATE UNIQUE INDEX ux_barcodes_code ON barcodes (code);
CREATE INDEX ix_barcodes_variant ON barcodes (variant_id, is_active);

-- At most one primary per variant-and-packaging: the unit barcode and the box barcode are both
-- primary for their own scale, which is what a label printer needs.
CREATE UNIQUE INDEX ux_barcodes_one_primary
  ON barcodes (variant_id, COALESCE(packaging_id, '')) WHERE is_primary = 1;
