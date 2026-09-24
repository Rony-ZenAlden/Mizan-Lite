-- 0012_invoice_suppliers_spoilage_usd.sql — what the owner asked for on 2026-09-24 (0.10.0).
--
--   §1 The shop's logo, kept INSIDE the database so a backup and a restore carry it.
--   §2 The printed invoice: a customer's city (الجهة), and how many of a product make one carton (طرد) — on the
--      product, and copied onto every sale line as the line keeps its name and unit.
--   §3 The A4 invoice, recorded in print_jobs like every other printed document.
--   §4 Suppliers, their purchases — lines, damage, discounts — and the payables book.
--   §5 What the shop owes its suppliers, taken off its capital.
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

-- ── §4. Suppliers, purchases, and what the shop owes them ───────────────────────────────────────────────────────────
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

-- ── §5. What the shop owes is part of what it is worth ──────────────────────────────────────────────────────────────
--
-- The capital in dollars (0011) was stock + drawer + what customers owe. What the shop owes its suppliers comes off it:
-- a shop that bought 1,000 dollars of stock on credit is not 1,000 dollars richer. Days recorded before 0.10.0 had no
-- suppliers, so nought is the truth for them, not a guess.
ALTER TABLE capital_snapshots ADD COLUMN payable_usd_minor BIGINT NOT NULL DEFAULT 0;
ALTER TABLE capital_snapshots ADD COLUMN payable_local_minor BIGINT NOT NULL DEFAULT 0;
