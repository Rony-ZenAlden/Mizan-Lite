-- 0010_returns_reorder_recurrence.sql — three things the owner asked for on 2026-09-20, after using 0.9.6.
--
--   1. A partial sales return: the customer brings back two of the three tins, not the whole receipt.
--   2. A reorder level per product, so the catalogue and the till can warn before the shelf is empty.
--   3. An expense that recurs — rent, electricity — told apart from the day's small change.
--
-- What is NOT here: expiry dates. A pantry shop holds stock from several deliveries at once, so one date on the product
-- would describe whichever delivery was typed last and quietly call the older stock fresh. Expiry needs a batch, and a
-- batch changes how every sale draws stock. The owner chose (2026-09-20) to ship the reorder level now and design expiry
-- against a batch model later, rather than ship a date that lies.
--
-- ORDER MATTERS, and the first cut of this migration got it wrong. Three tables are rebuilt to widen a CHECK, and the
-- runner's transaction keeps foreign keys ON and cannot switch them off. Dropping a parent whose children hold rows
-- fails — which an empty database never shows, and the upgrade matrix caught on a seeded one (voucher_numbers points
-- at debt_entries, and a shop that has taken a payment has rows in it). So:
--
--   §1 rebuild stock_ledger   — stock_levels copied aside first, as 0005 did
--   §2 rebuild debt_entries   — voucher_numbers copied aside first, for the same reason
--   §3 rebuild print_jobs     — nothing points at it
--   §4 create the return tables, now that debt_entries is the table they will point at for good
--   §5, §6 the two plain columns
--
-- Every nullable comparison in a CHECK is guarded (PROGRESS O7).

-- ── §1. The stock ledger learns a partial return ────────────────────────────────────────────────────────────────────
--
-- stock_ledger's kind is a CHECK, and SQLite cannot alter one. stock_levels points at the ledger, so it is copied
-- aside and put back, exactly as 0005 rebuilt the same table.
--
-- Why 'sale_return' is not 'sale_void'. A void reverses a sale line whole and once, which is why the ledger holds
-- ux_ledger_reversed_once on it. A line can be returned twice — one tin today, another next week — and each time it is
-- a NEW movement, not a reversal of the old one. Sharing the kind would either break that uniqueness or make the
-- second return impossible.
CREATE TABLE stock_levels_copy AS SELECT * FROM stock_levels;
DROP TABLE stock_levels;

CREATE TABLE stock_ledger_r10 (
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
  reverses_id                CHAR(36)    REFERENCES stock_ledger_r10(id),
  pair_id                    CHAR(36),
  sale_id                    CHAR(36)    REFERENCES sales(id),
  sale_line_id               CHAR(36)    REFERENCES sale_lines(id),
  created_at                 CHAR(24)    NOT NULL,

  -- A return puts goods back, so it is positive, like a void.
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
    reason_code IS NULL OR reason_code IN ('count', 'damaged', 'expired', 'own_use', 'gift', 'other')),
  CONSTRAINT ck_ledger_other_needs_note CHECK (reason_code IS NULL OR reason_code <> 'other' OR note IS NOT NULL),
  -- A return is NOT a reversal: it names no row it undoes, and two returns of one line are two movements.
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

INSERT INTO stock_ledger_r10 (
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
ALTER TABLE stock_ledger_r10 RENAME TO stock_ledger;

CREATE INDEX ix_stock_ledger_date ON stock_ledger (business_date);
CREATE INDEX ix_stock_ledger_sale ON stock_ledger (sale_id);

-- 0005's ux_stock_ledger_sale_line_kind held one sale and one void per line. That invariant is still right for those
-- two — a line is sold once and voided at most once — but it is exactly wrong for a return, which may happen twice for
-- one line: one tin today, another next week. So the uniqueness is now PARTIAL, covering the two kinds it was written
-- for and leaving returns to the running total the service checks against what was sold.
CREATE UNIQUE INDEX ux_stock_ledger_sale_line_kind ON stock_ledger (sale_line_id, kind)
  WHERE kind IN ('sale', 'sale_void');

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

-- ── §2. The debt ledger learns a return ─────────────────────────────────────────────────────────────────────────────
--
-- A return settled against a credit customer's balance has to be a kind of its own.
--
-- 'write_off' would have fitted the shape — it reduces a balance and needs a reason — and would have been wrong: the
-- reports count write-offs as BAD DEBTS (reports/domain.BadDebtsOn). A customer handing goods back has not failed to
-- pay, and a day's statement that called it a bad debt would both slander the customer and count the same event twice.
--
-- 'payment' would have been wrong for a plainer reason: its CHECK requires tendered, change and a rate, because a
-- payment is cash crossing the counter. No cash crosses the counter here; goods do.
--
-- sale_id stays NULL on a return: the sale it belongs to is named the other way round, by sale_returns.debt_entry_id.
-- That also keeps ux_debt_entries_one_charge_per_sale — one CHARGE per sale — true without weakening it.
--
-- The balance MAY go below zero. A customer who owes 2,000 and brings back 5,000 of goods is owed 3,000, and that is
-- what 'refund' is for. Forbidding it would leave the shop unable to say what had happened.
--
-- voucher_numbers points at this table and a shop that has taken one payment has rows in it, so it goes aside first.
CREATE TABLE voucher_numbers_copy AS SELECT * FROM voucher_numbers;
DROP TABLE voucher_numbers;

CREATE TABLE debt_entries_r10 (
  id                     CHAR(36)     NOT NULL PRIMARY KEY,
  customer_id            CHAR(36)     NOT NULL REFERENCES customers(id),
  currency               CHAR(3)      NOT NULL REFERENCES currencies(code),
  seq                    BIGINT       NOT NULL CHECK (seq >= 1),
  business_date          CHAR(10)     NOT NULL,
  occurred_at            CHAR(24)     NOT NULL,
  kind                   VARCHAR(12)  NOT NULL
                           CHECK (kind IN ('opening', 'charge', 'payment', 'write_off', 'refund', 'reversal', 'sale_return')),
  amount_minor           BIGINT       NOT NULL CHECK (amount_minor <> 0),
  balance_before_minor   BIGINT       NOT NULL,
  balance_after_minor    BIGINT       NOT NULL,
  sale_id                CHAR(36)     REFERENCES sales(id),
  reverses_id            CHAR(36)     REFERENCES debt_entries_r10(id),
  fx_rate_id             CHAR(36)     REFERENCES fx_rates(id),
  local_per_usd_nano     BIGINT,
  cash_note_minor        BIGINT,
  tendered_currency      CHAR(3)      REFERENCES currencies(code),
  tendered_minor         BIGINT,
  change_currency        CHAR(3)      REFERENCES currencies(code),
  change_minor           BIGINT,
  customer_name_snapshot VARCHAR(100) NOT NULL,
  note                   VARCHAR(200),
  created_at             CHAR(24)     NOT NULL,
  CONSTRAINT ux_debt_entries_place    UNIQUE (customer_id, currency, seq),
  CONSTRAINT ux_debt_entries_reverses UNIQUE (reverses_id),
  CONSTRAINT ck_debt_chain CHECK (balance_after_minor = balance_before_minor + amount_minor),
  CONSTRAINT ck_debt_sign_matches_kind CHECK (
    (kind IN ('opening', 'charge', 'refund')        AND amount_minor > 0) OR
    (kind IN ('payment', 'write_off', 'sale_return') AND amount_minor < 0) OR
     kind = 'reversal'),
  CONSTRAINT ck_debt_charge_names_its_sale    CHECK ((kind = 'charge')   = (sale_id IS NOT NULL)),
  CONSTRAINT ck_debt_reversal_names_its_entry CHECK ((kind = 'reversal') = (reverses_id IS NOT NULL)),
  CONSTRAINT ck_debt_cash_moves_only_on_payment_and_refund CHECK (
    (kind IN ('payment', 'refund')
       AND tendered_currency IS NOT NULL AND tendered_minor IS NOT NULL AND tendered_minor > 0
       AND change_currency IS NOT NULL AND change_minor IS NOT NULL AND change_minor >= 0
       AND fx_rate_id IS NOT NULL AND local_per_usd_nano IS NOT NULL AND local_per_usd_nano > 0
       AND cash_note_minor IS NOT NULL AND cash_note_minor >= 1) OR
    (kind NOT IN ('payment', 'refund')
       AND tendered_currency IS NULL AND tendered_minor IS NULL AND change_currency IS NULL AND change_minor IS NULL
       AND fx_rate_id IS NULL AND local_per_usd_nano IS NULL AND cash_note_minor IS NULL)),
  CONSTRAINT ck_debt_owner_acts_have_a_reason CHECK (
    kind NOT IN ('write_off', 'refund', 'reversal', 'sale_return') OR (note IS NOT NULL AND length(trim(note)) > 0)),
  CONSTRAINT ck_debt_payment_never_below_zero CHECK (kind NOT IN ('payment', 'write_off') OR balance_after_minor >= 0),
  CONSTRAINT ck_debt_refund_only_what_is_owed_back CHECK (
    kind <> 'refund' OR (balance_before_minor < 0 AND balance_after_minor <= 0))
);

INSERT INTO debt_entries_r10 SELECT * FROM debt_entries;
DROP TABLE debt_entries;
ALTER TABLE debt_entries_r10 RENAME TO debt_entries;

CREATE UNIQUE INDEX ux_debt_entries_one_charge_per_sale ON debt_entries (sale_id);
CREATE INDEX ix_debt_entries_business_date ON debt_entries (business_date);

CREATE TABLE voucher_numbers (
  entry_id    CHAR(36) NOT NULL PRIMARY KEY REFERENCES debt_entries(id),
  voucher_no  BIGINT   NOT NULL CHECK (voucher_no >= 1),
  CONSTRAINT ux_voucher_numbers_no UNIQUE (voucher_no)
);
INSERT INTO voucher_numbers (entry_id, voucher_no) SELECT entry_id, voucher_no FROM voucher_numbers_copy;
DROP TABLE voucher_numbers_copy;

-- ── §3. Two more things a shop prints ───────────────────────────────────────────────────────────────────────────────
--
-- A shelf label with a barcode, and the end-of-day Z-report on 80 mm paper (2026-09-20). Both are printed documents,
-- so both are recorded in print_jobs like every other, and document_kind is a CHECK that has to learn them.
--
-- Neither has a subject the way a receipt has a sale: a label is OF a product, and the Z-report is of a day. The
-- subject constraint is widened to say so rather than forcing a product id into a column that means "the thing this
-- document is a copy of".
CREATE TABLE print_jobs_r10 (
  id             CHAR(36)     NOT NULL PRIMARY KEY,
  seq            BIGINT       NOT NULL CHECK (seq >= 1),
  document_kind  VARCHAR(16)  NOT NULL CHECK (document_kind IN ('sale', 'credit_sale', 'payment', 'refund', 'test', 'label', 'zreport')),
  subject_id     CHAR(36),
  copy_no        INTEGER      NOT NULL CHECK (copy_no >= 1),
  printer_name   VARCHAR(200) NOT NULL CHECK (length(trim(printer_name)) > 0),
  path           VARCHAR(8)   NOT NULL CHECK (path IN ('raw', 'driver')),
  outcome        VARCHAR(8)   NOT NULL CHECK (outcome IN ('sent', 'failed')),
  error_code     VARCHAR(80),
  printed_at     CHAR(24)     NOT NULL,
  CONSTRAINT ux_print_jobs_seq UNIQUE (seq),
  -- A label is OF a product and a Z-report is of a day, so the Z-report has no subject, as the test page has none.
  CONSTRAINT ck_print_jobs_subject CHECK (
    (document_kind IN ('test', 'zreport') AND subject_id IS NULL) OR
    (document_kind NOT IN ('test', 'zreport') AND subject_id IS NOT NULL)),
  CONSTRAINT ck_print_jobs_error CHECK ((outcome = 'failed' AND error_code IS NOT NULL AND length(error_code) > 0) OR (outcome = 'sent' AND error_code IS NULL))
);
INSERT INTO print_jobs_r10 SELECT * FROM print_jobs;
DROP TABLE print_jobs;
ALTER TABLE print_jobs_r10 RENAME TO print_jobs;
CREATE INDEX ix_print_jobs_subject ON print_jobs (subject_id, outcome);

-- ── §4. Returns ─────────────────────────────────────────────────────────────────────────────────────────────────────
--
-- A return is its own document, not a negative sale. sale_lines holds quantity_micro > 0 and every amount >= 0, and
-- those CHECKs are real invariants — a sale is a thing that happened, and no part of it is below nothing. Loosening
-- them so a return could borrow the table would weaken the constraint on ten thousand honest rows to accommodate a
-- different kind of row. So returns are their own pair of tables, which the reports read as their own fact.
--
-- A return carries its OWN business date. The customer brings the tin back on Thursday for a sale made on Monday, and
-- Thursday's takings are what change. Monday's receipt is not rewritten; it happened.
CREATE TABLE sale_returns (
  id                 CHAR(36)    NOT NULL PRIMARY KEY,
  return_no          BIGINT      NOT NULL CHECK (return_no >= 1),
  sale_id            CHAR(36)    NOT NULL REFERENCES sales(id),
  business_date      CHAR(10)    NOT NULL,
  returned_at        CHAR(24)    NOT NULL,
  -- How the money went back: cash out of the drawer, or off what the customer owes.
  settlement         VARCHAR(6)  NOT NULL CHECK (settlement IN ('cash', 'debt')),
  settlement_currency CHAR(3)    NOT NULL REFERENCES currencies(code),
  -- What the customer got back, in the settlement currency, and the same sum in each currency of the books.
  refund_minor       BIGINT      NOT NULL CHECK (refund_minor >= 0),
  refund_local_minor BIGINT      NOT NULL CHECK (refund_local_minor >= 0),
  refund_usd_minor   BIGINT      NOT NULL CHECK (refund_usd_minor >= 0),
  -- The cost put back on the shelf, so the day's profit is reduced by the margin and not by the whole price.
  cost_usd_minor     BIGINT      NOT NULL CHECK (cost_usd_minor >= 0),
  cost_local_minor   BIGINT      NOT NULL CHECK (cost_local_minor >= 0),
  cost_known         SMALLINT    NOT NULL CHECK (cost_known IN (0, 1)),
  -- The rate the refund was converted at: the rate in force when the goods came back, not when they were sold.
  fx_rate_id         CHAR(36)    NOT NULL REFERENCES fx_rates(id),
  local_per_usd_nano BIGINT      NOT NULL CHECK (local_per_usd_nano > 0),
  -- A debt return names the customer whose balance it reduced; a cash one names nobody.
  customer_id        CHAR(36)    REFERENCES customers(id),
  -- A DEBT return moves money through the debt ledger, which is where a balance lives, and names the entry it wrote.
  --
  -- A CASH return writes no cash entry, and deliberately: a cash SALE does not write one either. The drawer derives
  -- what the till took from the sales themselves (reports/domain.cashMoves), so a refund that wrote a cash entry would
  -- be counted once as an entry and once as a return, and the drawer would be short by the refund twice over.
  debt_entry_id      CHAR(36)    REFERENCES debt_entries(id),
  reason             VARCHAR(200) NOT NULL,
  created_at         CHAR(24)    NOT NULL,
  CONSTRAINT ux_sale_returns_no UNIQUE (return_no),
  CONSTRAINT ck_sale_return_settlement_is_complete CHECK (
    (settlement = 'debt' AND customer_id IS NOT NULL AND debt_entry_id IS NOT NULL) OR
    (settlement = 'cash' AND customer_id IS NULL     AND debt_entry_id IS NULL)),
  CONSTRAINT ck_sale_return_unknown_cost_is_zero CHECK (
    cost_known = 1 OR (cost_usd_minor = 0 AND cost_local_minor = 0)),
  CONSTRAINT ck_sale_return_has_a_reason CHECK (length(trim(reason)) > 0)
);
CREATE INDEX ix_sale_returns_sale ON sale_returns (sale_id);
CREATE INDEX ix_sale_returns_date ON sale_returns (business_date);

-- One line of a return, against the sale line it came from. The quantity is what came back on THIS return; a customer
-- may bring one tin back today and the second next week, and the service holds the running total to what was sold.
CREATE TABLE sale_return_lines (
  id                 CHAR(36) NOT NULL PRIMARY KEY,
  return_id          CHAR(36) NOT NULL REFERENCES sale_returns(id),
  sale_line_id       CHAR(36) NOT NULL REFERENCES sale_lines(id),
  line_no            SMALLINT NOT NULL CHECK (line_no >= 1),
  product_id         CHAR(36) NOT NULL REFERENCES products(id),
  quantity_micro     BIGINT   NOT NULL CHECK (quantity_micro > 0),
  -- The share of that sale line's net (after its discount) that this quantity represents, in both currencies.
  refund_local_minor BIGINT   NOT NULL CHECK (refund_local_minor >= 0),
  refund_usd_minor   BIGINT   NOT NULL CHECK (refund_usd_minor >= 0),
  -- The cost that goes back on the shelf, at the cost the SALE snapshotted — not today's average (D-L9.1's reasoning).
  unit_cost_usd_micro BIGINT  NOT NULL CHECK (unit_cost_usd_micro >= 0),
  cost_known         SMALLINT NOT NULL CHECK (cost_known IN (0, 1)),
  cost_usd_minor     BIGINT   NOT NULL CHECK (cost_usd_minor >= 0),
  cost_local_minor   BIGINT   NOT NULL CHECK (cost_local_minor >= 0),
  -- Whether the goods went back on the shelf. A damaged tin is refunded but not resold.
  restocked          SMALLINT NOT NULL CHECK (restocked IN (0, 1)),
  CONSTRAINT ux_sale_return_lines_line UNIQUE (return_id, line_no),
  CONSTRAINT ck_sale_return_line_unknown_cost_is_zero CHECK (
    cost_known = 1 OR (unit_cost_usd_micro = 0 AND cost_usd_minor = 0 AND cost_local_minor = 0))
);
CREATE INDEX ix_sale_return_lines_return    ON sale_return_lines (return_id);
CREATE INDEX ix_sale_return_lines_sale_line ON sale_return_lines (sale_line_id);

-- ── §5. The reorder level ────────────────────────────────────────────────────────────────────────────────────────────
--
-- Nullable, like the cost price and for the same reason: a shop that has set no level is not saying the level is zero.
-- Where it is NULL the product is never called low, and the badge does not appear.
ALTER TABLE products ADD COLUMN reorder_micro BIGINT;

CREATE TRIGGER tr_products_reorder_positive_insert BEFORE INSERT ON products
WHEN NEW.reorder_micro IS NOT NULL AND NEW.reorder_micro <= 0
BEGIN
  SELECT RAISE(ABORT, 'a reorder level is more than nothing');
END;
CREATE TRIGGER tr_products_reorder_positive_update BEFORE UPDATE ON products
WHEN NEW.reorder_micro IS NOT NULL AND NEW.reorder_micro <= 0
BEGIN
  SELECT RAISE(ABORT, 'a reorder level is more than nothing');
END;

-- ── §6. Recurrence on an expense ─────────────────────────────────────────────────────────────────────────────────────
--
-- 'once' is the day's small change — نثريات. 'monthly' is rent, the electricity bill, the wages: money the shop pays on a
-- cycle, which a day's report should not make look like a bad day.
--
-- Nothing is posted automatically. The shop records the rent when it pays the rent; marking it monthly tells the reports
-- how to read it, and nothing else. An application that posted money movements nobody typed would put entries in the
-- drawer on days the shopkeeper had not paid, and the drawer has to match the cash in it.
--
-- NOT NULL DEFAULT 'once': every expense already recorded is a one-off, which is what it was.
ALTER TABLE cash_entries ADD COLUMN recurrence VARCHAR(7) NOT NULL DEFAULT 'once'
  CHECK (recurrence IN ('once', 'monthly'));

-- Only an expense recurs. A deposit, a withdrawal, a count or a reversal is an event, not a cycle.
CREATE TRIGGER tr_cash_recurrence_only_on_expense_insert BEFORE INSERT ON cash_entries
WHEN NEW.recurrence <> 'once' AND NEW.kind <> 'expense'
BEGIN
  SELECT RAISE(ABORT, 'only an expense recurs');
END;
CREATE TRIGGER tr_cash_recurrence_only_on_expense_update BEFORE UPDATE ON cash_entries
WHEN NEW.recurrence <> 'once' AND NEW.kind <> 'expense'
BEGIN
  SELECT RAISE(ABORT, 'only an expense recurs');
END;
