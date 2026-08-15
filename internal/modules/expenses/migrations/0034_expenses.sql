-- 0034_expenses — money out that buys no stock.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- WHY THIS IS NOT A PURCHASE BILL
-- ─────────────────────────────────────────────────────────────────────────────
--
-- The instinct is to reuse it: a bill has a supplier, a date, a total, and it posts to payables.
--
-- It fails on the LINE. A purchase bill line MUST name a goods receipt line (0029, NOT NULL),
-- because that is what makes the three-way match structural rather than validated — "invoiced for
-- goods that never arrived" is not a state the schema can hold, rather than one it checks for.
--
-- An electricity bill has no delivery, no stock movement, and nothing to match against. Reusing
-- the table would mean making that column nullable, and the moment it is nullable the match
-- becomes a rule somebody remembered to write. Phase 6's strongest guarantee would be traded for
-- one table's reuse.
--
-- The structural difference is one column: a purchase line points at a VARIANT, an expense line
-- points at a CATEGORY. Everything else follows from that.

-- ─────────────────────────────────────────────────────────────────────────────
-- expense_categories — what kind of spending, and where it lands
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Reference data, seeded per country profile. What a business must report separately is a TAX
-- question that differs by jurisdiction, and adding a category must not need a release.

CREATE TABLE expense_categories (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  company_id    CHAR(36)    NOT NULL REFERENCES companies(id),

  code          VARCHAR(40) NOT NULL,
  name          VARCHAR(200) NOT NULL,
  -- The translation key for a SHIPPED category. NULL for one a customer added, whose name is
  -- their own words and belongs in the `translations` table if they want it translated (§9.5).
  name_key      VARCHAR(120),

  -- Where this kind of spending posts.
  --
  -- A direct account reference rather than a mapping, and that is deliberate: mappings name
  -- concepts the POSTING RULES know about, and there are only ever a handful of those. Expense
  -- categories are open-ended — a business will have forty — so each carries its own account and
  -- the rule reads it from the document.
  account_id    CHAR(36)    NOT NULL REFERENCES accounts(id),

  -- Whether spending here is recoverable for tax. Differs by category in most jurisdictions:
  -- entertainment usually is not, fuel usually is, and getting it wrong is a claim a revenue
  -- authority disallows.
  tax_recoverable INTEGER   NOT NULL DEFAULT 1 CHECK (tax_recoverable IN (0, 1)),

  parent_id     CHAR(36)    REFERENCES expense_categories(id),
  sort_order    INTEGER     NOT NULL DEFAULT 0,
  is_active     INTEGER     NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),

  row_version   INTEGER     NOT NULL DEFAULT 1,
  created_at    VARCHAR(32) NOT NULL,
  updated_at    VARCHAR(32) NOT NULL
);

CREATE UNIQUE INDEX ux_expense_categories_code ON expense_categories (company_id, code);
CREATE INDEX ix_expense_categories_parent ON expense_categories (parent_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- expenses — the document
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE expenses (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  company_id    CHAR(36)    NOT NULL REFERENCES companies(id),
  branch_id     CHAR(36)    NOT NULL REFERENCES branches(id),

  status        VARCHAR(20) NOT NULL DEFAULT 'draft'
                CHECK (status IN ('draft', 'posted', 'cancelled')),
  document_number VARCHAR(60),

  -- NULLABLE, and usually null. The electricity company is a partner if you want a statement
  -- from them and a name on a report if you do not — forcing a partner record for every taxi
  -- fare is how a partner list becomes unusable.
  partner_id    CHAR(36)    REFERENCES partners(id),
  -- The payee as WRITTEN, whether or not there is a partner behind it (§9.3).
  payee_name    VARCHAR(200) NOT NULL,

  expense_date  VARCHAR(10) NOT NULL,
  -- Their reference: the invoice number on the utility bill, the receipt number from the garage.
  reference     VARCHAR(80),
  description   VARCHAR(1000),

  -- WHETHER IT IS ALREADY PAID.
  --
  -- The one field that gives two documents from one table, and the distinction is a real business
  -- one: fuel bought with cash is spent and gone; rent invoiced monthly is owed until paid. The
  -- same fact at different moments.
  --
  -- 'immediate' names the payment method, so the posting action carries it and the seeded rules
  -- decide which account the money left (§20.3, the shape 6.7 proved twice).
  settlement    VARCHAR(20) NOT NULL DEFAULT 'immediate'
                CHECK (settlement IN ('immediate', 'on_account')),
  paid_method   VARCHAR(20)
                CHECK (paid_method IS NULL
                       OR paid_method IN ('cash', 'card', 'bank_transfer', 'cheque')),
  -- When it falls due, for one on account.
  due_date      VARCHAR(10),

  currency_code CHAR(3)     NOT NULL REFERENCES currencies(code),
  exchange_rate_micro INTEGER NOT NULL DEFAULT 1000000 CHECK (exchange_rate_micro > 0),

  net_minor     INTEGER     NOT NULL DEFAULT 0,
  tax_minor     INTEGER     NOT NULL DEFAULT 0,
  total_minor   INTEGER     NOT NULL DEFAULT 0,

  -- A recurring expense's template. Pre-fills a form somebody CONFIRMS rather than posting by
  -- itself: an expense that appears in the books without a person deciding it did is an expense
  -- nobody checked, and a standing order posted after the lease ended is silent and compounding.
  is_template   INTEGER     NOT NULL DEFAULT 0 CHECK (is_template IN (0, 1)),
  recurs_every_months INTEGER,

  posted_at     VARCHAR(32),
  posted_by     CHAR(36)    REFERENCES users(id),

  row_version   INTEGER     NOT NULL DEFAULT 1,
  created_at    VARCHAR(32) NOT NULL,
  created_by    CHAR(36)    REFERENCES users(id),
  updated_at    VARCHAR(32) NOT NULL,

  CONSTRAINT ck_exp_draft_unnumbered CHECK (
    status <> 'draft' OR document_number IS NULL
  ),
  CONSTRAINT ck_exp_posted_numbered CHECK (
    status <> 'posted' OR document_number IS NOT NULL
  ),
  -- An expense paid immediately must say HOW; one on account must not, because it has not been
  -- paid yet and naming a method would assert something that has not happened.
  CONSTRAINT ck_exp_method CHECK (
    (settlement = 'immediate' AND paid_method IS NOT NULL)
    OR (settlement = 'on_account' AND paid_method IS NULL)
  ),
  -- A template is never posted: it is a form to fill in, not a document.
  CONSTRAINT ck_exp_template_unposted CHECK (
    is_template = 0 OR status = 'draft'
  )
);

CREATE UNIQUE INDEX ux_expenses_number
  ON expenses (company_id, document_number) WHERE document_number IS NOT NULL;
CREATE INDEX ix_expenses_date ON expenses (company_id, expense_date);
CREATE INDEX ix_expenses_partner ON expenses (partner_id, expense_date);
-- What is owed and when. The report somebody opens to decide what to pay this week.
CREATE INDEX ix_expenses_due ON expenses (company_id, due_date)
  WHERE status = 'posted' AND settlement = 'on_account';
CREATE INDEX ix_expenses_templates ON expenses (company_id) WHERE is_template = 1;

CREATE TABLE expense_lines (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  expense_id    CHAR(36)    NOT NULL REFERENCES expenses(id) ON DELETE CASCADE,
  line_number   INTEGER     NOT NULL,

  -- WHAT this line points at, and the whole difference from a purchase line.
  category_id   CHAR(36)    NOT NULL REFERENCES expense_categories(id),
  -- The account, SNAPSHOTTED (§9.3). A category re-pointed at a different account next year must
  -- not move where a posted expense landed — the journal entry says one thing and the document
  -- would say another.
  account_id    CHAR(36)    NOT NULL REFERENCES accounts(id),
  category_name VARCHAR(200) NOT NULL,

  description   VARCHAR(400),

  net_minor     INTEGER     NOT NULL CHECK (net_minor > 0),
  tax_rate_micro INTEGER    NOT NULL DEFAULT 0,
  tax_code      VARCHAR(40),
  tax_amount_minor INTEGER  NOT NULL DEFAULT 0,
  -- Whether the tax on THIS line is recoverable, snapshotted from the category for the same
  -- reason the account is.
  tax_recoverable INTEGER   NOT NULL DEFAULT 1 CHECK (tax_recoverable IN (0, 1)),
  total_minor   INTEGER     NOT NULL DEFAULT 0,

  row_version   INTEGER     NOT NULL DEFAULT 1,
  created_at    VARCHAR(32) NOT NULL,
  updated_at    VARCHAR(32) NOT NULL
);

CREATE UNIQUE INDEX ux_expense_lines_number ON expense_lines (expense_id, line_number);
CREATE INDEX ix_expense_lines_expense ON expense_lines (expense_id);
CREATE INDEX ix_expense_lines_category ON expense_lines (category_id);
