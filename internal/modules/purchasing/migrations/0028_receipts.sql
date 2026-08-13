-- 0028_receipts — goods receipts: what actually arrived.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- WHY A RECEIPT IS ITS OWN DOCUMENT
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Because it happens at its own time, in its own quantity, and somebody signs for it.
--
-- The alternative — marking quantities received on the order itself — loses the one thing a
-- delivery note is for: WHICH delivery. A dispute about a short shipment three months later is
-- settled by "the second delivery, on the 14th, signed by Yusuf" and not by an order line whose
-- received figure has been edited four times with no record of by whom.
--
-- It also cannot express a delivery that arrives against NO order, which every business takes:
-- a supplier sends a replacement for damaged goods, or somebody buys from a cash-and-carry.
-- `order_id` is nullable for that reason, and the three-way match simply has one fewer side.

CREATE TABLE goods_receipts (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  company_id     CHAR(36)    NOT NULL REFERENCES companies(id),
  branch_id      CHAR(36)    NOT NULL REFERENCES branches(id),
  warehouse_id   CHAR(36)    NOT NULL REFERENCES warehouses(id),

  -- NULL where goods arrived against no order. Not an error: a replacement for damaged stock, or
  -- a cash-and-carry purchase, is a real delivery with a real cost.
  order_id       CHAR(36)    REFERENCES purchase_orders(id),

  -- draft: being entered. Nothing has moved.
  -- confirmed: TERMINAL. Numbered, stock moved, GRNI accrued.
  -- cancelled: abandoned before confirming. Terminal, moves nothing, consumes no number.
  status         VARCHAR(20) NOT NULL DEFAULT 'draft'
                 CHECK (status IN ('draft', 'confirmed', 'cancelled')),

  document_number VARCHAR(60),

  partner_id     CHAR(36)    NOT NULL REFERENCES partners(id),
  partner_name   VARCHAR(200) NOT NULL,

  receipt_date   VARCHAR(10) NOT NULL,

  -- The supplier's delivery-note number, as printed on the paper that came with the lorry. The
  -- single most useful field on this table for settling a dispute, and the one a supplier will
  -- quote back down the telephone.
  delivery_note_reference VARCHAR(60),
  -- Who signed for it. A name, snapshotted, because "who accepted this" is asked years later and
  -- the user record may be gone.
  received_by_name VARCHAR(200),

  currency_code  CHAR(3)     NOT NULL REFERENCES currencies(code),

  -- What the goods were VALUED at when they arrived, at the order's price (D3). The bill may
  -- disagree, and correcting that difference is 6.4's job — this is what was accrued.
  value_minor    INTEGER     NOT NULL DEFAULT 0,

  -- Set when a bill has taken this receipt up. A receipt can be billed only once: billing it
  -- twice pays a supplier twice for one delivery, which is precisely what the three-way match
  -- exists to prevent.
  billed_at      VARCHAR(32),
  bill_id        CHAR(36),

  notes          VARCHAR(1000),

  confirmed_at   VARCHAR(32),
  confirmed_by   CHAR(36)    REFERENCES users(id),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  created_by     CHAR(36)    REFERENCES users(id),
  updated_at     VARCHAR(32) NOT NULL,

  CONSTRAINT ck_gr_draft_unnumbered CHECK (
    status <> 'draft' OR document_number IS NULL
  ),
  CONSTRAINT ck_gr_confirmed_numbered CHECK (
    status <> 'confirmed' OR document_number IS NOT NULL
  ),
  -- A receipt that is billed must name the bill. Half a link is a link nobody can follow back.
  CONSTRAINT ck_gr_billed_pair CHECK (
    (billed_at IS NULL AND bill_id IS NULL) OR (billed_at IS NOT NULL AND bill_id IS NOT NULL)
  )
);

CREATE UNIQUE INDEX ux_goods_receipts_number
  ON goods_receipts (company_id, document_number)
  WHERE document_number IS NOT NULL;
CREATE INDEX ix_goods_receipts_order ON goods_receipts (order_id);
CREATE INDEX ix_goods_receipts_partner ON goods_receipts (partner_id, receipt_date);
-- The GRNI report: what has arrived and not yet been invoiced. A balance that ages here is the
-- thing a purchasing manager most needs to see.
CREATE INDEX ix_goods_receipts_unbilled
  ON goods_receipts (company_id, receipt_date) WHERE billed_at IS NULL;

-- ─────────────────────────────────────────────────────────────────────────────
-- goods_receipt_lines
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE goods_receipt_lines (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  receipt_id     CHAR(36)    NOT NULL REFERENCES goods_receipts(id) ON DELETE CASCADE,
  line_number    INTEGER     NOT NULL,

  -- Which order line this fills. NULL for a delivery against no order.
  --
  -- THE LINK IS HERE, on the line, and that is the whole reason receipts are separate documents:
  -- one delivery can fill parts of several order lines, and one order line can be filled by
  -- several deliveries. A document-level link cannot say either.
  order_line_id  CHAR(36)    REFERENCES purchase_order_lines(id),

  product_id     CHAR(36)    NOT NULL REFERENCES products(id),
  variant_id     CHAR(36)    NOT NULL REFERENCES product_variants(id),

  -- The §9.3 snapshot.
  product_name   VARCHAR(300) NOT NULL,
  variant_sku    VARCHAR(80)  NOT NULL,
  uom_code       VARCHAR(20)  NOT NULL,

  uom_id         CHAR(36)    NOT NULL REFERENCES units_of_measure(id),
  -- What arrived, in the receipt's unit and in the stock unit.
  quantity_micro INTEGER     NOT NULL,
  quantity_stock_micro INTEGER NOT NULL,

  -- What the goods were valued at, from the ORDER's price (D3). Snapshotted here rather than
  -- read from the order line, because the order line's price can still change before it is
  -- placed — and because a receipt against no order has no order line to read.
  unit_cost_micro INTEGER    NOT NULL DEFAULT 0,
  value_minor    INTEGER     NOT NULL DEFAULT 0,

  -- Lot and serial, for a tracked product. Phase 4 built both in 0021 and a receipt is where
  -- they are first CREATED — a batch enters the business here.
  lot_id         CHAR(36)    REFERENCES stock_lots(id),
  serial_id      CHAR(36)    REFERENCES stock_serials(id),

  -- The movement this line wrote, once confirmed. The link a supplier return follows back to
  -- cost itself at the ORIGINAL receipt's cost (§D.3).
  movement_id    CHAR(36)    REFERENCES stock_movements(id),

  notes          VARCHAR(400),

  row_version    INTEGER     NOT NULL DEFAULT 1,
  created_at     VARCHAR(32) NOT NULL,
  updated_at     VARCHAR(32) NOT NULL,

  -- Receiving nothing is not a delivery. Receiving a NEGATIVE quantity is a return, which is its
  -- own document with its own accounting.
  CONSTRAINT ck_gr_line_positive CHECK (quantity_micro > 0)
);

CREATE UNIQUE INDEX ux_gr_lines_number ON goods_receipt_lines (receipt_id, line_number);
CREATE INDEX ix_gr_lines_receipt ON goods_receipt_lines (receipt_id);
CREATE INDEX ix_gr_lines_order_line ON goods_receipt_lines (order_line_id);
CREATE INDEX ix_gr_lines_movement ON goods_receipt_lines (movement_id);
