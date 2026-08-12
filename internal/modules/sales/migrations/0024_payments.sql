-- 0024_payments — customer payments and what they settle (§2.4).
--
-- # A payment is its own document, not a field on an invoice
--
-- The instinct is `paid_minor` on `sales_documents`. It cannot express any of the three shapes a
-- real shop produces every day:
--
--   - one payment settling SEVERAL invoices (a customer clears their account)
--   - one invoice taking SEVERAL payments (a deposit, then the balance)
--   - a payment allocated to NOTHING yet (a deposit against future orders)
--
-- Two tables express all three, and the invoice's outstanding amount becomes a sum over
-- allocations rather than a maintained figure that can drift from them.

CREATE TABLE sales_payments (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  branch_id      CHAR(36)    NOT NULL REFERENCES branches(id),

  -- NULL is a walk-in paying cash for a walk-in sale. Most of a shop's trade, and it must not
  -- require a customer record.
  partner_id     CHAR(36)    REFERENCES partners(id),
  partner_name   VARCHAR(200),

  document_number VARCHAR(60),
  payment_date   VARCHAR(10) NOT NULL,

  -- HOW the money arrived, which decides WHERE it lands.
  --
  -- Cash goes to the till; a card or a transfer goes to the bank. The account is not named here —
  -- the method becomes part of the posting ACTION, and the rules decide the rest (§20.3). That is
  -- what lets a business whose card settlements land in a third account change a seed file.
  method         VARCHAR(20) NOT NULL
                 CHECK (method IN ('cash', 'card', 'bank_transfer', 'cheque', 'credit')),
  reference      VARCHAR(80),

  currency_code  CHAR(3)     NOT NULL REFERENCES currencies(code),
  exchange_rate_micro INTEGER NOT NULL DEFAULT 1000000 CHECK (exchange_rate_micro > 0),
  -- Always POSITIVE. A refund is a payment in the other direction, and giving it a negative
  -- amount would make SUM(amount) meaningless — the same reasoning that keeps stock movement
  -- quantities positive (§4.1).
  amount_minor   INTEGER     NOT NULL CHECK (amount_minor > 0),

  status         VARCHAR(20) NOT NULL DEFAULT 'draft'
                 CHECK (status IN ('draft', 'posted', 'cancelled')),

  notes          TEXT,
  posted_at      VARCHAR(32),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  created_by     CHAR(36)    REFERENCES users(id),
  updated_at     VARCHAR(32) NOT NULL,

  CONSTRAINT ck_payment_posted_is_numbered CHECK (
    (status = 'posted' AND document_number IS NOT NULL) OR
    (status <> 'posted' AND document_number IS NULL)
  )
);

CREATE UNIQUE INDEX ux_sales_payments_number
  ON sales_payments (company_id, document_number) WHERE document_number IS NOT NULL;
CREATE INDEX ix_sales_payments_partner ON sales_payments (partner_id, payment_date);
CREATE INDEX ix_sales_payments_status ON sales_payments (company_id, status, payment_date);

-- ─────────────────────────────────────────────────────────────────────────────
-- sales_payment_allocations — which payment settles which invoice, and by how much
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE sales_payment_allocations (
  id           CHAR(36)    NOT NULL PRIMARY KEY,
  payment_id   CHAR(36)    NOT NULL REFERENCES sales_payments(id) ON DELETE CASCADE,
  document_id  CHAR(36)    NOT NULL REFERENCES sales_documents(id),

  amount_minor INTEGER     NOT NULL CHECK (amount_minor > 0),

  created_at   VARCHAR(32) NOT NULL
);

-- One allocation per payment per document. A payment settling the same invoice twice is two rows
-- that should have been one, and the sum would be right while the trail read as two events.
CREATE UNIQUE INDEX ux_payment_allocations
  ON sales_payment_allocations (payment_id, document_id);
CREATE INDEX ix_payment_allocations_document ON sales_payment_allocations (document_id);
