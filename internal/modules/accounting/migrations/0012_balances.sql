-- 0012_balances — per-period account movement, maintained incrementally (§20.5).
--
-- Owned by the accounting module. The trial balance and every financial statement read THIS
-- table, never the full ledger: a shop with three years of history has hundreds of thousands of
-- journal lines and about two hundred accounts, and summing the former on every report is the
-- difference between an instant screen and a spinner.
--
-- # Movement only, and why that deviates from §20.5
--
-- §20.5 lists "opening/debit/credit/closing". This table stores only the MOVEMENT — the debit
-- and credit totals within one period — and derives opening and closing on read.
--
-- The reason is the very failure §20.5's rebuild job exists to catch. An opening balance is
-- exactly the sum of every earlier period's movement; storing it too means two facts that must
-- agree, maintained by different code paths, and the day they disagree the reports are wrong
-- and still add up. Storing less means there is less that can drift.
--
-- The cost is a running sum over prior periods on read. For a decade of history that is a
-- hundred-odd rows per account — nothing, and bounded by the calendar rather than by trade
-- volume, which is the number that actually grows.

CREATE TABLE account_balances (
  id               CHAR(36) NOT NULL PRIMARY KEY,
  company_id       CHAR(36) NOT NULL REFERENCES companies(id),
  account_id       CHAR(36) NOT NULL REFERENCES accounts(id),
  fiscal_period_id CHAR(36) NOT NULL REFERENCES fiscal_periods(id),

  -- NULL means "all branches" — the aggregate row. Branch-level rows are written alongside it
  -- when a line carries a branch, so a branch P&L needs no different query shape.
  branch_id        CHAR(36) REFERENCES branches(id),

  -- Totals for THIS period only, in the functional currency's minor units, as integers.
  -- Never a float, in the one table a report reads most.
  debit_minor      BIGINT   NOT NULL DEFAULT 0,
  credit_minor     BIGINT   NOT NULL DEFAULT 0,

  updated_at       CHAR(24) NOT NULL,

  CONSTRAINT ck_account_balances_signs CHECK (debit_minor >= 0 AND credit_minor >= 0)
);

-- One row per account per period per branch scope. COALESCE gives NULL a value so the aggregate
-- row cannot be written twice — NULLs are distinct in a unique index, which would otherwise
-- allow two "all branches" rows whose totals each looked plausible.
CREATE UNIQUE INDEX ux_account_balances ON account_balances (
  account_id, fiscal_period_id, COALESCE(branch_id, '')
);

-- The index the trial balance reads: every account's movement within one period.
CREATE INDEX ix_account_balances_period ON account_balances (company_id, fiscal_period_id);
