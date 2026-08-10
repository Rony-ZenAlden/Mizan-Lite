-- 0011_journal — the general ledger itself (§20.2).
--
-- Owned by the accounting module. 0010 built the chart; this builds what posts to it.
--
-- # The one inviolable rule
--
-- For every entry, SUM(debit_minor) = SUM(credit_minor) in the functional currency. It is
-- enforced in three places on purpose (Phase 2 §2.2): the domain aggregate, which cannot be
-- constructed unbalanced; the posting service, because a repository is a separate trust
-- boundary; and a periodic job, because the first two only protect the paths that go through
-- them and a restore or a manual database edit does not.
--
-- SQL enforces the per-line half of it — a line is debit XOR credit, and neither is negative.
-- The cross-line sum is not expressible as a row CHECK, which is precisely why it has three
-- guards in Go rather than one comment here.
--
-- # Immutability
--
-- A posted entry is never modified or deleted. Corrections create a REVERSING entry linked by
-- reversal_of_entry_id. That is what makes a general ledger legally defensible, and it is the
-- same append-only reasoning the audit trail follows (§15.1) — the repository offers no update
-- and no delete for a posted entry, which is a stronger guarantee than a convention.

CREATE TABLE journal_entries (
  id                   CHAR(36)     NOT NULL PRIMARY KEY,
  company_id           CHAR(36)     NOT NULL REFERENCES companies(id),

  -- Unique and sequential, NOT gapless (§9.4, decision 4). Allocated at posting, inside the
  -- transaction. Gapless numbering requires serialising every posting attempt including the
  -- ones that fail, which trades throughput and complexity for a property only some
  -- jurisdictions demand — and `is_gapless` is a per-series flag for those, in Phase 5.
  entry_number         VARCHAR(40)  NOT NULL,

  -- A business DATE, not an instant (§7.5). Which day the transaction belongs to is a decision
  -- a human makes; it is not the moment a row was written, and in a shop open past midnight the
  -- two routinely disagree.
  entry_date           CHAR(10)     NOT NULL,          -- YYYY-MM-DD
  fiscal_period_id     CHAR(36)     NOT NULL REFERENCES fiscal_periods(id),

  status               VARCHAR(10)  NOT NULL,          -- draft|posted|reversed

  -- Traceability back to whatever caused this entry (§20.2). A ledger nobody can trace to its
  -- source documents is a ledger nobody can audit.
  source_module        VARCHAR(40)  NOT NULL,          -- 'sales' | 'manual' | …
  source_document_type VARCHAR(60),
  source_document_id   CHAR(36),

  -- Set on the CORRECTION, pointing at what it reverses. The reversed entry is marked
  -- 'reversed' but otherwise untouched — it remains exactly what was posted.
  reversal_of_entry_id CHAR(36)     REFERENCES journal_entries(id),

  memo                 VARCHAR(400),
  branch_id            CHAR(36)     REFERENCES branches(id),

  posted_at            CHAR(24),
  posted_by            CHAR(36),

  created_at           CHAR(24)     NOT NULL,
  updated_at           CHAR(24)     NOT NULL,
  created_by           CHAR(36),
  updated_by           CHAR(36),
  row_version          INTEGER      NOT NULL DEFAULT 1,

  CONSTRAINT ux_journal_entries_number UNIQUE (company_id, entry_number),
  CONSTRAINT ck_journal_entries_status CHECK (status IN ('draft', 'posted', 'reversed')),
  -- A posted entry must record when and by whom. An entry that claims to be posted with no
  -- timestamp is one nobody can place in a sequence of events.
  CONSTRAINT ck_journal_entries_posted CHECK (status <> 'posted' OR posted_at IS NOT NULL)
);

CREATE INDEX ix_journal_entries_period ON journal_entries (fiscal_period_id, status);
CREATE INDEX ix_journal_entries_date   ON journal_entries (company_id, entry_date);
CREATE INDEX ix_journal_entries_source ON journal_entries (source_document_type, source_document_id);

CREATE TABLE journal_lines (
  id                    CHAR(36)    NOT NULL PRIMARY KEY,
  journal_entry_id      CHAR(36)    NOT NULL REFERENCES journal_entries(id),
  line_number           INTEGER     NOT NULL,
  account_id            CHAR(36)    NOT NULL REFERENCES accounts(id),

  -- The functional currency, in minor units, as integers (§7.1). Never a float: the whole
  -- kernel exists so that money is exact, and a ledger is the last place to give that up.
  debit_minor           BIGINT      NOT NULL DEFAULT 0,
  credit_minor          BIGINT      NOT NULL DEFAULT 0,

  -- The TRANSACTION currency (§18.1). The ledger balances in the functional currency; the
  -- original is what an auditor asks about — "you charged them 100 dollars, show me".
  currency_code         CHAR(3)     REFERENCES currencies(code),
  original_amount_minor BIGINT,
  exchange_rate_micro   BIGINT,                        -- rate ×10⁶, integer (§7.3)

  -- Analytical dimensions (§20.2). Present from the first release although nothing populates
  -- most of them yet: adding a dimension to a ledger with three years of history means those
  -- three years cannot be analysed by it, and that is not recoverable.
  branch_id             CHAR(36)    REFERENCES branches(id),
  warehouse_id          CHAR(36)    REFERENCES warehouses(id),
  partner_id            CHAR(36),
  product_id            CHAR(36),
  cost_center_id        CHAR(36),
  project_id            CHAR(36),

  memo                  VARCHAR(400),
  created_at            CHAR(24)    NOT NULL,

  CONSTRAINT ck_journal_lines_signs  CHECK (debit_minor >= 0 AND credit_minor >= 0),
  -- A line is debit XOR credit (§20.2). A line carrying both is not a posting, it is two
  -- postings somebody netted off — and netting destroys the audit trail the ledger exists for.
  CONSTRAINT ck_journal_lines_side   CHECK (NOT (debit_minor > 0 AND credit_minor > 0)),
  -- A line of zero on both sides is noise that would survive into every report.
  CONSTRAINT ck_journal_lines_amount CHECK (debit_minor > 0 OR credit_minor > 0),
  CONSTRAINT ux_journal_lines_number UNIQUE (journal_entry_id, line_number)
);

CREATE INDEX ix_journal_lines_entry   ON journal_lines (journal_entry_id, line_number);
-- The index the trial balance and every account statement read.
CREATE INDEX ix_journal_lines_account ON journal_lines (account_id);
