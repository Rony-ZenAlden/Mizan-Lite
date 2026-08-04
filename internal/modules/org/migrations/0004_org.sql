-- 0004_org — the organisational spine: company, branches, warehouses, fiscal calendar.
--
-- Owned by the module (§10.3), embedded by internal/modules/org, merged into the globally
-- ordered set by the runner. Currency owns 0003, so org owns 0004.
--
-- NOTHING IS SEEDED HERE, and nothing is created at boot (Step 1.1, D1). These rows are
-- written by the setup wizard's single transaction (§WIZ.1). A default company seeded at
-- migration time would make the wizard's "no company exists yet" invariant dead on arrival,
-- and would leave a placeholder whose name, country, and currency are all wrong.
--
-- `created_by` / `updated_by` carry NO foreign key (D4). `users` does not exist until 1.2, so
-- an FK here would be a forward reference — but the better reason is that the audit trail
-- snapshots the actor's name precisely so a five-year-old record stays readable (§15.1).
-- Binding these columns to a live row would make identity load-bearing for history.

-- ─────────────────────────────────────────────────────────────────────────────
-- companies — exactly one in v1, schema-ready for more
-- ─────────────────────────────────────────────────────────────────────────────
--
-- A table rather than a bag of settings because multi-company is a plausible future and
-- retrofitting a company_id onto every table later is the §26 argument in miniature. v1
-- creates one and never shows a picker.

CREATE TABLE companies (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  code                VARCHAR(40)  NOT NULL,
  name                VARCHAR(200) NOT NULL,
  legal_name          VARCHAR(200),
  tax_number          VARCHAR(60),
  country_code        CHAR(2)      NOT NULL,          -- selects the country profile (§C.1)

  -- §18.1's three currency roles. The ledger is kept in functional_currency; pricing_currency
  -- NULL means "same as functional", which is the collapse-to-identity path, not a gap.
  functional_currency CHAR(3)      NOT NULL REFERENCES currencies(code),
  pricing_currency    CHAR(3)               REFERENCES currencies(code),

  logo_ref            VARCHAR(255),
  is_active           SMALLINT     NOT NULL DEFAULT 1,
  created_at          CHAR(24)     NOT NULL,
  created_by          CHAR(36),
  updated_at          CHAR(24)     NOT NULL,
  updated_by          CHAR(36),
  row_version         BIGINT       NOT NULL DEFAULT 1,

  CONSTRAINT ux_companies_code    UNIQUE (code),
  CONSTRAINT ck_companies_active  CHECK (is_active IN (0, 1)),
  CONSTRAINT ck_companies_country CHECK (LENGTH(country_code) = 2)
);

-- ─────────────────────────────────────────────────────────────────────────────
-- branches — every transactional table will carry branch_id NOT NULL (§26.1)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE branches (
  id          CHAR(36)     NOT NULL PRIMARY KEY,
  company_id  CHAR(36)     NOT NULL REFERENCES companies(id),
  code        VARCHAR(40)  NOT NULL,
  name        VARCHAR(200) NOT NULL,
  is_default  SMALLINT     NOT NULL DEFAULT 0,
  -- Shaped by the country profile's address_format (§C.1), so adding a country does not mean
  -- adding columns. Validated in Go; never queried by field (§8.2 forbids JSON SQL).
  address_json TEXT,
  is_active   SMALLINT     NOT NULL DEFAULT 1,
  created_at  CHAR(24)     NOT NULL,
  created_by  CHAR(36),
  updated_at  CHAR(24)     NOT NULL,
  updated_by  CHAR(36),
  row_version BIGINT       NOT NULL DEFAULT 1,

  CONSTRAINT ux_branches_code    UNIQUE (company_id, code),
  CONSTRAINT ck_branches_default CHECK (is_default IN (0, 1)),
  CONSTRAINT ck_branches_active  CHECK (is_active IN (0, 1))
);

CREATE INDEX ix_branches_company ON branches (company_id, is_active);

-- ─────────────────────────────────────────────────────────────────────────────
-- warehouses — stock is always tracked per warehouse (§26.3)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE warehouses (
  id          CHAR(36)     NOT NULL PRIMARY KEY,
  branch_id   CHAR(36)     NOT NULL REFERENCES branches(id),
  code        VARCHAR(40)  NOT NULL,
  name        VARCHAR(200) NOT NULL,
  is_default  SMALLINT     NOT NULL DEFAULT 0,
  -- §21.2: some businesses genuinely need negative stock, most should not. Read from Phase 4.
  allows_negative_stock SMALLINT NOT NULL DEFAULT 0,
  is_active   SMALLINT     NOT NULL DEFAULT 1,
  created_at  CHAR(24)     NOT NULL,
  created_by  CHAR(36),
  updated_at  CHAR(24)     NOT NULL,
  updated_by  CHAR(36),
  row_version BIGINT       NOT NULL DEFAULT 1,

  CONSTRAINT ux_warehouses_code     UNIQUE (branch_id, code),
  CONSTRAINT ck_warehouses_default  CHECK (is_default IN (0, 1)),
  CONSTRAINT ck_warehouses_negative CHECK (allows_negative_stock IN (0, 1)),
  CONSTRAINT ck_warehouses_active   CHECK (is_active IN (0, 1))
);

CREATE INDEX ix_warehouses_branch ON warehouses (branch_id, is_active);

-- ─────────────────────────────────────────────────────────────────────────────
-- fiscal_years / fiscal_periods — created now, first read in Phase 2 (§20.4)
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Created here rather than with accounting (D6, following 0.4's D4): their shape is fixed by
-- approved design, the wizard already asks for the fiscal year start (§C.2 step 6), and
-- generating twelve periods from a start month is arithmetic, not a design question.
--
-- Periods are GENERATED and never hand-entered. A boundary a day out means a Phase 2 posting
-- that belongs to no period or to two, and generation makes that unrepresentable rather than
-- merely validated.

CREATE TABLE fiscal_years (
  id          CHAR(36)    NOT NULL PRIMARY KEY,
  company_id  CHAR(36)    NOT NULL REFERENCES companies(id),
  code        VARCHAR(20) NOT NULL,             -- 'FY2026'
  start_date  CHAR(10)    NOT NULL,             -- business dates, not instants (§7.5)
  end_date    CHAR(10)    NOT NULL,
  status      VARCHAR(10) NOT NULL DEFAULT 'open',
  created_at  CHAR(24)    NOT NULL,
  created_by  CHAR(36),
  updated_at  CHAR(24)    NOT NULL,
  updated_by  CHAR(36),
  row_version BIGINT      NOT NULL DEFAULT 1,

  CONSTRAINT ux_fiscal_years        UNIQUE (company_id, code),
  CONSTRAINT ck_fiscal_years_status CHECK (status IN ('open', 'closed', 'locked')),
  CONSTRAINT ck_fiscal_years_range  CHECK (end_date > start_date)
);

CREATE TABLE fiscal_periods (
  id             CHAR(36)    NOT NULL PRIMARY KEY,
  fiscal_year_id CHAR(36)    NOT NULL REFERENCES fiscal_years(id),
  sequence       INTEGER     NOT NULL,          -- 1..12
  start_date     CHAR(10)    NOT NULL,
  end_date       CHAR(10)    NOT NULL,
  status         VARCHAR(10) NOT NULL DEFAULT 'open',
  created_at     CHAR(24)    NOT NULL,
  created_by     CHAR(36),
  updated_at     CHAR(24)    NOT NULL,
  updated_by     CHAR(36),
  row_version    BIGINT      NOT NULL DEFAULT 1,

  CONSTRAINT ux_fiscal_periods        UNIQUE (fiscal_year_id, sequence),
  CONSTRAINT ck_fiscal_periods_status CHECK (status IN ('open', 'closed', 'locked')),
  CONSTRAINT ck_fiscal_periods_range  CHECK (end_date >= start_date),
  CONSTRAINT ck_fiscal_periods_seq    CHECK (sequence >= 1)
);

CREATE INDEX ix_fiscal_periods_year ON fiscal_periods (fiscal_year_id, sequence);
