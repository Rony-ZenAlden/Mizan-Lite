-- 0003_currency — the currency module's schema.
--
-- Owned by the module, not the platform (ARCHITECTURE_v1 §10.3): this file lives inside
-- internal/modules/currency and is embedded by that package, which is what makes
-- Module.Migrations() a real statement rather than a formality. Version numbers stay
-- GLOBALLY ordered, and the loader rejects a duplicate across any two sources.
--
-- Three tables, not the five listed in PHASE_0_FOUNDATION §CUR.1 (Step 0.9, D1):
-- exchange_rate_batches and rate_provider_config are deferred to the phase that builds the
-- preview workflow, because §G.3's requirements (bulk actions, category filters, threshold
-- rules, psychological rounding) will shape them. exchange_rates.batch_id IS included, since
-- back-filling a nullable column onto historical rate rows later is the expensive half.

-- ─────────────────────────────────────────────────────────────────────────────
-- currencies — reference data, following the §CFG.3 metadata contract
-- ─────────────────────────────────────────────────────────────────────────────
--
-- `code` is the business key that documents and foreign keys reference; `id` exists so the
-- metadata seeder from Step 0.5 and the universal-column convention apply unchanged (D2).
--
-- CHAR(3) is a WIDTH, not a claim of ISO-4217 membership. A redenominated or
-- customer-defined currency is a row like any other — that assumption is what makes
-- redenomination (§G.2) a data operation instead of a schema migration under time pressure.

CREATE TABLE currencies (
  id                    CHAR(36)    NOT NULL PRIMARY KEY,
  code                  CHAR(3)     NOT NULL,
  name                  VARCHAR(80) NOT NULL,          -- base label; translated via `translations`
  symbol                VARCHAR(12) NOT NULL,
  decimal_places        SMALLINT    NOT NULL DEFAULT 2, -- minor-unit scale (SYP 0, USD 2, …)
  symbol_position       VARCHAR(8)  NOT NULL DEFAULT 'before',
  rounding_mode         VARCHAR(24) NOT NULL DEFAULT 'half_away_from_zero',

  -- Redenomination (§G.2). Fixed forever, never expires: an old-unit invoice must reprint
  -- with its original amount, while reports spanning the changeover convert through this.
  succeeded_by_code     CHAR(3),
  redenomination_factor BIGINT,                        -- ×10⁹

  is_historical         SMALLINT    NOT NULL DEFAULT 0,
  is_system             SMALLINT    NOT NULL DEFAULT 0,
  is_active             SMALLINT    NOT NULL DEFAULT 1,
  created_at            CHAR(24)    NOT NULL,
  updated_at            CHAR(24)    NOT NULL,
  row_version           BIGINT      NOT NULL DEFAULT 1,

  CONSTRAINT ux_currencies_code UNIQUE (code),
  CONSTRAINT ck_currencies_decimals   CHECK (decimal_places >= 0 AND decimal_places <= 9),
  CONSTRAINT ck_currencies_position   CHECK (symbol_position IN ('before', 'after')),
  CONSTRAINT ck_currencies_historical CHECK (is_historical IN (0, 1)),
  CONSTRAINT ck_currencies_system     CHECK (is_system IN (0, 1)),
  CONSTRAINT ck_currencies_active     CHECK (is_active IN (0, 1)),
  -- A successor without a factor (or the reverse) is a half-configured redenomination, which
  -- would silently produce wrong report totals.
  CONSTRAINT ck_currencies_redenom    CHECK (
    (succeeded_by_code IS NULL AND redenomination_factor IS NULL) OR
    (succeeded_by_code IS NOT NULL AND redenomination_factor IS NOT NULL
       AND redenomination_factor > 0)
  )
);

-- ─────────────────────────────────────────────────────────────────────────────
-- rate_types — DATA, so adding one needs no migration (§G.1)
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Businesses under currency controls track an official and a market rate simultaneously and
-- legitimately use different ones for different purposes: official for tax and statutory
-- accounts, market for pricing and purchasing.

CREATE TABLE rate_types (
  id          CHAR(36)    NOT NULL PRIMARY KEY,
  code        VARCHAR(20) NOT NULL,
  name        VARCHAR(80) NOT NULL,
  is_system   SMALLINT    NOT NULL DEFAULT 0,
  is_active   SMALLINT    NOT NULL DEFAULT 1,
  created_at  CHAR(24)    NOT NULL,
  updated_at  CHAR(24)    NOT NULL,
  row_version BIGINT      NOT NULL DEFAULT 1,

  CONSTRAINT ux_rate_types_code   UNIQUE (code),
  CONSTRAINT ck_rate_types_system CHECK (is_system IN (0, 1)),
  CONSTRAINT ck_rate_types_active CHECK (is_active IN (0, 1))
);

-- ─────────────────────────────────────────────────────────────────────────────
-- exchange_rates — temporal and append-only
-- ─────────────────────────────────────────────────────────────────────────────
--
-- A correction is a NEW ROW, never an update. That is what gives free rate history and makes
-- historical documents reproducible.
--
-- The uniqueness includes created_at (Step 0.9, D3). PHASE_0_FOUNDATION §CUR.1 specified
-- UNIQUE (from, to, rate_type, valid_from) while also requiring append-only corrections —
-- and those two rules contradict each other: correcting today's rate an hour after entering
-- it produces a second row with the same valid_from, which that constraint rejects. The only
-- alternatives were updating in place (destroying the history) or back-dating the correction
-- (falsifying when it was known). With created_at in the key, an exact double-insert is still
-- blocked, and "what rate did we believe for date X, as at time T" stays answerable.

CREATE TABLE exchange_rates (
  id            CHAR(36)    NOT NULL PRIMARY KEY,
  from_currency CHAR(3)     NOT NULL REFERENCES currencies(code),
  to_currency   CHAR(3)     NOT NULL REFERENCES currencies(code),
  rate_type_id  CHAR(36)    NOT NULL REFERENCES rate_types(id),
  rate_nano     BIGINT      NOT NULL,                  -- ×10⁹; integer, never a float
  valid_from    CHAR(10)    NOT NULL,                  -- date
  valid_to      CHAR(10),                              -- optional explicit expiry
  source        VARCHAR(32) NOT NULL DEFAULT 'manual', -- 'manual' | 'provider:<key>'
  batch_id      CHAR(36),                              -- reserved for the Phase 5 preview
  created_at    CHAR(24)    NOT NULL,
  updated_at    CHAR(24)    NOT NULL,
  row_version   BIGINT      NOT NULL DEFAULT 1,

  CONSTRAINT ux_exchange_rates
    UNIQUE (from_currency, to_currency, rate_type_id, valid_from, created_at),
  CONSTRAINT ck_exchange_rates_positive CHECK (rate_nano > 0),
  CONSTRAINT ck_exchange_rates_range    CHECK (valid_to IS NULL OR valid_to >= valid_from),
  -- A pair must differ: an identity rate is computed, never stored, so a stored one could
  -- only ever contradict it.
  CONSTRAINT ck_exchange_rates_pair     CHECK (from_currency <> to_currency)
);

-- The resolver's hot query: newest rate for a pair and type, valid on a date.
CREATE INDEX ix_exchange_rates_lookup
  ON exchange_rates (from_currency, to_currency, rate_type_id, valid_from);

-- Supports the Phase 5 preview's "show me this batch" and its roll-back-as-a-unit.
CREATE INDEX ix_exchange_rates_batch ON exchange_rates (batch_id);
