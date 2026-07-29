-- 0001_platform — tables owned by the platform itself.
--
-- No business tables live here; those arrive with their modules. Every column follows
-- the portable type contract (ARCHITECTURE_v1 §8.1) and naming rules (§8.3):
--   ID          CHAR(36)   UUIDv7
--   Timestamp   CHAR(24)   ISO-8601 UTC, e.g. 2026-07-27T10:30:00.000Z
--   Date        CHAR(10)   2026-07-27
--   Boolean     SMALLINT + CHECK (x IN (0,1))
--   Enum        VARCHAR(n) + CHECK
--   Money/qty   BIGINT     (minor units / scaled integers — never REAL)
--
-- Rules honoured: no AUTOINCREMENT, no rowid reliance, no partial/expression indexes,
-- no triggers, no SQLite-only syntax, no reserved words as identifiers.
--
-- `created_by` / `updated_by` intentionally carry NO foreign key: the users table does
-- not exist until Phase 1, and a platform table must not depend on a business module.

-- ─────────────────────────────────────────────────────────────────────────────
-- Migration bookkeeping (owned by platform/migrate)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE schema_migrations (
  version      BIGINT       NOT NULL PRIMARY KEY,
  name         VARCHAR(200) NOT NULL,
  checksum     CHAR(64)     NOT NULL,               -- SHA-256 of the migration bytes
  applied_at   CHAR(24)     NOT NULL,
  duration_ms  BIGINT       NOT NULL DEFAULT 0
);

-- Single-row cross-process advisory lock, so two app instances cannot migrate at once.
CREATE TABLE schema_lock (
  lock_id      INTEGER      NOT NULL PRIMARY KEY,
  is_locked    SMALLINT     NOT NULL DEFAULT 0,
  locked_at    CHAR(24),
  locked_by    VARCHAR(200),
  CONSTRAINT ck_schema_lock_single CHECK (lock_id = 1),
  CONSTRAINT ck_schema_lock_bool   CHECK (is_locked IN (0, 1))
);

INSERT INTO schema_lock (lock_id, is_locked) VALUES (1, 0);

-- ─────────────────────────────────────────────────────────────────────────────
-- Settings & feature flags (adopted by Step 0.5)
-- ─────────────────────────────────────────────────────────────────────────────

-- scope_id is NOT NULL DEFAULT '' rather than nullable: SQL treats NULLs as distinct
-- in a UNIQUE constraint, so a nullable scope_id would silently permit duplicate
-- system-scope settings for the same key.
CREATE TABLE settings (
  id            CHAR(36)     NOT NULL PRIMARY KEY,
  scope         VARCHAR(16)  NOT NULL,
  scope_id      CHAR(36)     NOT NULL DEFAULT '',
  setting_key   VARCHAR(120) NOT NULL,
  setting_value TEXT,
  value_type    VARCHAR(16)  NOT NULL,
  created_at    CHAR(24)     NOT NULL,
  created_by    CHAR(36),
  updated_at    CHAR(24)     NOT NULL,
  updated_by    CHAR(36),
  row_version   BIGINT       NOT NULL DEFAULT 1,
  CONSTRAINT ux_settings_scope_key UNIQUE (scope, scope_id, setting_key),
  CONSTRAINT ck_settings_scope CHECK (scope IN ('system', 'company', 'branch', 'user')),
  CONSTRAINT ck_settings_type  CHECK (value_type IN ('string','int','bool','money','json','enum','id','duration'))
);

CREATE INDEX ix_settings_key ON settings (setting_key);

CREATE TABLE feature_flags (
  id          CHAR(36)     NOT NULL PRIMARY KEY,
  flag_key    VARCHAR(120) NOT NULL,
  scope       VARCHAR(16)  NOT NULL,
  scope_id    CHAR(36)     NOT NULL DEFAULT '',
  is_enabled  SMALLINT     NOT NULL DEFAULT 0,
  created_at  CHAR(24)     NOT NULL,
  created_by  CHAR(36),
  updated_at  CHAR(24)     NOT NULL,
  updated_by  CHAR(36),
  row_version BIGINT       NOT NULL DEFAULT 1,
  CONSTRAINT ux_feature_flags_key UNIQUE (flag_key, scope, scope_id),
  CONSTRAINT ck_feature_flags_scope CHECK (scope IN ('system', 'company', 'branch', 'user')),
  CONSTRAINT ck_feature_flags_bool  CHECK (is_enabled IN (0, 1))
);

-- ─────────────────────────────────────────────────────────────────────────────
-- Transactional outbox (adopted by Step 0.6)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE outbox_events (
  id              CHAR(36)     NOT NULL PRIMARY KEY,
  occurred_at     CHAR(24)     NOT NULL,
  event_type      VARCHAR(120) NOT NULL,
  aggregate_type  VARCHAR(120) NOT NULL,
  aggregate_id    CHAR(36)     NOT NULL,
  payload_json    TEXT         NOT NULL,
  correlation_id  CHAR(36),
  causation_id    CHAR(36),
  branch_id       CHAR(36),
  status          VARCHAR(16)  NOT NULL DEFAULT 'pending',
  attempts        BIGINT       NOT NULL DEFAULT 0,
  next_attempt_at CHAR(24),
  last_error      TEXT,
  processed_at    CHAR(24),
  CONSTRAINT ck_outbox_status CHECK (status IN ('pending','processing','done','failed','dead'))
);

-- The dispatcher's hot query: pending work, oldest first.
CREATE INDEX ix_outbox_events_dispatch ON outbox_events (status, next_attempt_at, occurred_at);
CREATE INDEX ix_outbox_events_aggregate ON outbox_events (aggregate_type, aggregate_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- Durable job scheduler (adopted by Step 0.7)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE jobs (
  id               CHAR(36)     NOT NULL PRIMARY KEY,
  job_key          VARCHAR(120) NOT NULL,
  schedule_cron    VARCHAR(120),
  is_enabled       SMALLINT     NOT NULL DEFAULT 1,
  is_singleton     SMALLINT     NOT NULL DEFAULT 1,
  catch_up_policy  VARCHAR(16)  NOT NULL DEFAULT 'run_once',
  timeout_seconds  BIGINT       NOT NULL DEFAULT 300,
  max_attempts     BIGINT       NOT NULL DEFAULT 3,
  last_run_at      CHAR(24),
  next_run_at      CHAR(24),
  created_at       CHAR(24)     NOT NULL,
  updated_at       CHAR(24)     NOT NULL,
  row_version      BIGINT       NOT NULL DEFAULT 1,
  CONSTRAINT ck_jobs_key_present CHECK (job_key <> ''),
  CONSTRAINT ck_jobs_enabled   CHECK (is_enabled IN (0, 1)),
  CONSTRAINT ck_jobs_singleton CHECK (is_singleton IN (0, 1)),
  CONSTRAINT ck_jobs_catchup   CHECK (catch_up_policy IN ('run_once','skip','run_all'))
);

CREATE UNIQUE INDEX ux_jobs_job_key ON jobs (job_key);
CREATE INDEX ix_jobs_due ON jobs (is_enabled, next_run_at);

CREATE TABLE job_runs (
  id           CHAR(36)    NOT NULL PRIMARY KEY,
  job_id       CHAR(36)    NOT NULL REFERENCES jobs(id),
  started_at   CHAR(24)    NOT NULL,
  finished_at  CHAR(24),
  status       VARCHAR(16) NOT NULL,
  attempt      BIGINT      NOT NULL DEFAULT 1,
  error        TEXT,
  output_json  TEXT,
  triggered_by VARCHAR(16) NOT NULL DEFAULT 'schedule',
  CONSTRAINT ck_job_runs_status  CHECK (status IN ('running','succeeded','failed','timeout','cancelled')),
  CONSTRAINT ck_job_runs_trigger CHECK (triggered_by IN ('schedule','manual','startup'))
);

CREATE INDEX ix_job_runs_job ON job_runs (job_id, started_at);

-- ─────────────────────────────────────────────────────────────────────────────
-- User-content translations (adopted by Step 0.8)
-- ─────────────────────────────────────────────────────────────────────────────

-- Generic side table so adding a language is a DATA operation, never a migration on
-- every entity table (ARCHITECTURE_v1 §9.5).
CREATE TABLE translations (
  id          CHAR(36)     NOT NULL PRIMARY KEY,
  entity_type VARCHAR(120) NOT NULL,
  entity_id   CHAR(36)     NOT NULL,
  field_name  VARCHAR(120) NOT NULL,
  locale      VARCHAR(16)  NOT NULL,
  value       TEXT         NOT NULL,
  created_at  CHAR(24)     NOT NULL,
  updated_at  CHAR(24)     NOT NULL,
  row_version BIGINT       NOT NULL DEFAULT 1,
  CONSTRAINT ux_translations UNIQUE (entity_type, entity_id, field_name, locale)
);

CREATE INDEX ix_translations_lookup ON translations (entity_type, entity_id, locale);

-- ─────────────────────────────────────────────────────────────────────────────
-- Document number series (platform-level; used by every transactional module)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE number_series (
  id             CHAR(36)     NOT NULL PRIMARY KEY,
  code           VARCHAR(60)  NOT NULL,
  branch_id      CHAR(36)     NOT NULL DEFAULT '',
  fiscal_year_id CHAR(36)     NOT NULL DEFAULT '',
  prefix         VARCHAR(20)  NOT NULL DEFAULT '',
  suffix         VARCHAR(20)  NOT NULL DEFAULT '',
  padding        BIGINT       NOT NULL DEFAULT 6,
  next_value     BIGINT       NOT NULL DEFAULT 1,
  is_gapless     SMALLINT     NOT NULL DEFAULT 0,
  created_at     CHAR(24)     NOT NULL,
  updated_at     CHAR(24)     NOT NULL,
  row_version    BIGINT       NOT NULL DEFAULT 1,
  CONSTRAINT ux_number_series UNIQUE (code, branch_id, fiscal_year_id),
  CONSTRAINT ck_number_series_gapless CHECK (is_gapless IN (0, 1)),
  CONSTRAINT ck_number_series_padding CHECK (padding >= 0)
);
