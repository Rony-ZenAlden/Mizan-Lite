-- 0005_till.sql — Phase L4: sales, their lines, and the stock ledger rebuilt to hold them.
--
-- Design: docs/mizan_lite/phases/L4_TILL.md §6, with §14 (the owner's answers: discounts with the owner PIN). Verified
-- before implementation on a copy of the demo seeder's database, and by TestTheLedgerRebuildKeepsEveryRow through the
-- real migration runner.

-- ─────────────────────────────────────────────────────────────────────────────
-- Sales. Updated only posted → voided (a test scans the store's SQL). The receipt IS the sale: every figure a
-- customer was shown is here, and nothing on a receipt is read from another table when it is shown again.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE sales (
  id                    CHAR(36)     NOT NULL PRIMARY KEY,
  receipt_no            BIGINT       NOT NULL CHECK (receipt_no >= 1),
  business_date         CHAR(10)     NOT NULL,
  sold_at               CHAR(24)     NOT NULL,
  status                VARCHAR(8)   NOT NULL CHECK (status IN ('posted', 'voided')),
  -- credit is refused by the service until customers exist (L5); allowed here so L5 needs no rebuild (A-L4.2).
  payment               VARCHAR(8)   NOT NULL CHECK (payment IN ('cash', 'credit')),
  local_currency        CHAR(3)      NOT NULL REFERENCES currencies(code),
  -- The rate snapshot: its id, its value, and when it had been recorded.
  fx_rate_id            CHAR(36)     NOT NULL REFERENCES fx_rates(id),
  local_per_usd_nano    BIGINT       NOT NULL CHECK (local_per_usd_nano > 0),
  rate_recorded_at      CHAR(24)     NOT NULL,
  settlement_currency   CHAR(3)      NOT NULL REFERENCES currencies(code),
  -- Σ of the lines after their own discounts, in both currencies (the printed lines add up to these exactly).
  lines_local_minor     BIGINT       NOT NULL CHECK (lines_local_minor >= 0),
  lines_usd_minor       BIGINT       NOT NULL CHECK (lines_usd_minor >= 0),
  -- A discount on the whole sale, typed in the settlement currency, kept in both (L4 §14).
  discount_local_minor  BIGINT       NOT NULL CHECK (discount_local_minor >= 0),
  discount_usd_minor    BIGINT       NOT NULL CHECK (discount_usd_minor >= 0),
  cash_increment_minor  BIGINT       NOT NULL CHECK (cash_increment_minor >= 1),
  rounding_minor        BIGINT       NOT NULL,
  total_minor           BIGINT       NOT NULL CHECK (total_minor >= 0),
  tendered_currency     CHAR(3)      NOT NULL REFERENCES currencies(code),
  tendered_minor        BIGINT       NOT NULL CHECK (tendered_minor >= 0),
  change_currency       CHAR(3)      NOT NULL REFERENCES currencies(code),
  change_minor          BIGINT       NOT NULL CHECK (change_minor >= 0),
  cost_usd_minor        BIGINT       NOT NULL CHECK (cost_usd_minor >= 0),
  shop_name_snapshot    VARCHAR(100) NOT NULL,
  voided_at             CHAR(24),
  void_business_date    CHAR(10),
  void_reason           VARCHAR(200),
  created_at            CHAR(24)     NOT NULL,
  row_version           BIGINT       NOT NULL DEFAULT 1,
  CONSTRAINT ux_sales_receipt_no UNIQUE (receipt_no),
  CONSTRAINT ck_sale_local_is_not_usd CHECK (local_currency <> 'USD'),
  CONSTRAINT ck_sale_settles_in_its_two_currencies CHECK (settlement_currency IN (local_currency, 'USD')),
  CONSTRAINT ck_sale_tender_in_its_two_currencies CHECK (tendered_currency IN (local_currency, 'USD')),
  CONSTRAINT ck_sale_change_in_its_two_currencies CHECK (change_currency IN (local_currency, 'USD')),
  CONSTRAINT ck_sale_discount_within_lines CHECK (discount_local_minor <= lines_local_minor AND discount_usd_minor <= lines_usd_minor),
  CONSTRAINT ck_sale_total_is_lines_less_discount_plus_rounding CHECK (total_minor =
    CASE WHEN settlement_currency = 'USD' THEN lines_usd_minor - discount_usd_minor
         ELSE lines_local_minor - discount_local_minor END + rounding_minor),
  CONSTRAINT ck_sale_dollars_are_not_cash_rounded CHECK (settlement_currency <> 'USD' OR rounding_minor = 0),
  CONSTRAINT ck_sale_void_is_complete CHECK (
    (status = 'posted' AND voided_at IS NULL     AND void_business_date IS NULL     AND void_reason IS NULL) OR
    (status = 'voided' AND voided_at IS NOT NULL AND void_business_date IS NOT NULL AND void_reason IS NOT NULL))
);
CREATE INDEX ix_sales_business_date      ON sales (business_date, status);
CREATE INDEX ix_sales_void_business_date ON sales (void_business_date);

-- Every figure a later edit to the product could change is snapshotted. Never updated.
CREATE TABLE sale_lines (
  id                         CHAR(36)     NOT NULL PRIMARY KEY,
  sale_id                    CHAR(36)     NOT NULL REFERENCES sales(id),
  line_no                    SMALLINT     NOT NULL CHECK (line_no >= 1),
  product_id                 CHAR(36)     NOT NULL REFERENCES products(id),
  name_ar_snapshot           VARCHAR(200) NOT NULL,
  name_en_snapshot           VARCHAR(200),
  unit_code_snapshot         VARCHAR(16)  NOT NULL,
  quantity_micro             BIGINT       NOT NULL CHECK (quantity_micro > 0),
  price_currency             CHAR(3)      NOT NULL REFERENCES currencies(code),
  unit_price_micro           BIGINT       NOT NULL CHECK (unit_price_micro >= 0),
  -- The line before its discount, in both currencies from one exact product (DESIGN §4.4).
  gross_local_minor          BIGINT       NOT NULL CHECK (gross_local_minor >= 0),
  gross_usd_minor            BIGINT       NOT NULL CHECK (gross_usd_minor >= 0),
  -- A line discount is a percentage (owner PIN), applied to each currency's gross, each rounded once.
  discount_percent_micro     BIGINT       NOT NULL CHECK (discount_percent_micro BETWEEN 0 AND 1000000),
  discount_local_minor       BIGINT       NOT NULL CHECK (discount_local_minor >= 0),
  discount_usd_minor         BIGINT       NOT NULL CHECK (discount_usd_minor >= 0),
  unit_cost_usd_micro        BIGINT       NOT NULL CHECK (unit_cost_usd_micro >= 0),
  cost_known                 SMALLINT     NOT NULL CHECK (cost_known IN (0, 1)),
  cost_usd_minor             BIGINT       NOT NULL CHECK (cost_usd_minor >= 0),
  cost_local_minor           BIGINT       NOT NULL CHECK (cost_local_minor >= 0),
  CONSTRAINT ux_sale_lines_line UNIQUE (sale_id, line_no),
  CONSTRAINT ck_sale_line_discount_within_gross CHECK (discount_local_minor <= gross_local_minor AND discount_usd_minor <= gross_usd_minor),
  CONSTRAINT ck_sale_line_no_percent_no_discount CHECK (
    discount_percent_micro > 0 OR (discount_local_minor = 0 AND discount_usd_minor = 0)),
  CONSTRAINT ck_sale_line_unknown_cost_is_zero CHECK (
    cost_known = 1 OR (unit_cost_usd_micro = 0 AND cost_usd_minor = 0 AND cost_local_minor = 0))
);
CREATE INDEX ix_sale_lines_product ON sale_lines (product_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- The stock ledger, rebuilt to hold sales (L2 A-L2.3), IN DEPENDENCY ORDER (L4 A-L4.1). The runner's transaction
-- keeps foreign keys on, and they cannot be switched off inside it, so:
--   1. stock_levels, which points at the ledger, is copied aside and dropped;
--   2. the new ledger's self-reference names the NEW table — naming the old one would make its drop break the
--      reversal rows just copied; the rename below rewrites it;
--   3. every row is copied, the old ledger dropped, the new one renamed;
--   4. stock_levels is recreated exactly as 0003 created it, and copied back.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE stock_levels_copy AS SELECT * FROM stock_levels;
DROP TABLE stock_levels;

CREATE TABLE stock_ledger_new (
  id                         CHAR(36)    NOT NULL PRIMARY KEY,
  product_id                 CHAR(36)    NOT NULL REFERENCES products(id),
  seq                        BIGINT      NOT NULL CHECK (seq >= 1),
  business_date              CHAR(10)    NOT NULL,
  occurred_at                CHAR(24)    NOT NULL,
  kind                       VARCHAR(20) NOT NULL CHECK (kind IN (
                               'opening', 'receipt', 'receipt_reversal', 'count', 'adjustment',
                               'package_out', 'content_in', 'cost_correction', 'sale', 'sale_void')),
  quantity_micro             BIGINT      NOT NULL,
  unit_cost_usd_micro        BIGINT      NOT NULL CHECK (unit_cost_usd_micro >= 0),
  on_hand_before_micro       BIGINT      NOT NULL,
  avg_cost_before_usd_micro  BIGINT      NOT NULL CHECK (avg_cost_before_usd_micro >= 0),
  on_hand_after_micro        BIGINT      NOT NULL,
  avg_cost_after_usd_micro   BIGINT      NOT NULL CHECK (avg_cost_after_usd_micro >= 0),
  entered_currency           CHAR(3)     REFERENCES currencies(code),
  entered_unit_cost_micro    BIGINT,
  local_per_usd_nano         BIGINT,
  reason_code                VARCHAR(16),
  note                       VARCHAR(200),
  reverses_id                CHAR(36)    REFERENCES stock_ledger_new(id),
  pair_id                    CHAR(36),
  sale_id                    CHAR(36)    REFERENCES sales(id),
  sale_line_id               CHAR(36)    REFERENCES sale_lines(id),
  created_at                 CHAR(24)    NOT NULL,

  CONSTRAINT ck_ledger_quantity_by_kind CHECK (
    (kind IN ('opening', 'receipt', 'content_in', 'sale_void') AND quantity_micro > 0) OR
    (kind IN ('receipt_reversal', 'package_out', 'sale')      AND quantity_micro < 0) OR
    (kind = 'adjustment'                                      AND quantity_micro <> 0) OR
    (kind = 'count') OR
    (kind = 'cost_correction'                                 AND quantity_micro = 0)),
  CONSTRAINT ck_ledger_after_follows_before CHECK (on_hand_after_micro = on_hand_before_micro + quantity_micro),
  CONSTRAINT ck_ledger_reason_where_needed CHECK (
    (kind IN ('count', 'adjustment')) = (reason_code IS NOT NULL)),
  CONSTRAINT ck_ledger_reason_known CHECK (
    reason_code IS NULL OR reason_code IN ('count', 'damaged', 'expired', 'own_use', 'gift', 'other')),
  CONSTRAINT ck_ledger_other_needs_note CHECK (reason_code IS NULL OR reason_code <> 'other' OR note IS NOT NULL),
  CONSTRAINT ck_ledger_reversal_links CHECK ((kind IN ('receipt_reversal', 'sale_void')) = (reverses_id IS NOT NULL)),
  CONSTRAINT ck_ledger_package_pairs CHECK ((kind IN ('package_out', 'content_in')) = (pair_id IS NOT NULL)),
  CONSTRAINT ck_ledger_sale_links CHECK (
    (kind IN ('sale', 'sale_void')) = (sale_id IS NOT NULL AND sale_line_id IS NOT NULL)),
  CONSTRAINT ck_ledger_sale_links_both_or_neither CHECK ((sale_id IS NULL) = (sale_line_id IS NULL)),
  CONSTRAINT ck_ledger_entered_cost CHECK (
    (entered_currency IS NULL AND entered_unit_cost_micro IS NULL AND local_per_usd_nano IS NULL) OR
    (kind IN ('opening', 'receipt') AND entered_unit_cost_micro IS NOT NULL AND entered_unit_cost_micro >= 0 AND (
       (entered_currency IS NOT NULL AND entered_currency = 'USD' AND local_per_usd_nano IS NULL) OR
       (entered_currency IS NOT NULL AND entered_currency <> 'USD' AND
        local_per_usd_nano IS NOT NULL AND local_per_usd_nano > 0)))),
  CONSTRAINT ux_ledger_reversed_once UNIQUE (reverses_id),
  CONSTRAINT ux_ledger_product_seq UNIQUE (product_id, seq)
);

INSERT INTO stock_ledger_new (
  id, product_id, seq, business_date, occurred_at, kind, quantity_micro, unit_cost_usd_micro,
  on_hand_before_micro, avg_cost_before_usd_micro, on_hand_after_micro, avg_cost_after_usd_micro,
  entered_currency, entered_unit_cost_micro, local_per_usd_nano, reason_code, note, reverses_id, pair_id,
  sale_id, sale_line_id, created_at)
SELECT
  id, product_id, seq, business_date, occurred_at, kind, quantity_micro, unit_cost_usd_micro,
  on_hand_before_micro, avg_cost_before_usd_micro, on_hand_after_micro, avg_cost_after_usd_micro,
  entered_currency, entered_unit_cost_micro, local_per_usd_nano, reason_code, note, reverses_id, pair_id,
  NULL, NULL, created_at
FROM stock_ledger;

DROP TABLE stock_ledger;
ALTER TABLE stock_ledger_new RENAME TO stock_ledger;

CREATE INDEX ix_stock_ledger_date ON stock_ledger (business_date);
CREATE INDEX ix_stock_ledger_sale ON stock_ledger (sale_id);
-- One sale movement and one void movement per sale line, at most.
CREATE UNIQUE INDEX ux_stock_ledger_sale_line_kind ON stock_ledger (sale_line_id, kind);

-- stock_levels, exactly as 0003 created it.
CREATE TABLE stock_levels (
  product_id               CHAR(36)  NOT NULL PRIMARY KEY REFERENCES products(id),
  on_hand_micro            BIGINT    NOT NULL,
  avg_cost_usd_micro       BIGINT    NOT NULL CHECK (avg_cost_usd_micro >= 0),
  last_movement_id         CHAR(36)  NOT NULL REFERENCES stock_ledger(id),
  row_version              BIGINT    NOT NULL DEFAULT 1,
  updated_at               CHAR(24)  NOT NULL
);
INSERT INTO stock_levels (product_id, on_hand_micro, avg_cost_usd_micro, last_movement_id, row_version, updated_at)
  SELECT product_id, on_hand_micro, avg_cost_usd_micro, last_movement_id, row_version, updated_at FROM stock_levels_copy;
DROP TABLE stock_levels_copy;
