-- 0010_accounting — the chart of accounts (§20.1).
--
-- Owned by the accounting module (§10.3). Profile owns 0009, so accounting owns 0010.
--
-- This is the first table of the general ledger, and §20 opens by saying accounting is "built
-- completely from day one, exposed progressively in the UI". So the schema here is the FINAL
-- one, not a v1 subset: the five UI tiers of §20.6 must arrive with no schema change, which
-- only works if the columns they need already exist.
--
-- NOTHING IS SEEDED HERE. A chart of accounts is country- and trade-specific, so it arrives
-- from `seeds/chart_of_accounts/*.json` through the layered loader Step 1.8 built — the same
-- shipped-plus-data-directory mechanism country profiles use. A default chart written into a
-- migration would be an accounting opinion welded into the schema.

-- ─────────────────────────────────────────────────────────────────────────────
-- accounts — hierarchical, unlimited depth
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE accounts (
  id               CHAR(36)     NOT NULL PRIMARY KEY,
  company_id       CHAR(36)     NOT NULL REFERENCES companies(id),

  -- `code` is the account number an accountant actually uses ("1100"), and it is the stable
  -- key: seeds match on it, posting rules name it, and imported trial balances arrive keyed
  -- by it. Never the id.
  code             VARCHAR(40)  NOT NULL,
  name             VARCHAR(200) NOT NULL,
  -- The translation key, so an Arabic chart shows Arabic names (§22.4). The `name` above is
  -- the fallback for a locale with no translation, never the source of truth.
  name_key         VARCHAR(120),

  parent_id        CHAR(36)     REFERENCES accounts(id),
  -- Materialised path of ancestor codes, root-first, slash-separated: '/1000/1100/'.
  --
  -- Denormalised on purpose (§20.1). Subtree aggregation — "every asset account" — is a
  -- LIKE '/1000/%' scan instead of a recursive CTE, and recursive CTEs are exactly the kind
  -- of thing that behaves differently on the engines §8 says this schema must survive moving
  -- to. Maintained by the domain on insert and on re-parent.
  path             VARCHAR(400) NOT NULL,
  depth            INTEGER      NOT NULL DEFAULT 0,

  account_type     VARCHAR(20)  NOT NULL,   -- asset|liability|equity|revenue|expense
  account_subtype  VARCHAR(40),             -- current_asset|fixed_asset|cogs|…

  -- Derived from account_type and stored anyway (§20.1).
  --
  -- Storing a derivable value is normally a smell. Here it is the point: `normal_balance` is
  -- what a report reads to decide whether a positive balance is a debit or a credit, and
  -- re-deriving it in five places — SQL, Go, a report, a chart, an export — is five chances
  -- to disagree about whether an expense is a debit. One column, one answer.
  normal_balance   VARCHAR(6)   NOT NULL,   -- debit|credit

  -- NULL means the company's functional currency. Set only for accounts denominated in a
  -- foreign currency (§18.1) — a USD bank account in a company whose books are in SYP.
  currency_code    CHAR(3)      REFERENCES currencies(code),

  -- ONLY LEAF ACCOUNTS ACCEPT POSTINGS (§20.1).
  --
  -- Enforced when a journal line is written, not merely rendered greyed-out: a posting to a
  -- roll-up account makes every ancestor's total count it twice, and a trial balance that
  -- still balances is the worst possible symptom.
  is_postable      INTEGER      NOT NULL DEFAULT 1,

  -- A system account is one the posting layer references by mapping key — AR, AP, inventory,
  -- COGS, tax payable, FX gain/loss, rounding difference, retained earnings, opening balance.
  -- Deletable would mean a future document cannot post; renameable is fine and expected.
  is_system        INTEGER      NOT NULL DEFAULT 0,
  is_active        INTEGER      NOT NULL DEFAULT 1,

  -- NULL = shared across branches (§20.1). A branch-specific account is the exception.
  branch_id        CHAR(36)     REFERENCES branches(id),

  description      VARCHAR(400),

  created_at       CHAR(24)     NOT NULL,
  updated_at       CHAR(24)     NOT NULL,
  created_by       CHAR(36),
  updated_by       CHAR(36),
  row_version      INTEGER      NOT NULL DEFAULT 1,

  CONSTRAINT ux_accounts_code       UNIQUE (company_id, code),
  CONSTRAINT ck_accounts_type       CHECK (account_type IN
                                      ('asset', 'liability', 'equity', 'revenue', 'expense')),
  CONSTRAINT ck_accounts_normal     CHECK (normal_balance IN ('debit', 'credit')),
  CONSTRAINT ck_accounts_postable   CHECK (is_postable IN (0, 1)),
  CONSTRAINT ck_accounts_system     CHECK (is_system IN (0, 1)),
  CONSTRAINT ck_accounts_active     CHECK (is_active IN (0, 1)),
  CONSTRAINT ck_accounts_depth      CHECK (depth >= 0),
  -- An account cannot be its own parent. Deeper cycles are the domain's job — SQL cannot see
  -- them — but the one-step case is free to catch here and is the one a typo produces.
  CONSTRAINT ck_accounts_not_self   CHECK (parent_id IS NULL OR parent_id <> id)
);

CREATE INDEX ix_accounts_parent ON accounts (parent_id);
CREATE INDEX ix_accounts_path   ON accounts (company_id, path);
CREATE INDEX ix_accounts_type   ON accounts (company_id, account_type, is_active);

-- ─────────────────────────────────────────────────────────────────────────────
-- account_mappings — the indirection posting rules resolve through (§20.3)
-- ─────────────────────────────────────────────────────────────────────────────
--
-- A posting rule says `mapping:AR`, never `account:1200`. That one layer is what lets the same
-- rule set serve a shop whose receivables account is 1200 and one whose accountant numbered it
-- 130 — and what makes a mis-mapped account a data fix rather than a patch release.
--
-- Created here, with the accounts it points at, rather than with the rules that read it in
-- Step 2.5: the mapping is a property of the chart, and the seed that creates the chart is
-- what knows which account is receivables.

CREATE TABLE account_mappings (
  id                  CHAR(36)    NOT NULL PRIMARY KEY,
  company_id          CHAR(36)    NOT NULL REFERENCES companies(id),

  -- 'AR' | 'AP' | 'INVENTORY' | 'COGS' | 'TAX_PAYABLE' | 'FX_GAIN_LOSS' | …
  mapping_key         VARCHAR(60) NOT NULL,
  account_id          CHAR(36)    NOT NULL REFERENCES accounts(id),

  -- Both NULL is the general case. A row with a branch or a business profile is an override,
  -- and resolution takes the most specific match — the same shape as the settings scope chain
  -- (0.5), deliberately, because a second precedence model would be a second thing to learn.
  branch_id           CHAR(36)    REFERENCES branches(id),
  business_profile_id CHAR(36)    REFERENCES business_profiles(id),

  created_at          CHAR(24)    NOT NULL,
  updated_at          CHAR(24)    NOT NULL,
  row_version         INTEGER     NOT NULL DEFAULT 1
);

-- One mapping per key per scope. The COALESCE gives NULL a value so SQLite treats two
-- unscoped rows for the same key as the duplicate they are — NULLs are distinct in a unique
-- index, which would otherwise let 'AR' be mapped twice with no complaint.
CREATE UNIQUE INDEX ux_account_mappings ON account_mappings (
  company_id, mapping_key,
  COALESCE(branch_id, ''), COALESCE(business_profile_id, '')
);
