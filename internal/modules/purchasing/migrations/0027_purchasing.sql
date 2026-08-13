-- 0027_purchasing — purchase orders, goods receipts, and bills.
--
-- Sales owns 0023–0025 and identity took 0026, so purchasing owns 0027 (§10.3).
--
-- ─────────────────────────────────────────────────────────────────────────────
-- WHY THREE TABLES AND NOT ONE
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Sales models quotation → order → invoice as ONE table discriminated by type, because they are
-- the same document maturing: the same lines, the same quantities, one becoming the next.
--
-- Purchasing is not that shape, and copying the pattern would be the wrong kind of consistency.
-- An order, a delivery, and an invoice are three DIFFERENT documents describing three different
-- events that routinely disagree:
--
--   * one order arrives in three deliveries
--   * one invoice covers two orders
--   * the invoice arrives before the lorry
--   * 98 arrive where 100 were ordered
--
-- One table would need a self-reference per relationship and a status that means something
-- different per type, and the three-way match — ordered vs received vs billed — would have to be
-- expressed as a query over rows that are pretending to be each other.
--
-- The links live on the LINES rather than the documents, because the line is the grain at which
-- the quantities differ. A receipt line points at the order line it fills; a bill line points at
-- the receipt line it pays for.

-- ─────────────────────────────────────────────────────────────────────────────
-- purchase_orders — what we asked for
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Moves no stock and writes no journal entry. An intention is not a transaction, and a system
-- that books one has committed a business to a purchase it can still walk away from.

CREATE TABLE purchase_orders (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  branch_id      CHAR(36)    NOT NULL REFERENCES branches(id),
  -- Where the goods are expected. Needed on the ORDER, not just the receipt: a business with
  -- two warehouses orders for one of them, and a delivery that turns up at the other is a
  -- problem somebody has to solve on the loading bay.
  warehouse_id   CHAR(36)    NOT NULL REFERENCES warehouses(id),

  -- draft: editable, unnumbered, uncommitted.
  -- placed: sent to the supplier. Numbered. Still receivable against.
  -- closed: everything ordered has arrived, or somebody decided no more will. TERMINAL.
  -- cancelled: abandoned before placing. Terminal, and leaves no number consumed.
  status         VARCHAR(20) NOT NULL DEFAULT 'draft'
                 CHECK (status IN ('draft', 'placed', 'closed', 'cancelled')),

  -- NULL until placed (§9.4). An abandoned draft must consume no number.
  document_number VARCHAR(60),

  -- A supplier is REQUIRED, unlike a sale's walk-in customer. You cannot order from nobody, and
  -- the whole point of the document is that somebody has undertaken to deliver.
  partner_id     CHAR(36)    NOT NULL REFERENCES partners(id),
  -- Snapshotted for the same reason a sale snapshots its customer (§9.3): a reprint must say who
  -- we ordered from, not what that partner record is called now.
  partner_name   VARCHAR(200) NOT NULL,

  order_date     VARCHAR(10) NOT NULL,
  -- When the supplier said it would arrive. The column a "what is late" report reads, which is
  -- the report a purchasing manager opens first.
  expected_date  VARCHAR(10),

  currency_code  CHAR(3)     NOT NULL REFERENCES currencies(code),
  -- The rate AS AT the order, snapshotted (§18). A foreign-currency order re-priced by today's
  -- rate is a different order from the one that was placed.
  exchange_rate_micro INTEGER NOT NULL DEFAULT 1000000,

  net_minor      INTEGER     NOT NULL DEFAULT 0,
  tax_minor      INTEGER     NOT NULL DEFAULT 0,
  discount_minor INTEGER     NOT NULL DEFAULT 0,
  total_minor    INTEGER     NOT NULL DEFAULT 0,

  -- The supplier's own reference for this order, so a telephone call about it can start with a
  -- number they recognise.
  supplier_reference VARCHAR(60),
  notes          VARCHAR(1000),

  placed_at      VARCHAR(32),
  placed_by      CHAR(36)    REFERENCES users(id),
  closed_at      VARCHAR(32),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  created_by     CHAR(36)    REFERENCES users(id),
  updated_at     VARCHAR(32) NOT NULL,

  -- A draft has no number and anything placed has one. The same pair of constraints sales 0023
  -- carries, and for the same reason: the rule is kept by the allocator AND by the schema, so a
  -- code path that forgets it cannot write the row.
  CONSTRAINT ck_po_draft_unnumbered CHECK (
    status <> 'draft' OR document_number IS NULL
  ),
  CONSTRAINT ck_po_placed_numbered CHECK (
    status = 'draft' OR status = 'cancelled' OR document_number IS NOT NULL
  )
);

CREATE UNIQUE INDEX ux_purchase_orders_number
  ON purchase_orders (company_id, document_number)
  WHERE document_number IS NOT NULL;
CREATE INDEX ix_purchase_orders_partner ON purchase_orders (partner_id, status);
CREATE INDEX ix_purchase_orders_status ON purchase_orders (company_id, status, order_date);
-- The "what is late" index: open orders by the date they were promised.
CREATE INDEX ix_purchase_orders_expected
  ON purchase_orders (company_id, expected_date) WHERE status = 'placed';

-- ─────────────────────────────────────────────────────────────────────────────
-- purchase_order_lines
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE purchase_order_lines (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  order_id       CHAR(36)    NOT NULL REFERENCES purchase_orders(id) ON DELETE CASCADE,
  line_number    INTEGER     NOT NULL,

  product_id     CHAR(36)    NOT NULL REFERENCES products(id),
  variant_id     CHAR(36)    NOT NULL REFERENCES product_variants(id),

  -- The §9.3 snapshot columns. They look redundant next to variant_id and they are the
  -- difference between a reprint of what we ordered and a re-derivation of what that product is
  -- called now.
  product_name   VARCHAR(300) NOT NULL,
  variant_sku    VARCHAR(80)  NOT NULL,
  uom_code       VARCHAR(20)  NOT NULL,
  -- What the SUPPLIER calls it, as at the order. An order that goes out under our code gets
  -- filled wrong; a delivery note that comes back under theirs cannot be matched.
  supplier_code  VARCHAR(80),

  -- The unit the order is placed in, and the stock unit it converts to. Both, for the reason
  -- 5.2 gives: the day one is derived from the other is the day "2 rolls" and "200 metres" stop
  -- agreeing on a document that has already been sent.
  uom_id         CHAR(36)    NOT NULL REFERENCES units_of_measure(id),
  quantity_micro INTEGER     NOT NULL,
  quantity_stock_micro INTEGER NOT NULL,

  -- UnitAmount scale, 10^-6 of the MAJOR unit (§E). A purchase price per metre of cable is
  -- routinely a fraction of a minor unit, and rounding it before multiplying by 3,000 produces a
  -- materially wrong order value.
  unit_price_micro INTEGER   NOT NULL DEFAULT 0,
  discount_minor INTEGER     NOT NULL DEFAULT 0,
  tax_rate_micro INTEGER     NOT NULL DEFAULT 0,
  tax_code       VARCHAR(40),
  tax_amount_minor INTEGER   NOT NULL DEFAULT 0,
  net_minor      INTEGER     NOT NULL DEFAULT 0,
  total_minor    INTEGER     NOT NULL DEFAULT 0,

  -- How much of this line has actually arrived, maintained as receipts are confirmed.
  --
  -- A MAINTAINED PROJECTION, like stock_levels: the receipt lines are the truth and this is the
  -- answer to "what is still outstanding" without summing them on every screen. It is rebuildable
  -- from purchase_receipt_lines, and 6.2 ships the check that proves it agrees.
  received_micro INTEGER     NOT NULL DEFAULT 0,

  notes          VARCHAR(400),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  updated_at     VARCHAR(32) NOT NULL,

  -- Ordering nothing is a mistake with no reading that makes sense; a NEGATIVE order is a return
  -- wearing an order's clothes, and returns are their own document (6.6).
  CONSTRAINT ck_po_line_positive CHECK (quantity_micro > 0),
  -- Received is never negative. Un-receiving is a supplier return, not a smaller number here.
  CONSTRAINT ck_po_line_received CHECK (received_micro >= 0)
);

CREATE UNIQUE INDEX ux_po_lines_number ON purchase_order_lines (order_id, line_number);
CREATE INDEX ix_po_lines_order ON purchase_order_lines (order_id);
CREATE INDEX ix_po_lines_variant ON purchase_order_lines (variant_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- supplier_products — what the supplier calls it, and how they pack it
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Deliberately NOT a price list. Phase 3 owns pricing, and 3.6 already built resolution with a
-- `Purchase` direction that nothing has called — purchase prices belong there, where they get
-- the same tiers, dates, and partner scoping that sale prices get. Putting a price column here
-- would be a second pricing system that starts simple and grows into the first one.

CREATE TABLE supplier_products (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  partner_id     CHAR(36)    NOT NULL REFERENCES partners(id),
  variant_id     CHAR(36)    NOT NULL REFERENCES product_variants(id),

  -- Their code, on their catalogue and their delivery note.
  supplier_code  VARCHAR(80) NOT NULL,
  supplier_name  VARCHAR(300),

  -- How many stock units come in one of the supplier's packs. A supplier who sells cable by the
  -- 100m drum against our metre is the ordinary case, and an order for "3" that means 3 metres
  -- when they read 3 drums is a delivery nobody can put away.
  pack_quantity_micro INTEGER NOT NULL DEFAULT 1000000,

  -- How long they take, in days. Feeds the expected date rather than deciding it — a supplier's
  -- average is an estimate, and the buyer knows about the holiday next week.
  lead_time_days INTEGER,

  is_preferred   INTEGER     NOT NULL DEFAULT 0 CHECK (is_preferred IN (0, 1)),
  is_active      INTEGER     NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  updated_at     VARCHAR(32) NOT NULL,

  CONSTRAINT ck_supplier_pack_positive CHECK (pack_quantity_micro > 0)
);

-- One row per supplier per variant. The same supplier listing one product twice gives two codes
-- and no rule to choose between them.
CREATE UNIQUE INDEX ux_supplier_products
  ON supplier_products (partner_id, variant_id);
-- Their code, for matching an inbound delivery note. Not unique across suppliers: two suppliers
-- using the same code for different things is ordinary and harmless.
CREATE INDEX ix_supplier_products_code ON supplier_products (company_id, supplier_code);
CREATE INDEX ix_supplier_products_variant ON supplier_products (variant_id, is_preferred);
