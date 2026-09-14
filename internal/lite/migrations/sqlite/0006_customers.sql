-- 0006_customers.sql — Phase L5: customers and the debt book.
--
-- Design: docs/mizan_lite/phases/L5_CUSTOMERS.md §8, with §15 (cash_note_minor on money-moving rows). No existing
-- table changes: a credit sale's customer lives on its charge (A-L4.2). Every nullable comparison in a CHECK is
-- guarded, because a CHECK that evaluates to NULL passes (PROGRESS O7).

-- ─────────────────────────────────────────────────────────────────────────────
-- Customers. Updated only in their own fields; never deleted.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE customers (
  id            CHAR(36)     NOT NULL PRIMARY KEY,
  name          VARCHAR(100) NOT NULL CHECK (length(trim(name)) > 0),
  -- textkey.Normalise(name): one key for every spelling a reader calls the same name (L5 H9).
  name_key      VARCHAR(100) NOT NULL CHECK (length(name_key) > 0),
  phone         VARCHAR(20),
  note          VARCHAR(200),
  is_active     SMALLINT     NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  row_version   BIGINT       NOT NULL DEFAULT 1,
  created_at    CHAR(24)     NOT NULL,
  updated_at    CHAR(24)     NOT NULL,
  CONSTRAINT ux_customers_name_key UNIQUE (name_key)
);
CREATE INDEX ix_customers_phone ON customers (phone);

-- ─────────────────────────────────────────────────────────────────────────────
-- The debt book. Insert-only. One chain per customer AND currency, ordered by seq — never by clock (L5 H6) — each
-- entry carrying the balance before and after it. Balances in two currencies are never summed.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE debt_entries (
  id                     CHAR(36)     NOT NULL PRIMARY KEY,
  customer_id            CHAR(36)     NOT NULL REFERENCES customers(id),
  currency               CHAR(3)      NOT NULL REFERENCES currencies(code),
  seq                    BIGINT       NOT NULL CHECK (seq >= 1),
  business_date          CHAR(10)     NOT NULL,
  occurred_at            CHAR(24)     NOT NULL,
  kind                   VARCHAR(10)  NOT NULL
                           CHECK (kind IN ('opening', 'charge', 'payment', 'write_off', 'refund', 'reversal')),
  amount_minor           BIGINT       NOT NULL CHECK (amount_minor <> 0),        -- + owed, − settled
  balance_before_minor   BIGINT       NOT NULL,
  balance_after_minor    BIGINT       NOT NULL,
  sale_id                CHAR(36)     REFERENCES sales(id),                     -- a charge's sale
  reverses_id            CHAR(36)     REFERENCES debt_entries(id),              -- a reversal's entry
  -- Money that changed hands (a payment in, a refund out): the rate, what was handed over, the change, and the note
  -- the change was rounded to (A-L5.5).
  fx_rate_id             CHAR(36)     REFERENCES fx_rates(id),
  local_per_usd_nano     BIGINT,
  cash_note_minor        BIGINT,
  tendered_currency      CHAR(3)      REFERENCES currencies(code),
  tendered_minor         BIGINT,
  change_currency        CHAR(3)      REFERENCES currencies(code),
  change_minor           BIGINT,
  customer_name_snapshot VARCHAR(100) NOT NULL,                                 -- the name a receipt shows
  note                   VARCHAR(200),
  created_at             CHAR(24)     NOT NULL,
  CONSTRAINT ux_debt_entries_place    UNIQUE (customer_id, currency, seq),
  CONSTRAINT ux_debt_entries_reverses UNIQUE (reverses_id),
  CONSTRAINT ck_debt_chain CHECK (balance_after_minor = balance_before_minor + amount_minor),
  CONSTRAINT ck_debt_sign_matches_kind CHECK (
    (kind IN ('opening', 'charge', 'refund') AND amount_minor > 0) OR
    (kind IN ('payment', 'write_off')        AND amount_minor < 0) OR
     kind = 'reversal'),
  CONSTRAINT ck_debt_charge_names_its_sale    CHECK ((kind = 'charge')   = (sale_id IS NOT NULL)),
  CONSTRAINT ck_debt_reversal_names_its_entry CHECK ((kind = 'reversal') = (reverses_id IS NOT NULL)),
  CONSTRAINT ck_debt_cash_moves_only_on_payment_and_refund CHECK (
    (kind IN ('payment', 'refund')
       AND tendered_currency IS NOT NULL AND tendered_minor IS NOT NULL AND tendered_minor > 0
       AND change_currency IS NOT NULL AND change_minor IS NOT NULL AND change_minor >= 0
       AND fx_rate_id IS NOT NULL AND local_per_usd_nano IS NOT NULL AND local_per_usd_nano > 0
       AND cash_note_minor IS NOT NULL AND cash_note_minor >= 1) OR
    (kind NOT IN ('payment', 'refund')
       AND tendered_currency IS NULL AND tendered_minor IS NULL AND change_currency IS NULL AND change_minor IS NULL
       AND fx_rate_id IS NULL AND local_per_usd_nano IS NULL AND cash_note_minor IS NULL)),
  CONSTRAINT ck_debt_owner_acts_have_a_reason CHECK (
    kind NOT IN ('write_off', 'refund', 'reversal') OR (note IS NOT NULL AND length(trim(note)) > 0)),
  CONSTRAINT ck_debt_payment_never_below_zero CHECK (kind NOT IN ('payment', 'write_off') OR balance_after_minor >= 0),
  CONSTRAINT ck_debt_refund_only_what_is_owed_back CHECK (
    kind <> 'refund' OR (balance_before_minor < 0 AND balance_after_minor <= 0))
);
-- At most one charge per sale (A-L5.1). NULLs are distinct, so every entry that is not a charge passes.
CREATE UNIQUE INDEX ux_debt_entries_one_charge_per_sale ON debt_entries (sale_id);
CREATE INDEX ix_debt_entries_business_date ON debt_entries (business_date);
