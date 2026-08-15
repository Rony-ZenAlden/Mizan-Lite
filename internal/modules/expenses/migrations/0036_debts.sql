-- 0036_debts — money in and out with no trade document.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- WHY THESE ARE NOT SALES OR PURCHASE DOCUMENTS
-- ─────────────────────────────────────────────────────────────────────────────
--
-- An owner putting money into the business, a loan from a relative, an advance against wages:
-- money moves, nothing is bought or sold, and no invoice exists.
--
-- Modelling them as sales or purchases would put them in REVENUE and COST reports where they are
-- neither. A business whose "sales" included the owner's own capital would show a month it never
-- had, and the mistake is invisible — the totals all add up, they are just about the wrong thing.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- WHY THEY LIVE IN THE EXPENSES MODULE
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Not because they are expenses — they are not. Because a module is a unit of OWNERSHIP, and a
-- separate `debts` module owning two small tables would earn a migration range, a permission set,
-- a service, a binding façade, and a place in three registration lists, in exchange for keeping
-- apart two things that are already only used together: money out that buys no stock.
--
-- §10 draws module boundaries around what changes together. If debts grow a life of their own —
-- schedules, interest, terms — they earn their own module then, and the tables move with a
-- migration rather than being split now on a guess.

CREATE TABLE debts (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  company_id    CHAR(36)    NOT NULL REFERENCES companies(id),
  branch_id     CHAR(36)    NOT NULL REFERENCES branches(id),

  status        VARCHAR(20) NOT NULL DEFAULT 'draft'
                CHECK (status IN ('draft', 'posted', 'cancelled')),
  document_number VARCHAR(60),

  -- WHICH WAY THE MONEY WENT. Not a sign on the amount: a signed amount makes SUM(amount)
  -- meaningless and every query a minefield, which is the same reasoning that keeps stock
  -- movement quantities positive (§4.1) and payment amounts positive (0024, 0033).
  direction     VARCHAR(10) NOT NULL CHECK (direction IN ('received', 'paid')),

  -- WHAT KIND of obligation, which decides the account the other side lands in.
  --
  -- A closed set of three, deliberately. Expense categories are open-ended and therefore carry
  -- their own account (0034); these are not, so they go through MAPPINGS like everything else —
  -- using 7.1's document-named exception here would be naming accounts in Go with extra steps.
  kind          VARCHAR(20) NOT NULL
                CHECK (kind IN ('loan_payable', 'loan_receivable', 'owner_equity')),

  -- Who the money came from or went to. NULLABLE, because the owner is usually not a partner
  -- record and a loan from a relative rarely is either — but the NAME is always kept.
  partner_id    CHAR(36)    REFERENCES partners(id),
  counterparty_name VARCHAR(200) NOT NULL,

  debt_date     VARCHAR(10) NOT NULL,
  -- When it is expected back. NULL for owner capital, which has no due date and is not a debt in
  -- the legal sense — it shares this table because the money movement is identical.
  due_date      VARCHAR(10),

  -- HOW the money moved, which decides which money account it touched. Part of the posting
  -- action, so a business whose cheques clear elsewhere edits a seed file (§20.3).
  method        VARCHAR(20) NOT NULL
                CHECK (method IN ('cash', 'card', 'bank_transfer', 'cheque')),
  reference     VARCHAR(80),
  description   VARCHAR(1000),

  currency_code CHAR(3)     NOT NULL REFERENCES currencies(code),
  amount_minor  INTEGER     NOT NULL CHECK (amount_minor > 0),

  posted_at     VARCHAR(32),
  posted_by     CHAR(36)    REFERENCES users(id),

  row_version   INTEGER     NOT NULL DEFAULT 1,
  created_at    VARCHAR(32) NOT NULL,
  created_by    CHAR(36)    REFERENCES users(id),
  updated_at    VARCHAR(32) NOT NULL,

  CONSTRAINT ck_debt_draft_unnumbered CHECK (
    status <> 'draft' OR document_number IS NULL
  ),
  CONSTRAINT ck_debt_posted_numbered CHECK (
    status <> 'posted' OR document_number IS NOT NULL
  )
);

CREATE UNIQUE INDEX ux_debts_number
  ON debts (company_id, document_number) WHERE document_number IS NOT NULL;
CREATE INDEX ix_debts_partner ON debts (partner_id, debt_date);
CREATE INDEX ix_debts_kind ON debts (company_id, kind, status);
-- What is outstanding and when. The report somebody opens before promising anybody anything.
CREATE INDEX ix_debts_due ON debts (company_id, due_date) WHERE status = 'posted';
