-- 0007_cashbook.sql — Phase L6: the cash book — expenses, withdrawals, deposits and closing counts.
--
-- Design: docs/mizan_lite/phases/L6_REPORTS.md §7.3, §8 (verified on a copy of the L5 seeded shop, §8.1). No existing
-- table changes: profit, takings, stock value and the drawer's other terms are read from what earlier phases recorded.
-- Insert-only; ordered by seq, never by clock. Every nullable comparison in a CHECK is guarded (PROGRESS O7).

CREATE TABLE cash_entries (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  seq                 BIGINT       NOT NULL CHECK (seq >= 1),                    -- the book's order, never the clock
  business_date       CHAR(10)     NOT NULL,
  occurred_at         CHAR(24)     NOT NULL,
  kind                VARCHAR(10)  NOT NULL CHECK (kind IN ('expense', 'withdrawal', 'deposit', 'count', 'reversal')),
  currency            CHAR(3)      NOT NULL REFERENCES currencies(code),
  amount_minor        BIGINT       NOT NULL,          -- money: > 0; a count: what was counted, >= 0
  expected_minor      BIGINT,                         -- a count: what the report expected when it was counted
  category            VARCHAR(12),                    -- an expense: rent, electricity, wages, transport, supplies, other
  from_drawer         SMALLINT     CHECK (from_drawer IN (0, 1)),
  reverses_id         CHAR(36)     REFERENCES cash_entries(id),
  fx_rate_id          CHAR(36)     REFERENCES fx_rates(id),
  local_per_usd_nano  BIGINT       CHECK (local_per_usd_nano IS NULL OR local_per_usd_nano > 0),
  note                VARCHAR(200),
  created_at          CHAR(24)     NOT NULL,
  CONSTRAINT ux_cash_entries_seq      UNIQUE (seq),
  CONSTRAINT ux_cash_entries_reverses UNIQUE (reverses_id),
  -- A reversal copies its entry's amount, and a count may be of an empty drawer: both may be zero (L6 §17).
  CONSTRAINT ck_cash_money_is_positive CHECK (amount_minor >= 0 AND (kind IN ('count', 'reversal') OR amount_minor > 0)),
  CONSTRAINT ck_cash_count_is_complete CHECK (
    (kind = 'count' AND amount_minor >= 0 AND expected_minor IS NOT NULL) OR (kind <> 'count' AND expected_minor IS NULL)),
  CONSTRAINT ck_cash_expense_is_complete CHECK (
    (kind = 'expense' AND category IS NOT NULL
       AND category IN ('rent', 'electricity', 'wages', 'transport', 'supplies', 'other') AND from_drawer IS NOT NULL) OR
    (kind <> 'expense' AND category IS NULL AND from_drawer IS NULL)),
  CONSTRAINT ck_cash_reversal_names_its_entry CHECK ((kind = 'reversal') = (reverses_id IS NOT NULL)),
  CONSTRAINT ck_cash_reversal_has_a_reason CHECK (kind <> 'reversal' OR (note IS NOT NULL AND length(trim(note)) > 0)),
  CONSTRAINT ck_cash_rate_on_money CHECK (
    (kind IN ('expense', 'withdrawal', 'deposit') AND fx_rate_id IS NOT NULL AND local_per_usd_nano IS NOT NULL) OR
    (kind IN ('count', 'reversal') AND fx_rate_id IS NULL AND local_per_usd_nano IS NULL))
);
CREATE INDEX ix_cash_entries_day ON cash_entries (business_date, currency);
