-- 0012_invoice_suppliers_spoilage_usd.sql — what the owner asked for on 2026-09-24 (0.10.0).
--
--   §1 The shop's logo, kept INSIDE the database so a backup and a restore carry it.
--   §2 The printed invoice: a customer's city (الجهة), and how many of a product make one carton (طرد) — on the
--      product, and copied onto every sale line as the line keeps its name and unit.
--   §3 The A4 invoice, recorded in print_jobs like every other printed document.
--   §4 Stock that spoiled — food gone bad — written off under a reason of its own, beside damaged and expired.
--   §5 Suppliers, their purchases — lines, damage, discounts — and the payables book.
--   §6 What the shop owes its suppliers, taken off its capital.
--
-- ORDER MATTERS (as 0010 found): §4 rebuilds stock_ledger inside the runner's transaction, where foreign keys stay on.
-- stock_levels points at the ledger and is copied aside first; purchase_lines (§5) will point at it too, and is created
-- after the rebuild, so there are none of its rows to break.
--
-- Every nullable comparison in a CHECK is guarded (PROGRESS O7).

-- ── §1. The shop's logo ─────────────────────────────────────────────────────────────────────────────────────────────
--
-- One row or none. The logo is a PNG the application made from the owner's upload — at most 800 pixels on its longest
-- side — so a backup stays small and every printout draws the same picture.
--
-- Why not a file beside the database: a backup is the database. A logo kept as a separate file would be missing from
-- every restore, and a shop restored onto a new computer would print invoices with no logo and no reason given.
CREATE TABLE shop_logo (
  id          SMALLINT     NOT NULL PRIMARY KEY CHECK (id = 1),
  png         BLOB         NOT NULL CHECK (length(png) > 0),
  width       INTEGER      NOT NULL CHECK (width > 0),
  height      INTEGER      NOT NULL CHECK (height > 0),
  updated_at  CHAR(24)     NOT NULL
);

-- ── §2. The invoice ─────────────────────────────────────────────────────────────────────────────────────────────────
--
-- الجهة on an invoice is where the customer is — a town or a quarter. Optional, like the phone.
ALTER TABLE customers ADD COLUMN city VARCHAR(60) CHECK (city IS NULL OR length(trim(city)) > 0);

-- طرد: how many of the product's own unit make one carton — 6 dozen coffee cups, 24 thermos flasks — in millionths
-- of the unit, as every quantity is. An invoice's carton column is a sale line's quantity divided by it; a product
-- without one has no carton count.
--
-- Called a carton, not a package, on purpose: a package here (0003) is a PRODUCT that holds another — a 16-litre tin
-- of oil opened into litres — and is counted in stock. A carton is only how goods are counted on an invoice.
ALTER TABLE products ADD COLUMN units_per_carton_micro BIGINT
  CHECK (units_per_carton_micro IS NULL OR units_per_carton_micro > 0);

-- The sale line keeps the carton size it was sold at, as it keeps the name and the unit: an invoice printed again next
-- year must count the cartons that were sold, not the cartons the product is packed in by then.
ALTER TABLE sale_lines ADD COLUMN units_per_carton_micro BIGINT
  CHECK (units_per_carton_micro IS NULL OR units_per_carton_micro > 0);

-- ── §3. The A4 invoice is a printed document too ────────────────────────────────────────────────────────────────────
--
-- Recorded in print_jobs like every other print, and document_kind is a CHECK that has to learn it. An invoice is OF a
-- sale, so it has a subject. Nothing points at print_jobs, so the rebuild has no ordering hazard (0010 §3 did the same).
CREATE TABLE print_jobs_r12 (
  id             CHAR(36)     NOT NULL PRIMARY KEY,
  seq            BIGINT       NOT NULL CHECK (seq >= 1),
  document_kind  VARCHAR(16)  NOT NULL CHECK (document_kind IN ('sale', 'credit_sale', 'payment', 'refund', 'test', 'label', 'zreport', 'invoice')),
  subject_id     CHAR(36),
  copy_no        INTEGER      NOT NULL CHECK (copy_no >= 1),
  printer_name   VARCHAR(200) NOT NULL CHECK (length(trim(printer_name)) > 0),
  path           VARCHAR(8)   NOT NULL CHECK (path IN ('raw', 'driver')),
  outcome        VARCHAR(8)   NOT NULL CHECK (outcome IN ('sent', 'failed')),
  error_code     VARCHAR(80),
  printed_at     CHAR(24)     NOT NULL,
  CONSTRAINT ux_print_jobs_seq UNIQUE (seq),
  CONSTRAINT ck_print_jobs_subject CHECK (
    (document_kind IN ('test', 'zreport') AND subject_id IS NULL) OR
    (document_kind NOT IN ('test', 'zreport') AND subject_id IS NOT NULL)),
  CONSTRAINT ck_print_jobs_error CHECK ((outcome = 'failed' AND error_code IS NOT NULL AND length(error_code) > 0) OR (outcome = 'sent' AND error_code IS NULL))
);
INSERT INTO print_jobs_r12 SELECT * FROM print_jobs;
DROP TABLE print_jobs;
ALTER TABLE print_jobs_r12 RENAME TO print_jobs;
CREATE INDEX ix_print_jobs_subject ON print_jobs (subject_id, outcome);

-- ── §4. Stock that spoiled ──────────────────────────────────────────────────────────────────────────────────────────
--
-- The owner's spoilage screen (2026-09-24) writes stock off as damaged, expired or spoiled. 'spoiled' is food gone bad
-- before its date — a tub of labneh in the heat — which neither 'damaged' (broken, crushed) nor 'expired' (past its
-- date) says. reason_code is a CHECK, which SQLite cannot alter, so the ledger is rebuilt exactly as 0010 left it with
-- the one word added: stock_levels copied aside and put back, the new table's self-reference naming the NEW table, the
-- indexes and 0011's trigger made again.
CREATE TABLE stock_levels_copy AS SELECT * FROM stock_levels;
DROP TABLE stock_levels;

CREATE TABLE stock_ledger_r12 (
  id                         CHAR(36)    NOT NULL PRIMARY KEY,
  product_id                 CHAR(36)    NOT NULL REFERENCES products(id),
  seq                        BIGINT      NOT NULL CHECK (seq >= 1),
  business_date              CHAR(10)    NOT NULL,
  occurred_at                CHAR(24)    NOT NULL,
  kind                       VARCHAR(20) NOT NULL CHECK (kind IN (
                               'opening', 'receipt', 'receipt_reversal', 'count', 'adjustment',
                               'package_out', 'content_in', 'cost_correction', 'sale', 'sale_void', 'sale_return')),
  quantity_micro             BIGINT      NOT NULL,
  unit_cost_usd_micro        BIGINT      NOT NULL CHECK (unit_cost_usd_micro >= 0),
  on_hand_before_micro       BIGINT      NOT NULL,
  avg_cost_before_usd_micro  BIGINT      NOT NULL CHECK (avg_cost_before_usd_micro >= 0),
  on_hand_after_micro        BIGINT      NOT NULL,
  avg_cost_after_usd_micro   BIGINT      NOT NULL CHECK (avg_cost_after_usd_micro >= 0),
  entered_currency           CHAR(3)     REFERENCES currencies(code),
  entered_unit_cost_micro    BIGINT,
  local_per_usd_nano         BIGINT,
  reason_code                VARCHAR(16),
  note                       VARCHAR(200),
  reverses_id                CHAR(36)    REFERENCES stock_ledger_r12(id),
  pair_id                    CHAR(36),
  sale_id                    CHAR(36)    REFERENCES sales(id),
  sale_line_id               CHAR(36)    REFERENCES sale_lines(id),
  created_at                 CHAR(24)    NOT NULL,

  CONSTRAINT ck_ledger_quantity_by_kind CHECK (
    (kind IN ('opening', 'receipt', 'content_in', 'sale_void', 'sale_return') AND quantity_micro > 0) OR
    (kind IN ('receipt_reversal', 'package_out', 'sale')                     AND quantity_micro < 0) OR
    (kind = 'adjustment'                                                     AND quantity_micro <> 0) OR
    (kind = 'count') OR
    (kind = 'cost_correction'                                                AND quantity_micro = 0)),
  CONSTRAINT ck_ledger_after_follows_before CHECK (on_hand_after_micro = on_hand_before_micro + quantity_micro),
  CONSTRAINT ck_ledger_reason_where_needed CHECK (
    (kind IN ('count', 'adjustment')) = (reason_code IS NOT NULL)),
  CONSTRAINT ck_ledger_reason_known CHECK (
    reason_code IS NULL OR reason_code IN ('count', 'damaged', 'expired', 'spoiled', 'own_use', 'gift', 'other')),
  CONSTRAINT ck_ledger_other_needs_note CHECK (reason_code IS NULL OR reason_code <> 'other' OR note IS NOT NULL),
  CONSTRAINT ck_ledger_reversal_links CHECK ((kind IN ('receipt_reversal', 'sale_void')) = (reverses_id IS NOT NULL)),
  CONSTRAINT ck_ledger_package_pairs CHECK ((kind IN ('package_out', 'content_in')) = (pair_id IS NOT NULL)),
  CONSTRAINT ck_ledger_sale_links CHECK (
    (kind IN ('sale', 'sale_void', 'sale_return')) = (sale_id IS NOT NULL AND sale_line_id IS NOT NULL)),
  CONSTRAINT ck_ledger_sale_links_both_or_neither CHECK ((sale_id IS NULL) = (sale_line_id IS NULL)),
  CONSTRAINT ck_ledger_entered_cost CHECK (
    (entered_currency IS NULL AND entered_unit_cost_micro IS NULL AND local_per_usd_nano IS NULL) OR
    (kind IN ('opening', 'receipt') AND entered_unit_cost_micro IS NOT NULL AND entered_unit_cost_micro >= 0 AND (
       (entered_currency IS NOT NULL AND entered_currency = 'USD' AND local_per_usd_nano IS NULL) OR
       (entered_currency IS NOT NULL AND entered_currency <> 'USD' AND
        local_per_usd_nano IS NOT NULL AND local_per_usd_nano > 0)))),
  CONSTRAINT ux_ledger_reversed_once UNIQUE (reverses_id),
  CONSTRAINT ux_ledger_product_seq UNIQUE (product_id, seq)
);

INSERT INTO stock_ledger_r12 (
  id, product_id, seq, business_date, occurred_at, kind, quantity_micro, unit_cost_usd_micro,
  on_hand_before_micro, avg_cost_before_usd_micro, on_hand_after_micro, avg_cost_after_usd_micro,
  entered_currency, entered_unit_cost_micro, local_per_usd_nano, reason_code, note, reverses_id, pair_id,
  sale_id, sale_line_id, created_at)
SELECT
  id, product_id, seq, business_date, occurred_at, kind, quantity_micro, unit_cost_usd_micro,
  on_hand_before_micro, avg_cost_before_usd_micro, on_hand_after_micro, avg_cost_after_usd_micro,
  entered_currency, entered_unit_cost_micro, local_per_usd_nano, reason_code, note, reverses_id, pair_id,
  sale_id, sale_line_id, created_at
FROM stock_ledger;

DROP TABLE stock_ledger;
ALTER TABLE stock_ledger_r12 RENAME TO stock_ledger;

CREATE INDEX ix_stock_ledger_date ON stock_ledger (business_date);
CREATE INDEX ix_stock_ledger_sale ON stock_ledger (sale_id);
CREATE UNIQUE INDEX ux_stock_ledger_sale_line_kind ON stock_ledger (sale_line_id, kind)
  WHERE kind IN ('sale', 'sale_void');

-- 0011's guard, dropped with the old table.
CREATE TRIGGER tr_stock_ledger_no_open_price BEFORE INSERT ON stock_ledger
WHEN (SELECT open_price FROM products WHERE id = NEW.product_id) = 1
BEGIN
  SELECT RAISE(ABORT, 'an open-priced product holds no stock');
END;

CREATE TABLE stock_levels (
  product_id               CHAR(36)  NOT NULL PRIMARY KEY REFERENCES products(id),
  on_hand_micro            BIGINT    NOT NULL,
  avg_cost_usd_micro       BIGINT    NOT NULL CHECK (avg_cost_usd_micro >= 0),
  last_movement_id         CHAR(36)  NOT NULL REFERENCES stock_ledger(id),
  row_version              BIGINT    NOT NULL DEFAULT 1,
  updated_at               CHAR(24)  NOT NULL
);
INSERT INTO stock_levels (product_id, on_hand_micro, avg_cost_usd_micro, last_movement_id, row_version, updated_at)
  SELECT product_id, on_hand_micro, avg_cost_usd_micro, last_movement_id, row_version, updated_at FROM stock_levels_copy;
DROP TABLE stock_levels_copy;

-- ── §5. Suppliers, purchases, and what the shop owes them ───────────────────────────────────────────────────────────
--
-- A book of its own, separate from the customers' (the owner's request, 2026-09-24): a customer's balance is owed TO the
-- shop and a supplier's is owed BY it, and a book that held both would net one against the other.
CREATE TABLE suppliers (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  name         VARCHAR(100) NOT NULL CHECK (length(trim(name)) > 0),
  -- textkey.Normalise(name): one key for every spelling a reader calls the same name, as customers have.
  name_key     VARCHAR(100) NOT NULL CHECK (length(name_key) > 0),
  phone        VARCHAR(20),
  city         VARCHAR(60)  CHECK (city IS NULL OR length(trim(city)) > 0),
  note         VARCHAR(200),
  is_active    SMALLINT     NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  row_version  BIGINT       NOT NULL DEFAULT 1,
  created_at   CHAR(24)     NOT NULL,
  updated_at   CHAR(24)     NOT NULL,
  CONSTRAINT ux_suppliers_name_key UNIQUE (name_key)
);

-- A purchase is the supplier's invoice as it arrived: its lines, what was damaged, the discounts, and what it costs.
-- What it costs — due — is gross less the lines' discounts less the invoice's discount: the CHECK holds the sum.
CREATE TABLE purchases (
  id                      CHAR(36)     NOT NULL PRIMARY KEY,
  purchase_no             BIGINT       NOT NULL CHECK (purchase_no >= 1),
  supplier_id             CHAR(36)     NOT NULL REFERENCES suppliers(id),
  supplier_name_snapshot  VARCHAR(100) NOT NULL,
  business_date           CHAR(10)     NOT NULL,
  occurred_at             CHAR(24)     NOT NULL,
  currency                CHAR(3)      NOT NULL REFERENCES currencies(code),
  -- The rate a purchase in the local currency was paid at; none for dollars.
  local_per_usd_nano      BIGINT       CHECK (local_per_usd_nano IS NULL OR local_per_usd_nano > 0),
  supplier_ref            VARCHAR(40),
  gross_minor             BIGINT       NOT NULL CHECK (gross_minor >= 0),
  line_discount_minor     BIGINT       NOT NULL CHECK (line_discount_minor >= 0),
  invoice_discount_minor  BIGINT       NOT NULL CHECK (invoice_discount_minor >= 0),
  due_minor               BIGINT       NOT NULL CHECK (due_minor >= 0),
  paid_now_minor          BIGINT       NOT NULL CHECK (paid_now_minor >= 0),
  paid_from               VARCHAR(8)   CHECK (paid_from IS NULL OR paid_from IN ('drawer', 'owner')),
  status                  VARCHAR(8)   NOT NULL CHECK (status IN ('posted', 'voided')),
  voided_at               CHAR(24),
  void_reason             VARCHAR(200),
  note                    VARCHAR(200),
  row_version             BIGINT       NOT NULL DEFAULT 1,
  created_at              CHAR(24)     NOT NULL,
  CONSTRAINT ux_purchases_no UNIQUE (purchase_no),
  CONSTRAINT ck_purchases_due CHECK (due_minor = gross_minor - line_discount_minor - invoice_discount_minor),
  -- Money paid at the purchase says where it came from; no money, no source.
  CONSTRAINT ck_purchases_paid_from CHECK ((paid_now_minor = 0 AND paid_from IS NULL) OR (paid_now_minor > 0 AND paid_from IS NOT NULL)),
  CONSTRAINT ck_purchases_void CHECK (
    (status = 'posted' AND voided_at IS NULL AND void_reason IS NULL) OR
    (status = 'voided' AND voided_at IS NOT NULL AND void_reason IS NOT NULL AND length(trim(void_reason)) > 0))
);
CREATE INDEX ix_purchases_supplier ON purchases (supplier_id, business_date);
CREATE INDEX ix_purchases_date ON purchases (business_date);

-- A line: what arrived, what of it was damaged, the price, the discounts, and the receipt that took the good units into
-- stock. Damaged units are never charged for and never received (the owner's decision, 2026-09-24).
CREATE TABLE purchase_lines (
  id                      CHAR(36)     NOT NULL PRIMARY KEY,
  purchase_id             CHAR(36)     NOT NULL REFERENCES purchases(id),
  line_no                 INTEGER      NOT NULL CHECK (line_no >= 1),
  product_id              CHAR(36)     NOT NULL REFERENCES products(id),
  name_ar_snapshot        VARCHAR(200) NOT NULL,
  name_en_snapshot        VARCHAR(200),
  unit_code_snapshot      VARCHAR(16)  NOT NULL,
  quantity_micro          BIGINT       NOT NULL CHECK (quantity_micro > 0),
  damaged_micro           BIGINT       NOT NULL CHECK (damaged_micro >= 0 AND damaged_micro <= quantity_micro),
  unit_cost_micro         BIGINT       NOT NULL CHECK (unit_cost_micro >= 0),
  discount_percent_micro  BIGINT       NOT NULL CHECK (discount_percent_micro >= 0 AND discount_percent_micro <= 100000000),
  gross_minor             BIGINT       NOT NULL CHECK (gross_minor >= 0),
  line_discount_minor     BIGINT       NOT NULL CHECK (line_discount_minor >= 0 AND line_discount_minor <= gross_minor),
  invoice_share_minor     BIGINT       NOT NULL CHECK (invoice_share_minor >= 0),
  due_minor               BIGINT       NOT NULL CHECK (due_minor >= 0),
  stock_ledger_id         CHAR(36)     REFERENCES stock_ledger(id),
  CONSTRAINT ux_purchase_lines_no UNIQUE (purchase_id, line_no),
  CONSTRAINT ux_purchase_lines_receipt UNIQUE (stock_ledger_id),
  CONSTRAINT ck_purchase_lines_due CHECK (due_minor = gross_minor - line_discount_minor - invoice_share_minor),
  -- The good units were received, or there were none: a line wholly damaged has no receipt.
  CONSTRAINT ck_purchase_lines_receipt CHECK ((stock_ledger_id IS NULL) = (damaged_micro = quantity_micro))
);

-- The payables book, one chain per supplier and currency, as the customers' debt book is (0006): every entry holds the
-- balance before and after it, and the CHECKs refuse a chain that does not add up.
CREATE TABLE supplier_entries (
  id                      CHAR(36)     NOT NULL PRIMARY KEY,
  supplier_id             CHAR(36)     NOT NULL REFERENCES suppliers(id),
  currency                CHAR(3)      NOT NULL REFERENCES currencies(code),
  seq                     BIGINT       NOT NULL CHECK (seq >= 1),
  business_date           CHAR(10)     NOT NULL,
  occurred_at             CHAR(24)     NOT NULL,
  kind                    VARCHAR(10)  NOT NULL CHECK (kind IN ('opening', 'purchase', 'payment', 'refund', 'reversal')),
  amount_minor            BIGINT       NOT NULL CHECK (amount_minor <> 0),        -- + the shop owes more, − less
  balance_before_minor    BIGINT       NOT NULL,
  balance_after_minor     BIGINT       NOT NULL,
  purchase_id             CHAR(36)     REFERENCES purchases(id),
  reverses_id             CHAR(36)     REFERENCES supplier_entries(id),
  -- Where the money went or came from: the drawer, whose expected cash moves with it, or the owner's own money.
  cash_source             VARCHAR(8)   CHECK (cash_source IS NULL OR cash_source IN ('drawer', 'owner')),
  supplier_name_snapshot  VARCHAR(100) NOT NULL,
  note                    VARCHAR(200),
  created_at              CHAR(24)     NOT NULL,
  CONSTRAINT ux_supplier_entries_place    UNIQUE (supplier_id, currency, seq),
  CONSTRAINT ux_supplier_entries_reverses UNIQUE (reverses_id),
  CONSTRAINT ck_supplier_chain CHECK (balance_after_minor = balance_before_minor + amount_minor),
  CONSTRAINT ck_supplier_sign_matches_kind CHECK (
    (kind IN ('purchase', 'refund') AND amount_minor > 0) OR
    (kind = 'payment' AND amount_minor < 0) OR
     kind IN ('opening', 'reversal')),
  CONSTRAINT ck_supplier_purchase_names_it CHECK (kind <> 'purchase' OR purchase_id IS NOT NULL),
  CONSTRAINT ck_supplier_purchase_only_on_purchase_or_payment CHECK (purchase_id IS NULL OR kind IN ('purchase', 'payment')),
  CONSTRAINT ck_supplier_reversal_names_its_entry CHECK ((kind = 'reversal') = (reverses_id IS NOT NULL)),
  -- Cash moves on a payment and a refund, and on the reversal of one; never on an opening or a purchase.
  CONSTRAINT ck_supplier_cash_source CHECK (
    (kind IN ('payment', 'refund') AND cash_source IS NOT NULL) OR
    (kind IN ('opening', 'purchase') AND cash_source IS NULL) OR
     kind = 'reversal'),
  CONSTRAINT ck_supplier_reversal_has_a_reason CHECK (kind <> 'reversal' OR (note IS NOT NULL AND length(trim(note)) > 0))
);
CREATE INDEX ix_supplier_entries_day ON supplier_entries (business_date);

-- ── §6. What the shop owes is part of what it is worth ──────────────────────────────────────────────────────────────
--
-- The capital in dollars (0011) was stock + drawer + what customers owe. What the shop owes its suppliers comes off it:
-- a shop that bought 1,000 dollars of stock on credit is not 1,000 dollars richer. Days recorded before 0.10.0 had no
-- suppliers, so nought is the truth for them, not a guess.
ALTER TABLE capital_snapshots ADD COLUMN payable_usd_minor BIGINT NOT NULL DEFAULT 0;
ALTER TABLE capital_snapshots ADD COLUMN payable_local_minor BIGINT NOT NULL DEFAULT 0;
