-- 0020_inventory — the stock ledger (§21.2).
--
-- Owned by the inventory module (§10.3). Pricing owns 0019, so inventory owns 0020.
--
-- # The shape, and why it is Phase 2's shape
--
-- `stock_movements` is an APPEND-ONLY ledger and the source of truth. `stock_levels` is a
-- maintained PROJECTION that can always be rebuilt from it and is verified against it by a job.
--
-- That is exactly what Phase 2 did with `journal_lines` and `account_balances`, and the
-- reasoning transfers unchanged: a projection that cannot be rebuilt is a number nobody can
-- defend, and one that is never checked is a number nobody should trust. Inventory has the same
-- failure mode as the ledger and worse consequences — a drift here is a gross margin that is
-- quietly wrong for months, discovered at a stock count.

-- ─────────────────────────────────────────────────────────────────────────────
-- stock_movements — append-only
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE stock_movements (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  warehouse_id   CHAR(36)    NOT NULL REFERENCES warehouses(id),

  -- Both, though variant implies product. The product id is carried so that "everything that
  -- moved for this product across its variants" is one indexed scan rather than a join —
  -- the same denormalisation, for the same reason, as accounts.path.
  product_id     CHAR(36)    NOT NULL REFERENCES products(id),
  variant_id     CHAR(36)    NOT NULL REFERENCES product_variants(id),

  movement_type  VARCHAR(20) NOT NULL CHECK (movement_type IN (
                   'receipt', 'issue', 'adjustment_in', 'adjustment_out',
                   'transfer_out', 'transfer_in', 'count', 'revaluation', 'return_in')),

  -- ALWAYS POSITIVE. The direction lives in movement_type.
  --
  -- A signed quantity makes SUM(quantity_micro) meaningful and every other query a minefield:
  -- "how much did we receive this month" becomes a filtered sum that somebody eventually writes
  -- without the filter, and the answer looks plausible.
  quantity_micro INTEGER     NOT NULL CHECK (quantity_micro > 0),

  -- The cost applied to THIS movement, per unit, in minor units x 10^6.
  --
  -- Scaled because a unit cost divides: a box of 1000 screws at 7 minor units apiece is not
  -- representable in whole minor units, and rounding each movement would drift the valuation
  -- by a little on every single receipt.
  unit_cost_micro INTEGER    NOT NULL DEFAULT 0 CHECK (unit_cost_micro >= 0),
  -- The money this movement moved: quantity x unit cost, rounded ONCE, in whole minor units.
  -- This is what reaches the general ledger, so it is stored rather than recomputed — two
  -- places that both round are two places that can disagree.
  value_minor    INTEGER     NOT NULL DEFAULT 0,

  -- The resulting on-hand, recorded at write time.
  --
  -- Storing a derivable value is normally a smell; here it is the point. It is what lets the
  -- verifier catch a drifted projection AND say where the drift began, rather than only that
  -- the total is wrong today.
  balance_after_micro INTEGER NOT NULL,
  -- The resulting average cost, likewise recorded — so a valuation as at any past date can be
  -- read from the ledger without replaying it.
  average_after_micro INTEGER NOT NULL DEFAULT 0,

  -- For a return, the issue it reverses.
  --
  -- Designed in from the start because §D.3 requires a return to be costed at its ORIGINAL
  -- issue's cost, not today's average — returning an item bought at last year's price must not
  -- create phantom profit. Retrofitting this means every historical return has no source and
  -- no correct cost.
  source_movement_id CHAR(36) REFERENCES stock_movements(id),

  -- What caused it. Phases 5 and 6 fill these; an adjustment entered by hand leaves them null.
  document_type  VARCHAR(40),
  document_id    CHAR(36),
  document_line_id CHAR(36),

  -- Free text for an adjustment, and a stable code for the reason where one exists.
  reason_code    VARCHAR(40),
  reason         VARCHAR(400),

  -- When the stock actually moved, which is not when the row was written: a delivery note
  -- entered on Monday for goods received on Friday belongs in Friday's valuation.
  occurred_at    VARCHAR(32) NOT NULL,
  created_at     VARCHAR(32) NOT NULL,
  created_by     CHAR(36)    REFERENCES users(id)

  -- NO row_version, NO updated_at. This table is append-only: there is no UPDATE to
  -- concurrency-control, and a column implying otherwise would invite one.
);

CREATE INDEX ix_stock_movements_variant
  ON stock_movements (variant_id, warehouse_id, occurred_at);
CREATE INDEX ix_stock_movements_product ON stock_movements (company_id, product_id, occurred_at);
CREATE INDEX ix_stock_movements_document ON stock_movements (document_type, document_id);
CREATE INDEX ix_stock_movements_source ON stock_movements (source_movement_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- stock_levels — the projection
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE stock_levels (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  warehouse_id   CHAR(36)    NOT NULL REFERENCES warehouses(id),
  product_id     CHAR(36)    NOT NULL REFERENCES products(id),
  variant_id     CHAR(36)    NOT NULL REFERENCES product_variants(id),

  qty_on_hand_micro  INTEGER NOT NULL DEFAULT 0,
  -- Reserved by an order that has not shipped. Written by Phase 5.
  --
  -- On the level row rather than computed from open orders, because the till needs "can I sell
  -- this" in ONE indexed read — and a sum over open order lines at every keystroke is the
  -- difference between a responsive counter and a slow one.
  qty_reserved_micro INTEGER NOT NULL DEFAULT 0 CHECK (qty_reserved_micro >= 0),

  -- qty_available is DERIVED (on_hand - reserved) and deliberately not stored: unlike
  -- balance_after above, nothing needs to know its value at a past instant, and a third
  -- quantity column is a third thing that can disagree with the other two.

  avg_cost_micro INTEGER     NOT NULL DEFAULT 0 CHECK (avg_cost_micro >= 0),

  -- The last movement folded into this row. The verifier uses it to report WHERE a drift
  -- began rather than only that one exists.
  last_movement_id CHAR(36)  REFERENCES stock_movements(id),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  updated_at     VARCHAR(32) NOT NULL
);

-- One row per variant per warehouse. Two would make "how much is on hand" depend on which the
-- query happened to find.
CREATE UNIQUE INDEX ux_stock_levels ON stock_levels (variant_id, warehouse_id);
CREATE INDEX ix_stock_levels_product ON stock_levels (company_id, product_id);
CREATE INDEX ix_stock_levels_warehouse ON stock_levels (warehouse_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- inventory_layers — written ALWAYS, read only under FIFO (§D.2)
-- ─────────────────────────────────────────────────────────────────────────────
--
-- THE KEY DECISION OF THIS PHASE.
--
-- A row per receipt, in average-cost mode exactly as in FIFO. Under WAC these rows are recorded
-- and never read for valuation. That costs one insert per receipt and buys what §33.3 asked
-- for: switching a company to FIFO is a configuration change plus a recompute, NOT a migration.
--
-- If layers were created only once FIFO was switched on, the switch would require
-- reconstructing purchase history from movements — approximate at best, and impossible once
-- opening balances are involved.

CREATE TABLE inventory_layers (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  warehouse_id   CHAR(36)    NOT NULL REFERENCES warehouses(id),
  variant_id     CHAR(36)    NOT NULL REFERENCES product_variants(id),

  source_movement_id CHAR(36) NOT NULL REFERENCES stock_movements(id),
  received_at    VARCHAR(32) NOT NULL,

  quantity_micro  INTEGER    NOT NULL CHECK (quantity_micro > 0),
  -- Decremented as the layer is consumed. Under WAC it is maintained in receipt order anyway,
  -- so that a later switch to FIFO starts from a truthful remaining-quantity picture rather
  -- than from full layers that were never drawn down.
  remaining_micro INTEGER    NOT NULL CHECK (remaining_micro >= 0),
  unit_cost_micro INTEGER    NOT NULL CHECK (unit_cost_micro >= 0),

  is_exhausted   INTEGER     NOT NULL DEFAULT 0 CHECK (is_exhausted IN (0, 1)),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  updated_at     VARCHAR(32) NOT NULL,

  -- A layer cannot have more left than it ever held.
  CONSTRAINT ck_layer_remaining CHECK (remaining_micro <= quantity_micro)
);

-- The FIFO consumption order, indexed now so that turning FIFO on is a setting rather than a
-- performance investigation.
CREATE INDEX ix_inventory_layers_fifo
  ON inventory_layers (variant_id, warehouse_id, is_exhausted, received_at);
CREATE INDEX ix_inventory_layers_movement ON inventory_layers (source_movement_id);
