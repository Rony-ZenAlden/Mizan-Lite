-- 0029_bills — supplier invoices, and the third side of the match.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- THE THREE-WAY MATCH
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Ordered vs RECEIVED vs BILLED, on quantity and on price. It is the single mechanism that stops
-- a business paying for goods it never got — the most common way money leaves a small business by
-- accident, and the reason purchasing exists as a discipline separate from paying bills.
--
-- The match is only expressible because the three are separate documents linked on their LINES.
-- A bill line points at the RECEIPT line it pays for, and through it at the order line that
-- asked for the goods. Each side can then be compared with the others:
--
--   quantity  ordered → received : how much is still to come (0028)
--   quantity  received → billed  : is the supplier invoicing for goods that arrived?
--   price     ordered → billed   : is the supplier charging what was agreed?
--
-- ─────────────────────────────────────────────────────────────────────────────
-- A BILL LINE TAKES A RECEIPT LINE IN FULL
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Not a partial quantity of one. The receipt line is ALREADY the record of what physically
-- arrived in one delivery, at one time, signed for by one person — it is the natural unit of
-- "this much was received", and suppliers do not invoice half a delivery line.
--
-- Allowing partial take-up would mean tracking how much of each receipt line remains unbilled,
-- which is a third projection to maintain and reconcile, in exchange for a case nobody has.
-- Where a supplier really does split an invoice, the answer is two bills, each taking whole
-- lines — which is what the paper looks like anyway.
--
-- This also makes GRNI clearing exact rather than approximate: a bill clears precisely what its
-- receipts accrued, so the account returns to zero when the paperwork agrees.

CREATE TABLE purchase_bills (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  branch_id      CHAR(36)    NOT NULL REFERENCES branches(id),

  status         VARCHAR(20) NOT NULL DEFAULT 'draft'
                 CHECK (status IN ('draft', 'posted', 'cancelled')),
  document_number VARCHAR(60),

  partner_id     CHAR(36)    NOT NULL REFERENCES partners(id),
  partner_name   VARCHAR(200) NOT NULL,

  bill_date      VARCHAR(10) NOT NULL,
  -- When the supplier expects to be paid. Derived from their payment terms at drafting and then
  -- SNAPSHOTTED: terms renegotiated next year must not silently move the due date on an invoice
  -- already agreed.
  due_date       VARCHAR(10),

  -- THE SUPPLIER'S OWN INVOICE NUMBER, which is the one that matters.
  --
  -- Our `document_number` is for our filing; theirs is what a payment reference must quote and
  -- what a statement reconciliation matches on. Unique per supplier, because the same invoice
  -- entered twice is a payment made twice — and a duplicate number from a DIFFERENT supplier is
  -- entirely ordinary.
  supplier_invoice_number VARCHAR(60) NOT NULL,

  currency_code  CHAR(3)     NOT NULL REFERENCES currencies(code),
  exchange_rate_micro INTEGER NOT NULL DEFAULT 1000000,

  net_minor      INTEGER     NOT NULL DEFAULT 0,
  tax_minor      INTEGER     NOT NULL DEFAULT 0,
  discount_minor INTEGER     NOT NULL DEFAULT 0,
  total_minor    INTEGER     NOT NULL DEFAULT 0,

  -- What the receipts behind this bill accrued to GRNI. The figure the posting must clear
  -- EXACTLY — clearing the bill's own net instead would leave the price difference sitting in
  -- GRNI forever, and a GRNI balance nobody can explain is a GRNI balance nobody reads.
  accrued_minor  INTEGER     NOT NULL DEFAULT 0,
  -- net − accrued. Positive means the supplier charged more than was ordered.
  variance_minor INTEGER     NOT NULL DEFAULT 0,

  notes          VARCHAR(1000),

  posted_at      VARCHAR(32),
  posted_by      CHAR(36)    REFERENCES users(id),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  created_by     CHAR(36)    REFERENCES users(id),
  updated_at     VARCHAR(32) NOT NULL,

  CONSTRAINT ck_bill_draft_unnumbered CHECK (
    status <> 'draft' OR document_number IS NULL
  ),
  CONSTRAINT ck_bill_posted_numbered CHECK (
    status <> 'posted' OR document_number IS NOT NULL
  )
);

CREATE UNIQUE INDEX ux_purchase_bills_number
  ON purchase_bills (company_id, document_number)
  WHERE document_number IS NOT NULL;

-- The same supplier invoice entered twice is a payment made twice. Enforced by the SCHEMA as
-- well as by the service, because this is the one duplicate that costs money — and a busy
-- accounts clerk entering a batch is exactly who will do it.
CREATE UNIQUE INDEX ux_purchase_bills_supplier_invoice
  ON purchase_bills (company_id, partner_id, supplier_invoice_number);

CREATE INDEX ix_purchase_bills_partner ON purchase_bills (partner_id, bill_date);
CREATE INDEX ix_purchase_bills_due ON purchase_bills (company_id, due_date)
  WHERE status = 'posted';

-- ─────────────────────────────────────────────────────────────────────────────
-- purchase_bill_lines
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE purchase_bill_lines (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  bill_id        CHAR(36)    NOT NULL REFERENCES purchase_bills(id) ON DELETE CASCADE,
  line_number    INTEGER     NOT NULL,

  -- The receipt line this pays for. NOT NULL: a bill line with no delivery behind it is an
  -- invoice for goods nobody received, which is the whole thing the match exists to refuse.
  -- Charges that are genuinely not goods — freight, customs — are LANDED COSTS and are their own
  -- document (6.5), because they allocate across lines rather than being one.
  receipt_line_id CHAR(36)   NOT NULL REFERENCES goods_receipt_lines(id),

  product_id     CHAR(36)    NOT NULL REFERENCES products(id),
  variant_id     CHAR(36)    NOT NULL REFERENCES product_variants(id),

  -- The §9.3 snapshot.
  product_name   VARCHAR(300) NOT NULL,
  variant_sku    VARCHAR(80)  NOT NULL,
  uom_code       VARCHAR(20)  NOT NULL,

  quantity_micro INTEGER     NOT NULL,

  -- What the supplier is charging, and what we accrued when the goods arrived. Both, because
  -- their difference is the price variance and a document that stored only the difference could
  -- not say which side moved.
  unit_price_micro INTEGER   NOT NULL DEFAULT 0,
  accrued_unit_cost_micro INTEGER NOT NULL DEFAULT 0,

  discount_minor INTEGER     NOT NULL DEFAULT 0,
  tax_rate_micro INTEGER     NOT NULL DEFAULT 0,
  tax_code       VARCHAR(40),
  tax_amount_minor INTEGER   NOT NULL DEFAULT 0,
  net_minor      INTEGER     NOT NULL DEFAULT 0,
  accrued_minor  INTEGER     NOT NULL DEFAULT 0,
  total_minor    INTEGER     NOT NULL DEFAULT 0,

  notes          VARCHAR(400),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  updated_at     VARCHAR(32) NOT NULL
);

CREATE UNIQUE INDEX ux_bill_lines_number ON purchase_bill_lines (bill_id, line_number);

-- ONE bill line per receipt line, EVER — enforced across every bill, not merely within one.
--
-- This is the schema half of "a receipt can be billed only once". The service checks it too, and
-- both are needed: the service check gives a readable refusal, and this one holds when two
-- clerks post two bills for the same delivery at the same moment.
CREATE UNIQUE INDEX ux_bill_lines_receipt_line ON purchase_bill_lines (receipt_line_id);

CREATE INDEX ix_bill_lines_bill ON purchase_bill_lines (bill_id);
