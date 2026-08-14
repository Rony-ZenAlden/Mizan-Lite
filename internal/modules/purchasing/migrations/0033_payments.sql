-- 0033_payments — money going out to suppliers, and what it settles.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- THE SAME TWO-TABLE SHAPE SALES USES, AND FOR THE SAME REASONS
-- ─────────────────────────────────────────────────────────────────────────────
--
-- 0024 wrote the argument for customer payments and it transfers unchanged. `paid_minor` on a
-- bill cannot express any of the three shapes a real business produces every week:
--
--   - one payment settling SEVERAL bills (clearing a supplier's statement)
--   - one bill taking SEVERAL payments (a deposit, then the balance on delivery)
--   - a payment allocated to NOTHING yet (a prepayment against future orders)
--
-- Two tables express all three, and what a bill still owes becomes a sum over allocations rather
-- than a maintained figure that can drift from them.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- WHY THIS IS NOT `sales_payments` WITH A DIRECTION COLUMN
-- ─────────────────────────────────────────────────────────────────────────────
--
-- The two are symmetrical and they are not the same thing. A customer payment allocates against
-- `sales_documents`; a supplier payment allocates against `purchase_bills`. One table would need
-- a nullable foreign key to each and a CHECK that exactly one is set — which is a discriminated
-- union hand-rolled in SQL, and every query against it would carry a direction filter that
-- somebody eventually writes without.
--
-- It would also make sales and purchasing share a table, which `module-isolation` forbids for a
-- reason: neither could change its own shape without the other's agreement.

CREATE TABLE supplier_payments (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  branch_id      CHAR(36)    NOT NULL REFERENCES branches(id),

  -- A supplier is REQUIRED, unlike a customer payment's walk-in. Money leaving the business goes
  -- to somebody, and a payment with no payee is a hole in the cash position that nobody can
  -- chase.
  partner_id     CHAR(36)    NOT NULL REFERENCES partners(id),
  partner_name   VARCHAR(200) NOT NULL,

  document_number VARCHAR(60),
  payment_date   VARCHAR(10) NOT NULL,

  -- HOW the money left, which decides WHERE it came from.
  --
  -- Cash leaves the till; a transfer or a cheque leaves the bank. The account is NOT named here:
  -- the method becomes part of the posting ACTION and the seeded rules decide the rest (§20.3).
  -- That is what lets a business whose cheques clear through a separate account change a seed
  -- file rather than this schema.
  method         VARCHAR(20) NOT NULL
                 CHECK (method IN ('cash', 'card', 'bank_transfer', 'cheque')),
  -- Their reference: the transfer number, the cheque number. What a supplier quotes back when
  -- they cannot find the money.
  reference      VARCHAR(80),

  currency_code  CHAR(3)     NOT NULL REFERENCES currencies(code),
  exchange_rate_micro INTEGER NOT NULL DEFAULT 1000000 CHECK (exchange_rate_micro > 0),
  -- Always POSITIVE. A refund from a supplier is money coming the other way, which is its own
  -- document — a negative here would make SUM(amount) meaningless, the same reasoning that keeps
  -- stock movement quantities positive.
  amount_minor   INTEGER     NOT NULL CHECK (amount_minor > 0),

  status         VARCHAR(20) NOT NULL DEFAULT 'draft'
                 CHECK (status IN ('draft', 'posted', 'cancelled')),

  notes          VARCHAR(400),

  posted_at      VARCHAR(32),
  posted_by      CHAR(36)    REFERENCES users(id),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  created_by     CHAR(36)    REFERENCES users(id),
  updated_at     VARCHAR(32) NOT NULL,

  CONSTRAINT ck_pay_draft_unnumbered CHECK (
    status <> 'draft' OR document_number IS NULL
  ),
  CONSTRAINT ck_pay_posted_numbered CHECK (
    status <> 'posted' OR document_number IS NOT NULL
  )
);

CREATE UNIQUE INDEX ux_supplier_payments_number
  ON supplier_payments (company_id, document_number)
  WHERE document_number IS NOT NULL;
CREATE INDEX ix_supplier_payments_partner ON supplier_payments (partner_id, payment_date);

CREATE TABLE supplier_payment_allocations (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  payment_id     CHAR(36)    NOT NULL REFERENCES supplier_payments(id) ON DELETE CASCADE,
  bill_id        CHAR(36)    NOT NULL REFERENCES purchase_bills(id),

  amount_minor   INTEGER     NOT NULL CHECK (amount_minor > 0),

  created_at     VARCHAR(32) NOT NULL
);

-- One allocation per payment per bill. Two rows for the same pair would be two answers to "how
-- much of this payment went to that bill", and no rule for choosing between them.
CREATE UNIQUE INDEX ux_supplier_payment_allocations
  ON supplier_payment_allocations (payment_id, bill_id);
CREATE INDEX ix_supplier_payment_allocations_bill ON supplier_payment_allocations (bill_id);
