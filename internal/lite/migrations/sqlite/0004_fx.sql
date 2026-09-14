-- 0004_fx.sql — Phase L3: exchange rates, fetched and manual.
--
-- Design: docs/mizan_lite/phases/L3_RATES.md §6 and §14.7 (the dual-mode requirement). One direction only: local currency
-- per ONE US dollar, at 10⁻⁹. The inverse is never stored. Both tables are INSERT-only.

-- Every attempt to fetch a rate from the internet, including failures (L3 §14.5). A row says what the provider answered,
-- what the rate in force was, and what was decided — never the provider's own text.
CREATE TABLE fx_fetches (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  local_currency      CHAR(3)      NOT NULL REFERENCES currencies(code),
  -- Attempts in order, 1, 2, 3 … — never ordered by the shop PC's clock (L2 D-L2.i1, L3 A-L3.1).
  seq                 BIGINT       NOT NULL CHECK (seq >= 1),
  attempted_at        CHAR(24)     NOT NULL,
  business_date       CHAR(10)     NOT NULL,
  outcome             VARCHAR(12)  NOT NULL CHECK (outcome IN
                        ('applied', 'unchanged', 'proposed', 'held_mode', 'held_today', 'failed')),
  provider            VARCHAR(32),
  local_per_usd_nano  BIGINT,
  -- The rate in force the quote was compared with; NULL when there was none. A proposal is accepted only while it still is.
  against_rate_id     CHAR(36)     REFERENCES fx_rates(id),
  error_code          VARCHAR(64),
  CONSTRAINT ux_fx_fetches_place UNIQUE (local_currency, seq),
  -- Every comparison guarded: a CHECK that evaluates to NULL passes (L2 §4).
  CONSTRAINT ck_fx_fetch_answered CHECK (
    (outcome = 'failed' AND provider IS NULL AND local_per_usd_nano IS NULL AND error_code IS NOT NULL) OR
    (outcome <> 'failed' AND provider IS NOT NULL AND local_per_usd_nano IS NOT NULL AND local_per_usd_nano > 0
                         AND error_code IS NULL))
);

-- The rates. The rate in force is the highest place — never the latest timestamp (L3 A-L3.1).
CREATE TABLE fx_rates (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  local_currency      CHAR(3)      NOT NULL REFERENCES currencies(code),
  seq                 BIGINT       NOT NULL CHECK (seq >= 1),
  local_per_usd_nano  BIGINT       NOT NULL CHECK (local_per_usd_nano > 0),
  source              VARCHAR(12)  NOT NULL CHECK (source IN ('first_run', 'manual', 'fetched')),
  fetch_id            CHAR(36)     REFERENCES fx_fetches(id),
  business_date       CHAR(10)     NOT NULL,     -- the shop's day it was recorded on: what "out of date" is measured by
  recorded_at         CHAR(24)     NOT NULL,     -- UTC; for the history and the age, never for resolution
  note                VARCHAR(200),
  CONSTRAINT ck_fx_rates_local_is_not_usd CHECK (local_currency <> 'USD'),
  CONSTRAINT ck_fx_rates_fetched_names_its_fetch CHECK ((source = 'fetched') = (fetch_id IS NOT NULL)),
  CONSTRAINT ux_fx_rates_place UNIQUE (local_currency, seq),
  CONSTRAINT ux_fx_rates_fetch_applied_once UNIQUE (fetch_id)
);
