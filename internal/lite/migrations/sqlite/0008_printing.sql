-- 0008_printing.sql — Phase L7: what was printed, and debt voucher numbers.
--
-- Design: docs/mizan_lite/phases/L7_HARDWARE_BACKUP.md §7. No existing table changes; printer and backup settings are rows of
-- the settings table. Insert-only. Every nullable comparison in a CHECK is guarded (PROGRESS O7).

-- A print job: which document, which copy, where it went, and whether the queue took it. A reprint's copy stamp is read from
-- here (D-L7.12).
CREATE TABLE print_jobs (
  id             CHAR(36)     NOT NULL PRIMARY KEY,
  seq            BIGINT       NOT NULL CHECK (seq >= 1),
  document_kind  VARCHAR(16)  NOT NULL CHECK (document_kind IN ('sale', 'credit_sale', 'payment', 'refund', 'test')),
  subject_id     CHAR(36),
  copy_no        INTEGER      NOT NULL CHECK (copy_no >= 1),
  printer_name   VARCHAR(200) NOT NULL CHECK (length(trim(printer_name)) > 0),
  path           VARCHAR(8)   NOT NULL CHECK (path IN ('raw', 'driver')),
  outcome        VARCHAR(8)   NOT NULL CHECK (outcome IN ('sent', 'failed')),
  error_code     VARCHAR(80),
  printed_at     CHAR(24)     NOT NULL,
  CONSTRAINT ux_print_jobs_seq UNIQUE (seq),
  CONSTRAINT ck_print_jobs_subject CHECK ((document_kind = 'test' AND subject_id IS NULL) OR (document_kind <> 'test' AND subject_id IS NOT NULL)),
  CONSTRAINT ck_print_jobs_error CHECK ((outcome = 'failed' AND error_code IS NOT NULL AND length(error_code) > 0) OR (outcome = 'sent' AND error_code IS NULL))
);
CREATE INDEX ix_print_jobs_subject ON print_jobs (subject_id, outcome);

-- A debt payment's or refund's voucher number (سند رقم), shop-wide and without gaps, assigned in the transaction that records
-- the entry (Q-L7.3).
CREATE TABLE voucher_numbers (
  entry_id    CHAR(36) NOT NULL PRIMARY KEY REFERENCES debt_entries(id),
  voucher_no  BIGINT   NOT NULL CHECK (voucher_no >= 1),
  CONSTRAINT ux_voucher_numbers_no UNIQUE (voucher_no)
);
