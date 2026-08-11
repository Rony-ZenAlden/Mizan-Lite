-- 0018_partners — customers and suppliers, in ONE table (decision 9).
--
-- Owned by the partner module (§10.3). Catalog owns 0015–0017, so partner owns 0018.
--
-- # Why one table
--
-- A business that both buys from and sells to the same company is ordinary — a workshop that
-- buys steel from a merchant and sells them finished brackets — and two tables make that either
-- a duplicate row nobody keeps in step or a join nobody remembers to write. One table
-- discriminated by role flags means the statement of account for such a company is one query,
-- and the tax number is recorded once.
--
-- The cost is a `partners` row carrying columns only one role uses: a credit limit means nothing
-- for a supplier, and a supplier lead time means nothing for a customer. That is a smaller price
-- than a duplicate identity, and NULL says "not applicable" perfectly well.

CREATE TABLE partners (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  company_id   CHAR(36)     NOT NULL REFERENCES companies(id),

  -- The stable key: imports match on it, statements are headed with it, an operator recognises
  -- it. Never the id.
  code         VARCHAR(40)  NOT NULL,
  name         VARCHAR(200) NOT NULL,
  -- The name as it must appear on a legal document, when that differs from the name everyone
  -- uses. "Ahmad's" on the screen, "Ahmad Trading Company LLC" on the invoice.
  legal_name   VARCHAR(200),

  -- THE ROLE FLAGS. At least one must be set — enforced by the CHECK below and by the domain,
  -- because a partner who is neither is a row nothing can ever reference.
  is_customer  INTEGER      NOT NULL DEFAULT 0 CHECK (is_customer IN (0, 1)),
  is_supplier  INTEGER      NOT NULL DEFAULT 0 CHECK (is_supplier IN (0, 1)),

  -- individual|company. Drives which tax rules apply in several countries and what a document
  -- must print, so it is a column rather than something inferred from whether a tax number is
  -- present.
  partner_type VARCHAR(20)  NOT NULL DEFAULT 'company'
               CHECK (partner_type IN ('individual', 'company')),

  -- ── tax ──────────────────────────────────────────────────────────────────
  --
  -- The tax NUMBER is free text: formats differ by country, they change, and a validation rule
  -- welded into the schema becomes a reason a customer cannot be entered at all. Format checking
  -- belongs in the country profile as a warning.
  tax_number   VARCHAR(60),
  -- Whichever group answers when resolution reaches the partner level (§19.3, level 4). NULL
  -- means "the company default decides".
  tax_group_id CHAR(36)     REFERENCES tax_groups(id),
  -- An exemption is a fact about the partner, and it OUTRANKS the group: a diplomatic mission
  -- pays no VAT whatever the product's group says. Phase 2's resolver already reads this.
  is_tax_exempt INTEGER     NOT NULL DEFAULT 0 CHECK (is_tax_exempt IN (0, 1)),
  tax_exempt_reason VARCHAR(200),

  -- ── money ────────────────────────────────────────────────────────────────
  --
  -- NULL means the company's functional currency. Set for a partner invoiced in something else
  -- — §18 already owns the rate mechanics; this only records the choice.
  currency_code CHAR(3)     REFERENCES currencies(code),

  -- Days from the invoice date. 0 is cash. A term is a number rather than a reference to a
  -- terms table because "30 days" is the whole of what v1 needs, and a table of one column with
  -- a foreign key is a join bought for nothing.
  payment_terms_days INTEGER NOT NULL DEFAULT 0 CHECK (payment_terms_days >= 0),

  -- Minor units, as everywhere. 0 means no limit rather than "no credit": a limit of zero would
  -- refuse every sale, which is never what leaving the field alone was meant to say.
  credit_limit_minor INTEGER NOT NULL DEFAULT 0 CHECK (credit_limit_minor >= 0),

  -- Where this partner's balance sits. NULL means the company's mapped receivable or payable
  -- account (Phase 2's account_mappings). Set for a business that keeps certain partners on
  -- their own control account.
  receivable_account_id CHAR(36) REFERENCES accounts(id),
  payable_account_id    CHAR(36) REFERENCES accounts(id),

  -- The price list this partner buys at. The first level of §2.6's resolution order, and the
  -- reason it is here rather than in a separate assignment table: one partner has one list, and
  -- a join table for a one-to-one is a query nobody thanks you for. The COLUMN is added in
  -- 3.5, when price_lists exists.

  -- ── contact ──────────────────────────────────────────────────────────────
  phone        VARCHAR(60),
  email        VARCHAR(200),
  website      VARCHAR(200),
  notes        TEXT,

  is_active    INTEGER      NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
  -- Set when the partner first appears on a document. Read by the domain to refuse deletion,
  -- for the same reason a variant with history is refused: a supplier deleted after a two-year
  -- old bill breaks that document and every report that groups by them.
  has_history  INTEGER      NOT NULL DEFAULT 0 CHECK (has_history IN (0, 1)),

  row_version  INTEGER      NOT NULL DEFAULT 1,
  created_at   VARCHAR(32)  NOT NULL,
  updated_at   VARCHAR(32)  NOT NULL,

  -- A partner who is neither a customer nor a supplier is a row nothing can ever reference.
  CONSTRAINT ck_partner_has_a_role CHECK (is_customer = 1 OR is_supplier = 1)
);

CREATE UNIQUE INDEX ux_partners_code ON partners (company_id, code);
CREATE INDEX ix_partners_customers ON partners (company_id, is_customer, is_active);
CREATE INDEX ix_partners_suppliers ON partners (company_id, is_supplier, is_active);
CREATE INDEX ix_partners_name ON partners (company_id, name);

-- ─────────────────────────────────────────────────────────────────────────────
-- partner_addresses
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Separate rather than columns on `partners`, because a partner genuinely has several: bill to
-- head office, deliver to three branches. Flattening them onto the partner would mean a fourth
-- delivery address needs a migration.

CREATE TABLE partner_addresses (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  partner_id   CHAR(36)     NOT NULL REFERENCES partners(id) ON DELETE CASCADE,

  label        VARCHAR(100) NOT NULL,
  address_type VARCHAR(20)  NOT NULL DEFAULT 'both'
               CHECK (address_type IN ('billing', 'shipping', 'both')),

  -- Free-form lines rather than street/number/district columns. Addresses differ enormously
  -- between the countries §1 says this must serve, and a schema that assumes one country's shape
  -- forces every other into the wrong boxes.
  line1        VARCHAR(200) NOT NULL,
  line2        VARCHAR(200),
  city         VARCHAR(100),
  region       VARCHAR(100),
  postal_code  VARCHAR(20),
  country_code CHAR(2),

  contact_name  VARCHAR(200),
  contact_phone VARCHAR(60),

  is_default   INTEGER      NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
  is_active    INTEGER      NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),

  row_version  INTEGER      NOT NULL DEFAULT 1,
  created_at   VARCHAR(32)  NOT NULL,
  updated_at   VARCHAR(32)  NOT NULL
);

CREATE INDEX ix_partner_addresses_partner ON partner_addresses (partner_id, is_active);

-- At most one default per partner per purpose, the same partial-index shape 0015's reference
-- unit and 0016's default variant use.
CREATE UNIQUE INDEX ux_partner_addresses_one_default
  ON partner_addresses (partner_id, address_type) WHERE is_default = 1;

-- ─────────────────────────────────────────────────────────────────────────────
-- partner_contacts — the people
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE partner_contacts (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  partner_id   CHAR(36)     NOT NULL REFERENCES partners(id) ON DELETE CASCADE,

  name         VARCHAR(200) NOT NULL,
  role         VARCHAR(100),
  phone        VARCHAR(60),
  email        VARCHAR(200),

  is_primary   INTEGER      NOT NULL DEFAULT 0 CHECK (is_primary IN (0, 1)),
  is_active    INTEGER      NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),

  row_version  INTEGER      NOT NULL DEFAULT 1,
  created_at   VARCHAR(32)  NOT NULL,
  updated_at   VARCHAR(32)  NOT NULL
);

CREATE INDEX ix_partner_contacts_partner ON partner_contacts (partner_id, is_active);
CREATE UNIQUE INDEX ux_partner_contacts_one_primary
  ON partner_contacts (partner_id) WHERE is_primary = 1;

-- ─────────────────────────────────────────────────────────────────────────────
-- partner_role_usage — which roles have actually been used
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Separate from `partners.has_history`, because the two facts drive different rules and
-- collapsing them would make one of the rules wrong.
--
-- `has_history` refuses DELETION: the partner appears on some document, so removing them would
-- break it. Per-role usage refuses removing THAT ONE ROLE: a supplier who has been billed cannot
-- stop being a supplier, because their payable sits in the accounts and clearing the flag would
-- hide them from the supplier list while the balance remains. But that same partner may
-- perfectly well stop being a customer, if nothing was ever sold to them.
--
-- One flag could not express that, and a partner who bought once and supplies constantly is not
-- a rare shape.

CREATE TABLE partner_role_usage (
  partner_id     CHAR(36)    NOT NULL PRIMARY KEY REFERENCES partners(id) ON DELETE CASCADE,
  sold_to        INTEGER     NOT NULL DEFAULT 0 CHECK (sold_to IN (0, 1)),
  purchased_from INTEGER     NOT NULL DEFAULT 0 CHECK (purchased_from IN (0, 1)),
  updated_at     VARCHAR(32) NOT NULL
);
