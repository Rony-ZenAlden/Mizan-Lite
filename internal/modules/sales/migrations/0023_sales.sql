-- 0023_sales — sales documents and their lines (§9.3).
--
-- # There is deliberately no `number_series` table here
--
-- One was written first, and the migration failed: `migrations/sqlite/0001_platform.sql` already
-- creates it, commented "platform-level; used by every transactional module". That is the right
-- home — purchasing (Phase 6) and payments will number documents too, and a series table owned
-- by sales would make them either import sales or build a second one.
--
-- The same lesson Step 4.4 recorded: before declaring a new place for a fact to live, look for
-- the one an earlier phase already left.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- THE RULE THIS WHOLE FILE EXISTS TO KEEP (§9.3)
-- ─────────────────────────────────────────────────────────────────────────────
--
-- "Every posted document line stores a SNAPSHOT of everything used to compute it: unit price,
--  discount, tax rate applied, exchange rate used, cost at time of sale. Never a reference to a
--  mutable current value. Reprinting a two-year-old invoice must reproduce it byte-identically
--  even after prices, tax rates and exchange rates have all changed."
--
-- This inverts the instinct a schema brings. Normalisation says store `variant_id` and join for
-- the price. Correctness says store the price — because the join answers TODAY's question, and
-- an invoice is a record of what was agreed THEN.
--
-- Several columns below therefore look redundant next to a foreign key. They are not. They are
-- the difference between a reprint that says what the customer bought and one that says what
-- that product happens to be called now.

CREATE TABLE sales_documents (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  branch_id      CHAR(36)    NOT NULL REFERENCES branches(id),
  -- Where the goods leave from. NULL for a quotation, which moves nothing.
  warehouse_id   CHAR(36)    REFERENCES warehouses(id),

  -- ONE TABLE, discriminated by type (§2.1).
  --
  -- Not four tables. A quotation that becomes an order that becomes an invoice is the ordinary
  -- path, and copying rows between tables at each step is how a line's history gets lost.
  document_type  VARCHAR(20) NOT NULL
                 CHECK (document_type IN ('quotation', 'order', 'invoice', 'credit_note')),

  -- draft: editable, numbered by nobody, moves nothing.
  -- posted: TERMINAL for editing. Numbered, stock issued, books written, snapshot frozen.
  -- cancelled: abandoned before posting. Terminal.
  status         VARCHAR(20) NOT NULL DEFAULT 'draft'
                 CHECK (status IN ('draft', 'posted', 'cancelled')),

  -- NULL until posting (§9.4). An abandoned draft must consume no number.
  document_number VARCHAR(60),

  -- The chain: which document this one came from. A quotation → order → invoice progression, or
  -- the invoice a credit note reverses.
  source_document_id CHAR(36) REFERENCES sales_documents(id),

  -- NULL is a walk-in customer, which is most of a shop's trade and must not require a record.
  partner_id     CHAR(36)    REFERENCES partners(id),
  -- The customer's NAME as it was, for the same reason the line snapshots a product name: a
  -- reprint must say who bought it, not who that partner record is called now.
  partner_name   VARCHAR(200),

  -- The business date, which decides the fiscal period — not the instant the row was written.
  document_date  VARCHAR(10) NOT NULL,
  due_date       VARCHAR(10),

  currency_code  CHAR(3)     NOT NULL REFERENCES currencies(code),
  -- The rate used, frozen. A reprint must not re-convert at today's rate.
  exchange_rate_micro INTEGER NOT NULL DEFAULT 1000000 CHECK (exchange_rate_micro > 0),

  -- Totals, in the DOCUMENT's currency, minor units. Stored rather than summed on read: they are
  -- what was agreed, and a sum over lines would drift the moment a rounding rule changed.
  net_minor      INTEGER     NOT NULL DEFAULT 0,
  tax_minor      INTEGER     NOT NULL DEFAULT 0,
  discount_minor INTEGER     NOT NULL DEFAULT 0,
  total_minor    INTEGER     NOT NULL DEFAULT 0,
  -- What the goods cost us, frozen at posting. The figure the COGS entry uses and every margin
  -- report reads.
  cost_minor     INTEGER     NOT NULL DEFAULT 0,

  -- A held sale is a draft, parked (§2.6). It occupies no number and moves no stock.
  is_held        INTEGER     NOT NULL DEFAULT 0 CHECK (is_held IN (0, 1)),
  hold_label     VARCHAR(80),

  notes          TEXT,
  posted_at      VARCHAR(32),
  posted_by      CHAR(36)    REFERENCES users(id),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  created_by     CHAR(36)    REFERENCES users(id),
  updated_at     VARCHAR(32) NOT NULL,

  -- A posted document has a number; a draft does not. Both halves matter: the first stops a
  -- posting that skipped allocation, the second stops a draft that consumed one.
  CONSTRAINT ck_sales_posted_is_numbered CHECK (
    (status = 'posted' AND document_number IS NOT NULL) OR
    (status <> 'posted' AND document_number IS NULL)
  ),
  -- Only a draft can be held. A posted sale that claims to be parked is a contradiction.
  CONSTRAINT ck_sales_held_is_draft CHECK (is_held = 0 OR status = 'draft')
);

-- Two documents can never share a number (§9.4). The allocator prevents it; this makes it
-- unrepresentable — the pairing Phase 3 and 4 established for every invariant that matters.
CREATE UNIQUE INDEX ux_sales_documents_number
  ON sales_documents (company_id, document_type, document_number)
  WHERE document_number IS NOT NULL;

CREATE INDEX ix_sales_documents_partner ON sales_documents (partner_id, document_date);
CREATE INDEX ix_sales_documents_status ON sales_documents (company_id, status, document_date);
CREATE INDEX ix_sales_documents_source ON sales_documents (source_document_id);
CREATE INDEX ix_sales_documents_held ON sales_documents (branch_id, is_held);

-- ─────────────────────────────────────────────────────────────────────────────
-- sales_lines — where the snapshot lives
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE sales_lines (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  document_id    CHAR(36)    NOT NULL REFERENCES sales_documents(id) ON DELETE CASCADE,
  line_number    INTEGER     NOT NULL,

  product_id     CHAR(36)    NOT NULL REFERENCES products(id),
  variant_id     CHAR(36)    NOT NULL REFERENCES product_variants(id),

  -- ── the snapshot: what things were CALLED ────────────────────────────────
  --
  -- Redundant next to the foreign keys above, and deliberately so. A product renamed from
  -- "Cement 25kg" to "Cement 25kg (old)" must not rewrite last year's invoices.
  product_name   VARCHAR(200) NOT NULL,
  variant_sku    VARCHAR(60)  NOT NULL,
  uom_code       VARCHAR(40)  NOT NULL,

  -- ── DUAL QUANTITY (§B.3) ─────────────────────────────────────────────────
  --
  -- What the customer bought, in the unit they bought it in — and what left the warehouse, in
  -- the unit stock is held in. A reprinted invoice says "2 rolls" while inventory correctly
  -- moved 200 metres, and neither figure depends on a conversion factor that might be edited
  -- later.
  quantity_micro       INTEGER NOT NULL CHECK (quantity_micro > 0),
  uom_id               CHAR(36) NOT NULL REFERENCES units_of_measure(id),
  quantity_stock_micro INTEGER NOT NULL CHECK (quantity_stock_micro > 0),

  -- ── the snapshot: the money ──────────────────────────────────────────────
  unit_price_minor INTEGER   NOT NULL CHECK (unit_price_minor >= 0),
  -- WHICH price list answered, and at which quantity break. §2.6 of Phase 3 requires resolution
  -- to record its reason, and this is where the reason outlives the resolution.
  price_source     VARCHAR(40),
  price_list_code  VARCHAR(40),

  discount_minor   INTEGER   NOT NULL DEFAULT 0 CHECK (discount_minor >= 0),

  -- The RATE, not the tax group. A group's rate changes; what this line was taxed at does not.
  tax_rate_micro   INTEGER   NOT NULL DEFAULT 0,
  tax_code         VARCHAR(40),
  tax_amount_minor INTEGER   NOT NULL DEFAULT 0,

  net_minor        INTEGER   NOT NULL DEFAULT 0,
  total_minor      INTEGER   NOT NULL DEFAULT 0,

  -- What the goods cost us AT THE MOMENT OF SALE, from the costing port. Frozen: the average
  -- moves with every receipt, and a margin report run next year must use the cost that applied
  -- when the sale happened.
  cost_micro       INTEGER   NOT NULL DEFAULT 0,

  -- The stock movement this line produced, so a line and its inventory effect can be walked in
  -- either direction. Written at posting; NULL on a draft.
  movement_id      CHAR(36)  REFERENCES stock_movements(id),
  -- For a credit note line: the sale line it reverses. §D.3 requires a return to be costed at
  -- its ORIGINAL cost, and this is what makes that reachable.
  source_line_id   CHAR(36)  REFERENCES sales_lines(id),

  lot_id           CHAR(36)  REFERENCES stock_lots(id),
  serial_id        CHAR(36)  REFERENCES stock_serials(id),

  notes            VARCHAR(400),

  row_version      INTEGER   NOT NULL DEFAULT 1,
  created_at       VARCHAR(32) NOT NULL,
  updated_at       VARCHAR(32) NOT NULL
);

CREATE UNIQUE INDEX ux_sales_lines_number ON sales_lines (document_id, line_number);
CREATE INDEX ix_sales_lines_variant ON sales_lines (variant_id);
CREATE INDEX ix_sales_lines_movement ON sales_lines (movement_id);
CREATE INDEX ix_sales_lines_source ON sales_lines (source_line_id);
