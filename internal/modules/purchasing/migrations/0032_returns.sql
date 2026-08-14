-- 0032_returns — goods going back to a supplier, and the debit note that says so.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- WHY A DEBIT NOTE AND NOT A NEGATIVE BILL
-- ─────────────────────────────────────────────────────────────────────────────
--
-- A negative bill would balance arithmetically and mean the wrong thing everywhere else. The
-- posting engine refuses negative amounts (§20.3) precisely because a negative silently flips a
-- line to the other side of an entry, and a report that filters `status = 'posted'` would count
-- the return as a purchase and understate nothing while overstating both sides.
--
-- The same reasoning Phase 5 applied to credit notes: when the books must differ, the ACTION
-- differs. A debit note is its own document with its own action, and the seeded rule decides
-- what that action does.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- WHY EACH LINE NAMES THE DELIVERY LINE IT SENDS BACK
-- ─────────────────────────────────────────────────────────────────────────────
--
-- §D.3: a return is costed at the ORIGINAL receipt's cost, never today's average. Returning
-- goods bought at last year's price at this year's average invents a gain or a loss that never
-- happened.
--
-- That requires the line to point at what it reverses, which is why `receipt_line_id` is NOT
-- NULL — and through it at the stock movement whose unit cost is the answer.

CREATE TABLE supplier_returns (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  branch_id      CHAR(36)    NOT NULL REFERENCES branches(id),
  warehouse_id   CHAR(36)    NOT NULL REFERENCES warehouses(id),

  status         VARCHAR(20) NOT NULL DEFAULT 'draft'
                 CHECK (status IN ('draft', 'posted', 'cancelled')),
  document_number VARCHAR(60),

  partner_id     CHAR(36)    NOT NULL REFERENCES partners(id),
  partner_name   VARCHAR(200) NOT NULL,

  return_date    VARCHAR(10) NOT NULL,

  -- Why the goods went back. The single most-read field on this table: a supplier arguing about
  -- a debit note asks what was wrong with the delivery, and "damaged in transit" settles it.
  reason         VARCHAR(400),
  -- Their reference for the return authorisation, where they issue one.
  supplier_reference VARCHAR(60),

  currency_code  CHAR(3)     NOT NULL REFERENCES currencies(code),

  -- What the supplier is being debited: the price they charged.
  net_minor      INTEGER     NOT NULL DEFAULT 0,
  tax_minor      INTEGER     NOT NULL DEFAULT 0,
  total_minor    INTEGER     NOT NULL DEFAULT 0,
  -- What the goods COST us, at the original receipt's figure. Different from the net whenever a
  -- bill corrected the price after delivery (6.4), and the difference is a real gain or loss on
  -- the return rather than an error.
  cost_minor     INTEGER     NOT NULL DEFAULT 0,

  posted_at      VARCHAR(32),
  posted_by      CHAR(36)    REFERENCES users(id),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  created_by     CHAR(36)    REFERENCES users(id),
  updated_at     VARCHAR(32) NOT NULL,

  CONSTRAINT ck_ret_draft_unnumbered CHECK (
    status <> 'draft' OR document_number IS NULL
  ),
  CONSTRAINT ck_ret_posted_numbered CHECK (
    status <> 'posted' OR document_number IS NOT NULL
  )
);

CREATE UNIQUE INDEX ux_supplier_returns_number
  ON supplier_returns (company_id, document_number)
  WHERE document_number IS NOT NULL;
CREATE INDEX ix_supplier_returns_partner ON supplier_returns (partner_id, return_date);

CREATE TABLE supplier_return_lines (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  return_id      CHAR(36)    NOT NULL REFERENCES supplier_returns(id) ON DELETE CASCADE,
  line_number    INTEGER     NOT NULL,

  -- What is being sent back. NOT NULL, because a return that cannot name its delivery cannot be
  -- costed at that delivery's price — and costing it at today's average is the defect §D.3
  -- exists to prevent.
  receipt_line_id CHAR(36)   NOT NULL REFERENCES goods_receipt_lines(id),

  product_id     CHAR(36)    NOT NULL REFERENCES products(id),
  variant_id     CHAR(36)    NOT NULL REFERENCES product_variants(id),

  product_name   VARCHAR(300) NOT NULL,
  variant_sku    VARCHAR(80)  NOT NULL,
  uom_code       VARCHAR(20)  NOT NULL,

  -- PARTIAL returns are ordinary: two of ten arrived damaged. Unlike a bill line, which takes a
  -- delivery in full, a return names its own quantity — and it is bounded by what arrived and
  -- by what has already gone back.
  quantity_micro INTEGER     NOT NULL CHECK (quantity_micro > 0),
  quantity_stock_micro INTEGER NOT NULL,

  -- The price they charged, and the cost we carried. Both snapshotted (§9.3).
  unit_price_micro INTEGER   NOT NULL DEFAULT 0,
  unit_cost_micro INTEGER    NOT NULL DEFAULT 0,
  tax_rate_micro INTEGER     NOT NULL DEFAULT 0,
  tax_code       VARCHAR(40),
  tax_amount_minor INTEGER   NOT NULL DEFAULT 0,
  net_minor      INTEGER     NOT NULL DEFAULT 0,
  cost_minor     INTEGER     NOT NULL DEFAULT 0,
  total_minor    INTEGER     NOT NULL DEFAULT 0,

  -- The outward movement this line wrote once posted.
  movement_id    CHAR(36)    REFERENCES stock_movements(id),

  notes          VARCHAR(400),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  updated_at     VARCHAR(32) NOT NULL
);

CREATE UNIQUE INDEX ux_return_lines_number ON supplier_return_lines (return_id, line_number);
CREATE INDEX ix_return_lines_return ON supplier_return_lines (return_id);
-- NOT unique: one delivery line can be returned against more than once, because damage is found
-- in batches. What bounds it is the running total against what arrived, which the service checks.
CREATE INDEX ix_return_lines_receipt_line ON supplier_return_lines (receipt_line_id);
