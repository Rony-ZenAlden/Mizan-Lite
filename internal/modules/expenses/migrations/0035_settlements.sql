-- 0035_settlements — paying what an expense left owed.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- THE THIRD MODULE TO NEED THIS SHAPE, AND STILL ITS OWN TABLES
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Sales settles invoices, purchasing settles bills, and this settles expenses left on account.
-- Three copies of "payment + allocations" — which by Phase 6's own recorded rule ("the moment a
-- fact needs a third home, the second home was the wrong one") looks like a mistake.
--
-- It is not, and the distinction is worth being precise about. What moved to a shared home is the
-- ARITHMETIC: what over-allocation means, what outstanding means, and that neither may go
-- negative. That was genuinely copied twice and now lives in `kernel/settle`.
--
-- What did NOT move is the DATA, because Phases 5 and 6 each rejected a shared table twice and
-- independently, with an argument that still holds: a shared allocation table would need a
-- nullable foreign key to sales documents, purchase bills, AND expenses, plus a CHECK that
-- exactly one is set. That is a discriminated union hand-rolled in SQL, and every query against
-- it carries a filter somebody eventually writes without.
--
-- A real foreign key to a real table is worth more than one fewer table. Three small tables with
-- integrity beat one wide table with a CHECK constraint standing in for it.

CREATE TABLE expense_payments (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  company_id    CHAR(36)    NOT NULL REFERENCES companies(id),
  branch_id     CHAR(36)    NOT NULL REFERENCES branches(id),

  -- NULLABLE, like the expense it settles. A landlord paid in cash with no partner record is
  -- ordinary, and the payee NAME is what a report shows.
  partner_id    CHAR(36)    REFERENCES partners(id),
  payee_name    VARCHAR(200) NOT NULL,

  document_number VARCHAR(60),
  payment_date  VARCHAR(10) NOT NULL,

  -- HOW the money left, which decides WHERE it came from. Part of the posting ACTION, so a
  -- business whose cheques clear through a separate account edits a seed file (§20.3).
  method        VARCHAR(20) NOT NULL
                CHECK (method IN ('cash', 'card', 'bank_transfer', 'cheque')),
  reference     VARCHAR(80),

  currency_code CHAR(3)     NOT NULL REFERENCES currencies(code),
  -- Always POSITIVE. A refund is money coming the other way and is its own document; a negative
  -- would make every sum over payments meaningless.
  amount_minor  INTEGER     NOT NULL CHECK (amount_minor > 0),

  status        VARCHAR(20) NOT NULL DEFAULT 'draft'
                CHECK (status IN ('draft', 'posted', 'cancelled')),

  posted_at     VARCHAR(32),
  posted_by     CHAR(36)    REFERENCES users(id),

  row_version   INTEGER     NOT NULL DEFAULT 1,
  created_at    VARCHAR(32) NOT NULL,
  created_by    CHAR(36)    REFERENCES users(id),
  updated_at    VARCHAR(32) NOT NULL,

  CONSTRAINT ck_exppay_draft_unnumbered CHECK (
    status <> 'draft' OR document_number IS NULL
  ),
  CONSTRAINT ck_exppay_posted_numbered CHECK (
    status <> 'posted' OR document_number IS NOT NULL
  )
);

CREATE UNIQUE INDEX ux_expense_payments_number
  ON expense_payments (company_id, document_number) WHERE document_number IS NOT NULL;
CREATE INDEX ix_expense_payments_partner ON expense_payments (partner_id, payment_date);

CREATE TABLE expense_payment_allocations (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  payment_id    CHAR(36)    NOT NULL REFERENCES expense_payments(id) ON DELETE CASCADE,
  -- A REAL foreign key, which is the whole reason this table exists rather than a shared one.
  expense_id    CHAR(36)    NOT NULL REFERENCES expenses(id),

  amount_minor  INTEGER     NOT NULL CHECK (amount_minor > 0),

  created_at    VARCHAR(32) NOT NULL
);

-- One allocation per payment per expense. Two rows for the same pair would be two answers to
-- "how much of this payment went to that expense", with no rule for choosing between them.
CREATE UNIQUE INDEX ux_expense_payment_allocations
  ON expense_payment_allocations (payment_id, expense_id);
CREATE INDEX ix_expense_payment_allocations_expense
  ON expense_payment_allocations (expense_id);
