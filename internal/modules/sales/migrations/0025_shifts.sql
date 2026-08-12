-- 0025_shifts — the POS shift and its cash reconciliation (§2.5).
--
-- # What a shift is for
--
-- A till opens with a float, takes money all day, and closes with somebody counting the drawer.
-- The difference between what the system expected and what was actually there is the single most
-- useful number a shop's owner gets from a POS — and the one a badly built system quietly
-- absorbs.
--
-- This is the POS equivalent of Phase 4's stock reconciliation, and it follows the same rule: the
-- difference is RECORDED, not hidden. A till that balances itself teaches nobody anything.

CREATE TABLE pos_shifts (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  branch_id      CHAR(36)    NOT NULL REFERENCES branches(id),

  -- Which physical till. Free text rather than a table: a shop with two drawers calls them
  -- whatever it calls them, and a `terminals` table would be a registry somebody has to maintain
  -- before they can take money.
  terminal       VARCHAR(40) NOT NULL DEFAULT '',

  opened_at      VARCHAR(32) NOT NULL,
  opened_by      CHAR(36)    REFERENCES users(id),
  -- What was in the drawer at the start. Counted by a person, not carried over from yesterday:
  -- carrying it over would make one shift's error every later shift's error too.
  opening_float_minor INTEGER NOT NULL DEFAULT 0 CHECK (opening_float_minor >= 0),

  closed_at      VARCHAR(32),
  closed_by      CHAR(36)    REFERENCES users(id),

  -- What the system believed should be there: the float plus every cash payment taken during the
  -- shift. DERIVED at close and then FROZEN, because it is the figure the difference was computed
  -- from and a later payment must not silently change a closed shift's arithmetic.
  expected_minor INTEGER,
  -- What was actually in the drawer.
  counted_minor  INTEGER,
  -- counted − expected. Stored rather than derived on read, for the same reason: it is what the
  -- person who closed the shift was shown and signed off.
  difference_minor INTEGER,

  status         VARCHAR(20) NOT NULL DEFAULT 'open'
                 CHECK (status IN ('open', 'closed')),
  notes          TEXT,

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  updated_at     VARCHAR(32) NOT NULL,

  -- A closed shift has all three figures; an open one has none. Half a reconciliation is a
  -- number somebody will read as complete.
  CONSTRAINT ck_shift_closed_is_counted CHECK (
    (status = 'closed' AND closed_at IS NOT NULL AND counted_minor IS NOT NULL
       AND expected_minor IS NOT NULL AND difference_minor IS NOT NULL) OR
    (status = 'open' AND closed_at IS NULL AND counted_minor IS NULL
       AND expected_minor IS NULL AND difference_minor IS NULL)
  )
);

-- ONE OPEN SHIFT PER TILL.
--
-- Two would make "which shift did this payment belong to" unanswerable, and the answer is the
-- whole point of the table. The same partial-unique-index shape used for a category's reference
-- unit (0015), a product's default variant (0016), and a company's default price list (0019).
CREATE UNIQUE INDEX ux_pos_shifts_one_open
  ON pos_shifts (branch_id, terminal) WHERE status = 'open';

CREATE INDEX ix_pos_shifts_branch ON pos_shifts (branch_id, opened_at);

-- Which shift took a payment. NULL for a payment taken outside the POS — a bank transfer that
-- arrives while the shop is shut belongs to no till.
ALTER TABLE sales_payments ADD COLUMN shift_id CHAR(36) REFERENCES pos_shifts(id);

CREATE INDEX ix_sales_payments_shift ON sales_payments (shift_id);
