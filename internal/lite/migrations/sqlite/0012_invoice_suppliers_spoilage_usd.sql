-- 0012_invoice_suppliers_spoilage_usd.sql — what the owner asked for on 2026-09-24 (0.10.0).
--
--   §1 The shop's logo, kept INSIDE the database so a backup and a restore carry it.
--   §2 The printed invoice: a customer's city (الجهة), and how many of a product make one carton (طرد) — on the
--      product, and copied onto every sale line as the line keeps its name and unit.
--   §3 The A4 invoice, recorded in print_jobs like every other printed document.
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
