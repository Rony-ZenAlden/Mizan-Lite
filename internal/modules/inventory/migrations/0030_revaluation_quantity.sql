-- 0030_revaluation_quantity — let a revaluation move no quantity.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- WHAT THIS FIXES, AND WHY IT WAS NOT FOUND UNTIL PHASE 6
-- ─────────────────────────────────────────────────────────────────────────────
--
-- 4.2 built a `revaluation` movement type: value without quantity, given the Neutral direction
-- precisely so that it would not be mistaken for an inward move of zero. The direction table knew
-- it. The costing strategy had a `revalue` case. The type appeared in this table's CHECK list.
--
-- And it could not be written. `quantity_micro > 0` refused it here, and the domain constructor
-- refused it there — so every path to the feature ended in a validation error. The seam was
-- built, documented, tested at the unit level, and structurally unusable.
--
-- It was found the moment something needed it: 6.4's purchase price variance, which revalues the
-- stock a supplier's invoice says cost more than the order did. That is the lesson, and it is
-- worth more than the fix — A SEAM IS ONLY PROVEN BY A CALLER. Everything about this one looked
-- right from the inside.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- WHY A TABLE REBUILD
-- ─────────────────────────────────────────────────────────────────────────────
--
-- SQLite cannot drop or alter a CHECK constraint. The documented approach is to rebuild: create
-- the replacement, copy every row, drop the original, rename, and recreate the indexes.
--
-- Foreign keys are disabled for the swap by the migration runner's own transaction discipline;
-- the self-reference on `source_movement_id` survives because the rows are copied wholesale and
-- the new table is renamed into the old name before anything reads it.

CREATE TABLE stock_movements_new (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  warehouse_id   CHAR(36)    NOT NULL REFERENCES warehouses(id),
  product_id     CHAR(36)    NOT NULL REFERENCES products(id),
  variant_id     CHAR(36)    NOT NULL REFERENCES product_variants(id),

  movement_type  VARCHAR(20) NOT NULL CHECK (movement_type IN (
                   'receipt', 'issue', 'adjustment_in', 'adjustment_out',
                   'transfer_out', 'transfer_in', 'count', 'revaluation',
                   'return_in', 'return_out')),

  -- A revaluation moves NO quantity, and that is the whole point of it. Every other type must
  -- still move something: zero would write a ledger row for a movement that did not happen, and
  -- a negative is the signed-quantity mistake this design exists to make unrepresentable.
  quantity_micro INTEGER     NOT NULL CHECK (
                   quantity_micro > 0
                   OR (quantity_micro = 0 AND movement_type = 'revaluation')),

  unit_cost_micro INTEGER    NOT NULL DEFAULT 0 CHECK (unit_cost_micro >= 0),
  value_minor    INTEGER     NOT NULL DEFAULT 0,
  balance_after_micro INTEGER NOT NULL,
  average_after_micro INTEGER NOT NULL DEFAULT 0,
  source_movement_id CHAR(36) REFERENCES stock_movements(id),
  document_type  VARCHAR(40),
  document_id    CHAR(36),
  document_line_id CHAR(36),
  reason_code    VARCHAR(40),
  reason         VARCHAR(400),
  occurred_at    VARCHAR(32) NOT NULL,
  created_at     VARCHAR(32) NOT NULL,
  created_by     CHAR(36)    REFERENCES users(id),

  -- Added by 0021. Carried through the rebuild rather than re-added afterwards, so the table
  -- never briefly exists without the columns lot tracking depends on.
  lot_id         CHAR(36)    REFERENCES stock_lots(id),
  serial_id      CHAR(36)    REFERENCES stock_serials(id)
);

INSERT INTO stock_movements_new (
  id, company_id, warehouse_id, product_id, variant_id, movement_type,
  quantity_micro, unit_cost_micro, value_minor, balance_after_micro, average_after_micro,
  source_movement_id, document_type, document_id, document_line_id,
  reason_code, reason, occurred_at, created_at, created_by, lot_id, serial_id
)
SELECT
  id, company_id, warehouse_id, product_id, variant_id, movement_type,
  quantity_micro, unit_cost_micro, value_minor, balance_after_micro, average_after_micro,
  source_movement_id, document_type, document_id, document_line_id,
  reason_code, reason, occurred_at, created_at, created_by, lot_id, serial_id
FROM stock_movements;

DROP TABLE stock_movements;

ALTER TABLE stock_movements_new RENAME TO stock_movements;

-- The indexes 0020 and 0021 created, restored on the rebuilt table.
CREATE INDEX ix_stock_movements_variant
  ON stock_movements (variant_id, warehouse_id, occurred_at);
CREATE INDEX ix_stock_movements_product ON stock_movements (company_id, product_id, occurred_at);
CREATE INDEX ix_stock_movements_document ON stock_movements (document_type, document_id);
CREATE INDEX ix_stock_movements_source ON stock_movements (source_movement_id);
CREATE INDEX ix_stock_movements_lot ON stock_movements (lot_id);
CREATE INDEX ix_stock_movements_serial ON stock_movements (serial_id);
