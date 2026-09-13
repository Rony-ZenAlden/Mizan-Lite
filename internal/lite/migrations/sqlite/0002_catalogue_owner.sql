-- 0002_catalogue_owner.sql — Phase L1: currencies, units, products, and the owner.
--
-- Design: docs/mizan_lite/phases/L1_CATALOGUE.md §3. One migration for the phase (DESIGN §13.3 A3).

-- ─────────────────────────────────────────────────────────────────────────────
-- Currencies. Codes are data (DESIGN C8), seeded here because a product cannot be priced
-- without one and the set is fixed for v1.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE currencies (
  code            CHAR(3)      NOT NULL PRIMARY KEY,
  decimal_places  SMALLINT     NOT NULL CHECK (decimal_places BETWEEN 0 AND 4),
  sort_order      SMALLINT     NOT NULL
);

INSERT INTO currencies (code, decimal_places, sort_order) VALUES ('SYP', 0, 1);
INSERT INTO currencies (code, decimal_places, sort_order) VALUES ('USD', 2, 2);

-- ─────────────────────────────────────────────────────────────────────────────
-- Units. Display names live in the catalogs under "uom.<code>". Nine, per the owner (Q-L1.5).
-- input_decimals limits what a person may type; storage is always 10⁻⁶.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE uoms (
  code            VARCHAR(16)  NOT NULL PRIMARY KEY,
  kind            VARCHAR(8)   NOT NULL CHECK (kind IN ('mass', 'volume', 'count')),
  input_decimals  SMALLINT     NOT NULL CHECK (input_decimals BETWEEN 0 AND 6),
  sort_order      SMALLINT     NOT NULL,
  CONSTRAINT ck_uom_count_is_whole CHECK (kind <> 'count' OR input_decimals = 0)
);

INSERT INTO uoms (code, kind, input_decimals, sort_order) VALUES ('kg', 'mass', 3, 1);
INSERT INTO uoms (code, kind, input_decimals, sort_order) VALUES ('l', 'volume', 3, 2);
INSERT INTO uoms (code, kind, input_decimals, sort_order) VALUES ('piece', 'count', 0, 3);
INSERT INTO uoms (code, kind, input_decimals, sort_order) VALUES ('jar', 'count', 0, 4);
INSERT INTO uoms (code, kind, input_decimals, sort_order) VALUES ('container', 'count', 0, 5);
INSERT INTO uoms (code, kind, input_decimals, sort_order) VALUES ('tin', 'count', 0, 6);
INSERT INTO uoms (code, kind, input_decimals, sort_order) VALUES ('bag', 'count', 0, 7);
INSERT INTO uoms (code, kind, input_decimals, sort_order) VALUES ('bottle', 'count', 0, 8);
INSERT INTO uoms (code, kind, input_decimals, sort_order) VALUES ('box', 'count', 0, 9);

-- ─────────────────────────────────────────────────────────────────────────────
-- Products. No stock or cost columns: L2 adds them in the phase that writes them.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE products (
  id                 CHAR(36)     NOT NULL PRIMARY KEY,
  name_ar            VARCHAR(200) NOT NULL,
  name_en            VARCHAR(200),
  -- The normalised Arabic name (textkey.Normalise). UNIQUE across active and inactive
  -- products, so one product cannot exist under three spellings (L1 §5, D-L1.2).
  name_key           VARCHAR(200) NOT NULL,
  -- Normalised Arabic name, English name and barcode: the one column search reads.
  search_text        VARCHAR(500) NOT NULL,
  barcode            VARCHAR(64),
  uom_code           VARCHAR(16)  NOT NULL REFERENCES uoms(code),
  price_currency     CHAR(3)      NOT NULL REFERENCES currencies(code),
  sell_price_micro   BIGINT       NOT NULL CHECK (sell_price_micro >= 0),   -- 10⁻⁶ of the major unit
  quick_slot         SMALLINT     CHECK (quick_slot BETWEEN 1 AND 24),       -- Q-L1.6
  is_active          SMALLINT     NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  row_version        BIGINT       NOT NULL DEFAULT 1,
  created_at         CHAR(24)     NOT NULL,
  updated_at         CHAR(24)     NOT NULL,
  CONSTRAINT ux_products_name_key   UNIQUE (name_key),
  CONSTRAINT ux_products_barcode    UNIQUE (barcode),
  CONSTRAINT ux_products_quick_slot UNIQUE (quick_slot),
  CONSTRAINT ck_products_name_present CHECK (name_ar <> '' AND name_key <> ''),
  -- A deactivated product cannot hold a till button: it would be a button that sells nothing.
  CONSTRAINT ck_products_slot_needs_active CHECK (quick_slot IS NULL OR is_active = 1)
);

CREATE INDEX ix_products_active_name ON products (is_active, name_key);

-- ─────────────────────────────────────────────────────────────────────────────
-- The owner: exactly one credential row (L1 §7.2).
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE owner_credentials (
  singleton          SMALLINT     NOT NULL PRIMARY KEY CHECK (singleton = 1),
  pin_hash           VARCHAR(200) NOT NULL,     -- PHC-encoded Argon2id (platform/crypto)
  recovery_hash      VARCHAR(200) NOT NULL,
  failed_attempts    SMALLINT     NOT NULL DEFAULT 0 CHECK (failed_attempts >= 0),
  locked_until       CHAR(24),                  -- NULL = not locked
  created_at         CHAR(24)     NOT NULL,
  updated_at         CHAR(24)     NOT NULL,
  row_version        BIGINT       NOT NULL DEFAULT 1
);

-- LEDGER. Append-only: what the owner's PIN was used for, and every attempt that failed.
-- Never a credential, a hash, or a recovery code (Mizan 5.7's rule for audit trails).
CREATE TABLE owner_events (
  id            CHAR(36)     NOT NULL PRIMARY KEY,
  occurred_at   CHAR(24)     NOT NULL,
  kind          VARCHAR(24)  NOT NULL CHECK (kind IN (
                  'pin_set', 'pin_changed', 'elevated', 'elevation_failed',
                  'locked_out', 'recovered', 'elevation_ended', 'guarded_act')),
  action        VARCHAR(64),
  subject_id    CHAR(36),
  before_value  VARCHAR(200),
  after_value   VARCHAR(200),
  CONSTRAINT ck_owner_events_act_complete CHECK ((kind = 'guarded_act') = (action IS NOT NULL))
);

CREATE INDEX ix_owner_events_occurred ON owner_events (occurred_at);
