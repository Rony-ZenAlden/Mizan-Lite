-- DRAFT — not a migration. The stocktake (الجرد) the owner asked for on 2026-09-16, written while the cost price beside it
-- was built. It ships as migration 0010 with the module that uses it: a shop's database must not carry tables nothing
-- writes to. Kept here so the thinking is not lost.
--
-- The owner's answers of 2026-09-16 that shaped it: an audit is scoped by a SECTION the shopkeeper types (\"الرف العلوي\") —
-- chosen over a category scheme the catalogue does not have — and its money figures use the typed cost price.

-- ── The stocktake ──────────────────────────────────────────────────────────────────────────────────────────────────────

-- One walk of one section of the shop. `section` is free text the shopkeeper types ("الرف العلوي", "البراد") — the owner's
-- answer of 2026-09-16, chosen over a category scheme the catalogue does not have and might not fit.
--
-- `kind` is how often the walk is meant to happen, which is what the sheet is titled by; it constrains nothing.
--
-- An audit is OPEN while it is being counted and CLOSED when it is signed off. Only a closed audit moves stock: until then
-- the counts are a piece of paper, and a piece of paper has never changed what is on a shelf.
CREATE TABLE audits (
  id             CHAR(36)     NOT NULL PRIMARY KEY,
  seq            BIGINT       NOT NULL CHECK (seq >= 1),
  kind           VARCHAR(16)  NOT NULL CHECK (kind IN ('hourly', 'daily', 'weekly', 'monthly', 'section')),
  section        VARCHAR(200) NOT NULL DEFAULT '',
  status         VARCHAR(8)   NOT NULL CHECK (status IN ('open', 'closed', 'abandoned')),
  business_date  CHAR(10)     NOT NULL,
  started_at     CHAR(24)     NOT NULL,
  closed_at      CHAR(24),
  note           VARCHAR(500) NOT NULL DEFAULT '',
  -- The rate in force when the audit was closed, so its money figures can be read in pounds exactly as they were.
  rate_nano      BIGINT,
  local_currency CHAR(3),
  row_version    BIGINT       NOT NULL DEFAULT 1,
  CONSTRAINT ux_audits_seq UNIQUE (seq),
  CONSTRAINT ck_audits_closed CHECK (
    (status = 'open'      AND closed_at IS NULL) OR
    (status = 'abandoned' AND closed_at IS NOT NULL) OR
    (status = 'closed'    AND closed_at IS NOT NULL)),
  CONSTRAINT ck_audits_rate_pair CHECK ((rate_nano IS NULL) = (local_currency IS NULL)),
  CONSTRAINT ck_audits_rate_positive CHECK (rate_nano IS NULL OR rate_nano > 0)
);
CREATE INDEX ix_audits_status ON audits (status, business_date);

-- One product on one audit sheet: what the books said, what was found, and what the difference is worth.
--
-- `system_micro` is snapshotted when the line is added, not read at closing time: the sheet must say what the books said
-- when the shopkeeper started walking, or the variance is measured against a moving target.
--
-- `counted_micro` is NULL until someone writes a number. A line never counted is not a line counted as zero — the difference
-- between "the shelf was empty" and "nobody looked" is the whole point of an audit.
CREATE TABLE audit_lines (
  id                 CHAR(36) NOT NULL PRIMARY KEY,
  audit_id           CHAR(36) NOT NULL REFERENCES audits(id) ON DELETE CASCADE,
  product_id         CHAR(36) NOT NULL REFERENCES products(id),
  line_no            INTEGER  NOT NULL CHECK (line_no >= 1),
  name_snapshot      VARCHAR(200) NOT NULL,
  unit_snapshot      VARCHAR(16)  NOT NULL,
  unit_decimals      INTEGER  NOT NULL CHECK (unit_decimals BETWEEN 0 AND 3),
  system_micro       BIGINT   NOT NULL,
  counted_micro      BIGINT,
  -- The cost one unit was carrying when the line was added: the typed cost price where there is one, the weighted average
  -- otherwise, and NULL when the shop has never known what this product costs.
  unit_cost_usd_micro BIGINT,
  cost_known         SMALLINT NOT NULL DEFAULT 0 CHECK (cost_known IN (0, 1)),
  counted_at         CHAR(24),
  CONSTRAINT ux_audit_lines_product UNIQUE (audit_id, product_id),
  CONSTRAINT ux_audit_lines_no      UNIQUE (audit_id, line_no),
  CONSTRAINT ck_audit_lines_counted CHECK ((counted_micro IS NULL) = (counted_at IS NULL)),
  CONSTRAINT ck_audit_lines_counted_sign CHECK (counted_micro IS NULL OR counted_micro >= 0),
  CONSTRAINT ck_audit_lines_cost_pair CHECK (
    (cost_known = 1 AND unit_cost_usd_micro IS NOT NULL) OR
    (cost_known = 0 AND unit_cost_usd_micro IS NULL))
);
CREATE INDEX ix_audit_lines_audit ON audit_lines (audit_id, line_no);

-- The stock movement a closed audit made, so a line and the ledger row it caused can be read back to each other. One row per
-- line that moved; a line counted exactly right moves nothing and has none.
CREATE TABLE audit_movements (
  line_id     CHAR(36) NOT NULL PRIMARY KEY REFERENCES audit_lines(id) ON DELETE CASCADE,
  movement_id CHAR(36) NOT NULL REFERENCES stock_ledger(id),
  CONSTRAINT ux_audit_movements_movement UNIQUE (movement_id)
);
