-- 0022_counts — physical stock counts, as documents.
--
-- Owned by the inventory module, which owns 0020 and 0021.
--
-- # Why a count is a DOCUMENT and not a movement
--
-- A movement is an instant. A count is a process that takes hours: a sheet is generated, people
-- walk the aisles, numbers come back, somebody reviews the surprising ones, and only then does
-- the stock change. Modelling it as a bare movement loses every part of that except the last —
-- and the parts it loses are the ones an auditor asks about.
--
-- It also loses the control that makes counting worth doing. A blind count hides the expected
-- quantity from the counter, so what comes back is what is on the shelf rather than what the
-- system already believed. That needs the expectation recorded somewhere the counter cannot see,
-- which needs a document.

CREATE TABLE stock_counts (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  company_id    CHAR(36)    NOT NULL REFERENCES companies(id),
  warehouse_id  CHAR(36)    NOT NULL REFERENCES warehouses(id),

  -- Human-readable, because a count is referred to out loud: "the March count".
  reference     VARCHAR(40) NOT NULL,
  description   VARCHAR(400),

  -- draft: lines are being added.
  -- counting: the sheet is out, expectations are frozen, counters are entering figures.
  -- review: figures are in and somebody is looking at the variances.
  -- applied: the movements have been written. TERMINAL.
  -- cancelled: abandoned. Also terminal, and leaves no movements.
  status        VARCHAR(20) NOT NULL DEFAULT 'draft'
                CHECK (status IN ('draft', 'counting', 'review', 'applied', 'cancelled')),

  -- A blind count hides the expected quantity from whoever is counting.
  --
  -- The single most valuable control in this table. Showing the expectation is how a count comes
  -- back agreeing with the system on every line while the shelves say otherwise — not through
  -- dishonesty, but because a tired person looking at "10" finds ten.
  is_blind      INTEGER     NOT NULL DEFAULT 1 CHECK (is_blind IN (0, 1)),

  -- When the expectations were frozen. Everything after this instant is a concurrent movement,
  -- and the apply must NOT discard it — see stock_count_lines.expected_micro.
  snapshot_at   VARCHAR(32),
  applied_at    VARCHAR(32),
  applied_by    CHAR(36)    REFERENCES users(id),

  row_version   INTEGER     NOT NULL DEFAULT 1,
  created_at    VARCHAR(32) NOT NULL,
  created_by    CHAR(36)    REFERENCES users(id),
  updated_at    VARCHAR(32) NOT NULL
);

CREATE UNIQUE INDEX ux_stock_counts_reference ON stock_counts (company_id, reference);
CREATE INDEX ix_stock_counts_warehouse ON stock_counts (warehouse_id, status);

-- ─────────────────────────────────────────────────────────────────────────────
-- stock_count_lines — one per thing counted
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE stock_count_lines (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  count_id      CHAR(36)    NOT NULL REFERENCES stock_counts(id) ON DELETE CASCADE,

  product_id    CHAR(36)    NOT NULL REFERENCES products(id),
  variant_id    CHAR(36)    NOT NULL REFERENCES product_variants(id),
  -- Set when the product is lot-tracked: a batch is counted, not a product.
  lot_id        CHAR(36)    REFERENCES stock_lots(id),

  -- What the system believed AT THE SNAPSHOT. Frozen, never refreshed.
  --
  -- This is the column the whole design turns on. Applying a count as an ABSOLUTE ("set stock to
  -- 8") would silently discard every movement made while people were counting — a sale during
  -- the count would be undone by the count. Applying the DIFFERENCE the counter found
  -- (counted − expected) preserves them: the counter found two fewer than the sheet said, so two
  -- fewer is the adjustment, whatever else happened meanwhile.
  expected_micro INTEGER    NOT NULL DEFAULT 0,

  -- What was actually on the shelf. NULL until somebody counts it, which is not the same as
  -- zero: an uncounted line must not be applied as "there are none", which would write off the
  -- entire stock of anything the counter did not reach.
  counted_micro INTEGER,

  -- Recorded at apply time, so the line keeps the arithmetic that produced the movement.
  variance_micro INTEGER,

  note          VARCHAR(400),
  counted_at    VARCHAR(32),
  counted_by    CHAR(36)    REFERENCES users(id),

  row_version   INTEGER     NOT NULL DEFAULT 1,
  created_at    VARCHAR(32) NOT NULL,
  updated_at    VARCHAR(32) NOT NULL
);

-- One line per thing. Counting the same variant twice on one sheet gives two answers and no rule
-- to choose between them. COALESCE, because a NULL lot is a distinct value in an index but not
-- to a person.
CREATE UNIQUE INDEX ux_stock_count_lines
  ON stock_count_lines (count_id, variant_id, COALESCE(lot_id, ''));
CREATE INDEX ix_stock_count_lines_count ON stock_count_lines (count_id);
