-- 0014_tax — the tax engine (§19).
--
-- Owned by the tax module. Accounting owns 0010–0013, so tax owns 0014.
--
-- # NO RATE IS SEEDED, ANYWHERE
--
-- §19.1: "no tax rate, threshold, or rule is ever written in Go code. The code contains a
-- resolution algorithm; the rates and rules are data." §C.3 goes further and forbids asserting
-- jurisdictional facts at all — those come from the customer or their accountant.
--
-- So v1 ships the complete engine and NOT ONE RATE. That is not a stub: every table, the
-- resolution order, the arithmetic, and the disabled state are built and tested. What is absent
-- is the data, which was never ours to invent.
--
-- §19.5 designs the empty state as first-class: with `tax.enabled = false` every document
-- resolves to zero tax and no tax posting is made, while the tables and code paths remain —
-- so turning it on later needs NO MIGRATION.

CREATE TABLE tax_jurisdictions (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  company_id   CHAR(36)     NOT NULL REFERENCES companies(id),
  country_code CHAR(2)      NOT NULL,
  -- A state, province, or emirate. NULL means the whole country.
  region_code  VARCHAR(10),
  name         VARCHAR(120) NOT NULL,
  is_active    INTEGER      NOT NULL DEFAULT 1,

  created_at   CHAR(24)     NOT NULL,
  updated_at   CHAR(24)     NOT NULL,
  row_version  INTEGER      NOT NULL DEFAULT 1,

  CONSTRAINT ck_tax_jurisdictions_active CHECK (is_active IN (0, 1))
);

CREATE TABLE taxes (
  id                    CHAR(36)     NOT NULL PRIMARY KEY,
  company_id            CHAR(36)     NOT NULL REFERENCES companies(id),
  jurisdiction_id       CHAR(36)     REFERENCES tax_jurisdictions(id),

  code                  VARCHAR(40)  NOT NULL,
  name                  VARCHAR(120) NOT NULL,
  name_key              VARCHAR(120),

  tax_type              VARCHAR(20)  NOT NULL,   -- vat|sales|excise|withholding
  calculation           VARCHAR(24)  NOT NULL,   -- percentage|fixed_per_unit|fixed_per_document

  -- A compound tax computes on a base that already includes an earlier tax.
  is_compound           INTEGER      NOT NULL DEFAULT 0,
  -- Recoverable input tax is an ASSET (we reclaim it); non-recoverable is a cost.
  is_recoverable        INTEGER      NOT NULL DEFAULT 1,

  -- Where the tax posts (§19.4, §20). NULL falls back to the TAX_PAYABLE / TAX_RECEIVABLE
  -- account mappings — which is the normal case, and why these are nullable rather than
  -- required: a shop with one VAT does not need to name accounts per tax.
  payable_account_id    CHAR(36)     REFERENCES accounts(id),
  receivable_account_id CHAR(36)     REFERENCES accounts(id),

  is_active             INTEGER      NOT NULL DEFAULT 1,

  created_at            CHAR(24)     NOT NULL,
  updated_at            CHAR(24)     NOT NULL,
  row_version           INTEGER      NOT NULL DEFAULT 1,

  CONSTRAINT ux_taxes_code        UNIQUE (company_id, code),
  CONSTRAINT ck_taxes_type        CHECK (tax_type IN ('vat', 'sales', 'excise', 'withholding')),
  CONSTRAINT ck_taxes_calculation CHECK (calculation IN
                                    ('percentage', 'fixed_per_unit', 'fixed_per_document')),
  CONSTRAINT ck_taxes_compound    CHECK (is_compound IN (0, 1)),
  CONSTRAINT ck_taxes_recoverable CHECK (is_recoverable IN (0, 1)),
  CONSTRAINT ck_taxes_active      CHECK (is_active IN (0, 1))
);

-- ─────────────────────────────────────────────────────────────────────────────
-- tax_versions — THE CRITICAL PIECE (§19.2)
-- ─────────────────────────────────────────────────────────────────────────────
--
-- When VAT moves from 15% to 16%, a VERSION is added with an effective date. Historical
-- invoices continue to resolve the old rate, and the change is auditable.
--
-- §19.2 states the alternative outright: "editing a rate in place would silently falsify past
-- documents." That is the exact failure this phase is shaped around — books that balance and
-- are wrong. There is deliberately no UPDATE path for a rate anywhere in this module.

CREATE TABLE tax_versions (
  id             CHAR(36) NOT NULL PRIMARY KEY,
  tax_id         CHAR(36) NOT NULL REFERENCES taxes(id),

  -- The rate as a fraction ×10⁶ (§7.3): 15% is 150000. An integer, never a float — the whole
  -- kernel exists so money arithmetic is exact, and a rate is half of every tax computation.
  rate_micro     BIGINT   NOT NULL,
  -- For fixed_per_unit / fixed_per_document taxes, in minor units.
  fixed_minor    BIGINT   NOT NULL DEFAULT 0,

  effective_from CHAR(10) NOT NULL,          -- YYYY-MM-DD, business date
  -- NULL means "still in force". Set when a later version supersedes this one.
  effective_to   CHAR(10),

  created_at     CHAR(24) NOT NULL,

  CONSTRAINT ck_tax_versions_rate  CHECK (rate_micro >= 0),
  CONSTRAINT ck_tax_versions_fixed CHECK (fixed_minor >= 0),
  CONSTRAINT ck_tax_versions_range CHECK (effective_to IS NULL OR effective_to >= effective_from),
  CONSTRAINT ux_tax_versions       UNIQUE (tax_id, effective_from)
);

CREATE INDEX ix_tax_versions_lookup ON tax_versions (tax_id, effective_from);

-- ─────────────────────────────────────────────────────────────────────────────
-- tax_groups — several taxes applied together (§19.2)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE tax_groups (
  id                 CHAR(36)     NOT NULL PRIMARY KEY,
  company_id         CHAR(36)     NOT NULL REFERENCES companies(id),
  code               VARCHAR(40)  NOT NULL,
  name               VARCHAR(120) NOT NULL,
  name_key           VARCHAR(120),

  -- INCLUSIVE vs EXCLUSIVE is a property of the GROUP, not a global setting: retail markets
  -- usually quote tax-inclusive, B2B quotes exclusive, and a business doing both needs both.
  is_price_inclusive INTEGER      NOT NULL DEFAULT 0,
  is_default         INTEGER      NOT NULL DEFAULT 0,
  is_active          INTEGER      NOT NULL DEFAULT 1,

  created_at         CHAR(24)     NOT NULL,
  updated_at         CHAR(24)     NOT NULL,
  row_version        INTEGER      NOT NULL DEFAULT 1,

  CONSTRAINT ux_tax_groups_code      UNIQUE (company_id, code),
  CONSTRAINT ck_tax_groups_inclusive CHECK (is_price_inclusive IN (0, 1)),
  CONSTRAINT ck_tax_groups_default   CHECK (is_default IN (0, 1)),
  CONSTRAINT ck_tax_groups_active    CHECK (is_active IN (0, 1))
);

CREATE TABLE tax_group_items (
  id                      CHAR(36) NOT NULL PRIMARY KEY,
  tax_group_id            CHAR(36) NOT NULL REFERENCES tax_groups(id),
  tax_id                  CHAR(36) NOT NULL REFERENCES taxes(id),

  -- Order matters when a tax is compound: it computes on a base that already includes the
  -- taxes before it.
  sequence                INTEGER  NOT NULL DEFAULT 1,
  is_compound_on_previous INTEGER  NOT NULL DEFAULT 0,

  created_at              CHAR(24) NOT NULL,

  CONSTRAINT ux_tax_group_items      UNIQUE (tax_group_id, tax_id),
  CONSTRAINT ck_tax_group_items_comp CHECK (is_compound_on_previous IN (0, 1))
);

CREATE INDEX ix_tax_group_items_group ON tax_group_items (tax_group_id, sequence);

-- ─────────────────────────────────────────────────────────────────────────────
-- tax_exemptions — why a customer was not charged (§19.2)
-- ─────────────────────────────────────────────────────────────────────────────
--
-- The evidence columns are the point. "We did not charge this customer tax" is a claim a tax
-- authority will ask about years later, and the answer has to be a certificate number and a
-- validity window rather than somebody's memory.

CREATE TABLE tax_exemptions (
  id                 CHAR(36)     NOT NULL PRIMARY KEY,
  company_id         CHAR(36)     NOT NULL REFERENCES companies(id),

  -- No foreign key: `partners` arrives in Phase 3. The same forward-reference reasoning as
  -- journal_lines.partner_id (0011) — and the same conclusion, that an exemption's evidence
  -- should outlive a partner record anyway.
  partner_id         CHAR(36)     NOT NULL,
  -- NULL exempts the partner from EVERY tax; a value exempts them from one.
  tax_id             CHAR(36)     REFERENCES taxes(id),

  reason_code        VARCHAR(40)  NOT NULL,
  certificate_number VARCHAR(80),
  valid_from         CHAR(10)     NOT NULL,
  valid_to           CHAR(10),
  evidence_ref       VARCHAR(200),

  created_at         CHAR(24)     NOT NULL,
  updated_at         CHAR(24)     NOT NULL,
  row_version        INTEGER      NOT NULL DEFAULT 1,

  CONSTRAINT ck_tax_exemptions_range CHECK (valid_to IS NULL OR valid_to >= valid_from)
);

CREATE INDEX ix_tax_exemptions_partner ON tax_exemptions (company_id, partner_id, valid_from);
