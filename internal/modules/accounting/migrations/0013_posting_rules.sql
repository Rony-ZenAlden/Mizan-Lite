-- 0013_posting_rules — table-driven posting (§20.3).
--
-- Owned by the accounting module. The most important schema in the phase, and the one that
-- decides how much accounting knowledge every LATER module has to carry.
--
-- # The decision this table exists to avoid
--
-- §20.3 states it: "the naive approach hardcodes 'when an invoice is posted, debit AR, credit
-- revenue, credit tax'. That makes the accounting untestable, un-configurable, and
-- country-specific."
--
-- Instead a rule is a ROW. The accounting module subscribes to domain events, finds the rule for
-- the event type, and evaluates it. What that buys, restated because it is the whole argument:
--
--   - Sales code contains ZERO accounting logic. It publishes "an invoice was posted".
--   - Different countries and trades get different books from seed data.
--   - A mis-mapped account is a data fix, not a patch release.
--   - Posting behaviour is unit-testable without a sales module existing at all — which is
--     exactly what this step does, three phases before the first invoice.

CREATE TABLE posting_rules (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  company_id          CHAR(36)     NOT NULL REFERENCES companies(id),

  -- The domain event this rule answers, e.g. 'sales.invoice.posted'. Matched exactly: a
  -- pattern language here would mean a rule whose reach nobody can state precisely, and
  -- "which rule posted this?" is the first question asked when the books look wrong.
  event_type          VARCHAR(80)  NOT NULL,

  -- NULL applies to every trade. A row naming a profile is an override, resolved most-specific
  -- first — the same precedence as settings (0.5) and account mappings (0010).
  business_profile_id CHAR(36)     REFERENCES business_profiles(id),

  -- Order among rules that all match. Several may: a sale posts revenue AND cost of goods, and
  -- expressing that as two rules keeps each one readable.
  sequence            INTEGER      NOT NULL DEFAULT 1,
  code                VARCHAR(60)  NOT NULL,
  description         VARCHAR(400),
  is_active           INTEGER      NOT NULL DEFAULT 1,

  created_at          CHAR(24)     NOT NULL,
  updated_at          CHAR(24)     NOT NULL,
  row_version         INTEGER      NOT NULL DEFAULT 1,

  CONSTRAINT ux_posting_rules_code   UNIQUE (company_id, code),
  CONSTRAINT ck_posting_rules_active CHECK (is_active IN (0, 1))
);

CREATE INDEX ix_posting_rules_event ON posting_rules (company_id, event_type, is_active, sequence);

CREATE TABLE posting_rule_lines (
  id               CHAR(36)     NOT NULL PRIMARY KEY,
  posting_rule_id  CHAR(36)     NOT NULL REFERENCES posting_rules(id),
  line_number      INTEGER      NOT NULL,

  side             VARCHAR(6)   NOT NULL,        -- debit|credit

  -- WHICH account. 'mapping:AR' resolves through account_mappings; 'account:1200' names one
  -- directly. The mapping form is the one to use: it is what lets the same rule serve a shop
  -- whose receivables account is 1200 and one whose accountant numbered it 130.
  account_selector VARCHAR(80)  NOT NULL,

  -- WHICH amount, by name, from the event's own table of amounts: 'document.total',
  -- 'document.tax', 'document.cost'. The publisher decides what it offers; the rule decides
  -- what it uses. Neither knows the other's vocabulary beyond these strings.
  amount_selector  VARCHAR(80)  NOT NULL,

  -- Reserved (§20.3). Deliberately NOT implemented in Step 2.5, and the reason is on the
  -- evaluator: a line whose amount resolves to zero is skipped, which covers "only when there
  -- is tax" without an expression language at all. Anything powerful enough to need a parser
  -- is powerful enough to hide a bug in a customer's books.
  condition_expr   VARCHAR(200),
  -- Reserved for per-line dimension mapping, which arrives with documents that have lines.
  dimension_map    VARCHAR(400),

  memo             VARCHAR(200),
  created_at       CHAR(24)     NOT NULL,

  CONSTRAINT ck_posting_rule_lines_side CHECK (side IN ('debit', 'credit')),
  CONSTRAINT ux_posting_rule_lines      UNIQUE (posting_rule_id, line_number)
);

CREATE INDEX ix_posting_rule_lines_rule ON posting_rule_lines (posting_rule_id, line_number);
