-- 0003_stock.sql — Phase L2: stock levels, the stock ledger, and product packages.
--
-- Design: docs/mizan_lite/phases/L2_STOCK.md §4, with the owner's answers in §12.3. Verified against SQLite before
-- the design was presented, including every NULL case of every CHECK (a CHECK that evaluates to NULL passes).

-- One row per product that has ever moved. Owned by the stock module (A-L2.1).
CREATE TABLE stock_levels (
  product_id               CHAR(36)  NOT NULL PRIMARY KEY REFERENCES products(id),
  on_hand_micro            BIGINT    NOT NULL,
  avg_cost_usd_micro       BIGINT    NOT NULL CHECK (avg_cost_usd_micro >= 0),
  -- The newest movement: what a receipt reversal checks it is (§5.5), and where the verifier's chain ends.
  last_movement_id         CHAR(36)  NOT NULL REFERENCES stock_ledger(id),
  row_version              BIGINT    NOT NULL DEFAULT 1,
  updated_at               CHAR(24)  NOT NULL
);

-- LEDGER. Append-only. The only permitted writes are INSERTs (a test scans the store's SQL).
CREATE TABLE stock_ledger (
  id                         CHAR(36)    NOT NULL PRIMARY KEY,
  product_id                 CHAR(36)    NOT NULL REFERENCES products(id),
  -- The movement's place in its product's ledger: 1, 2, 3 … with no gaps. The chain is walked in this order, never
  -- by occurred_at — a shop PC's clock set back an hour would otherwise reorder history (L2 implementation, D-L2.i1).
  seq                        BIGINT      NOT NULL CHECK (seq >= 1),
  business_date              CHAR(10)    NOT NULL,     -- the shop's local day (§3.7)
  occurred_at                CHAR(24)    NOT NULL,     -- UTC
  kind                       VARCHAR(20) NOT NULL CHECK (kind IN (
                               'opening', 'receipt', 'receipt_reversal', 'count', 'adjustment',
                               'package_out', 'content_in', 'cost_correction')),
  quantity_micro             BIGINT      NOT NULL,     -- signed
  unit_cost_usd_micro        BIGINT      NOT NULL CHECK (unit_cost_usd_micro >= 0),

  -- A-L2.2: the state before AND after, so a reversal is exact and the verifier can walk the chain.
  on_hand_before_micro       BIGINT      NOT NULL,
  avg_cost_before_usd_micro  BIGINT      NOT NULL CHECK (avg_cost_before_usd_micro >= 0),
  on_hand_after_micro        BIGINT      NOT NULL,
  avg_cost_after_usd_micro   BIGINT      NOT NULL CHECK (avg_cost_after_usd_micro >= 0),

  -- What was typed on a receipt or opening: the currency, the unit cost in it, and (for pounds) the rate (§5.1).
  entered_currency           CHAR(3)     REFERENCES currencies(code),
  entered_unit_cost_micro    BIGINT,
  local_per_usd_nano         BIGINT,

  reason_code                VARCHAR(16),              -- counts and adjustments (Q-L2.6)
  note                       VARCHAR(200),
  reverses_id                CHAR(36)    REFERENCES stock_ledger(id),
  pair_id                    CHAR(36),                 -- links a package_out to its content_in
  created_at                 CHAR(24)    NOT NULL,

  CONSTRAINT ck_ledger_quantity_by_kind CHECK (
    (kind IN ('opening', 'receipt', 'content_in')     AND quantity_micro > 0) OR
    (kind IN ('receipt_reversal', 'package_out')      AND quantity_micro < 0) OR
    (kind = 'adjustment'                              AND quantity_micro <> 0) OR
    -- A count may confirm the shelf (zero); a cost correction moves value, never quantity (H3).
    (kind = 'count') OR
    (kind = 'cost_correction'                         AND quantity_micro = 0)),
  CONSTRAINT ck_ledger_after_follows_before CHECK (on_hand_after_micro = on_hand_before_micro + quantity_micro),
  CONSTRAINT ck_ledger_reason_where_needed CHECK (
    (kind IN ('count', 'adjustment')) = (reason_code IS NOT NULL)),
  CONSTRAINT ck_ledger_reason_known CHECK (
    reason_code IS NULL OR reason_code IN ('count', 'damaged', 'expired', 'own_use', 'gift', 'other')),
  CONSTRAINT ck_ledger_other_needs_note CHECK (reason_code IS NULL OR reason_code <> 'other' OR note IS NOT NULL),
  CONSTRAINT ck_ledger_reversal_links CHECK ((kind = 'receipt_reversal') = (reverses_id IS NOT NULL)),
  CONSTRAINT ck_ledger_package_pairs CHECK ((kind IN ('package_out', 'content_in')) = (pair_id IS NOT NULL)),
  -- Every comparison guarded with IS NOT NULL: in SQL, NULL > 0 is NULL, and a CHECK that evaluates to NULL PASSES.
  -- The first draft of this constraint accepted a pound receipt with no rate for exactly that reason (§4 notes).
  CONSTRAINT ck_ledger_entered_cost CHECK (
    (entered_currency IS NULL AND entered_unit_cost_micro IS NULL AND local_per_usd_nano IS NULL) OR
    (kind IN ('opening', 'receipt') AND entered_unit_cost_micro IS NOT NULL AND entered_unit_cost_micro >= 0 AND (
       (entered_currency IS NOT NULL AND entered_currency = 'USD' AND local_per_usd_nano IS NULL) OR
       (entered_currency IS NOT NULL AND entered_currency <> 'USD' AND
        local_per_usd_nano IS NOT NULL AND local_per_usd_nano > 0)))),
  CONSTRAINT ux_ledger_reversed_once UNIQUE (reverses_id),
  -- Two acts on one product that both read the same level cannot both append the next place.
  CONSTRAINT ux_ledger_product_seq UNIQUE (product_id, seq)
);
CREATE INDEX ix_stock_ledger_date    ON stock_ledger (business_date);

-- A package and what it opens into. Owned by the catalogue: it is a fact about two products.
CREATE TABLE product_packages (
  package_product_id       CHAR(36)  NOT NULL PRIMARY KEY REFERENCES products(id),
  content_product_id       CHAR(36)  NOT NULL REFERENCES products(id),
  content_quantity_micro   BIGINT    NOT NULL CHECK (content_quantity_micro > 0),  -- per package, in the content's unit
  row_version              BIGINT    NOT NULL DEFAULT 1,
  updated_at               CHAR(24)  NOT NULL,
  CONSTRAINT ck_package_not_itself CHECK (package_product_id <> content_product_id)
);
CREATE INDEX ix_product_packages_content ON product_packages (content_product_id);
