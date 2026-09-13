-- 0001_lite_platform.sql — what Mizan Lite needs before any business table exists.
--
-- The first four tables are VERBATIM copies of Mizan's 0001_platform.sql, statement for statement.
-- platform/migrate expects its bookkeeping to have exactly this shape once version 1 is applied,
-- platform/backup reads schema_migrations for its manifest, and platform/jobs reads and writes
-- jobs and job_runs. Mizan's 0001 cannot be split to share them, because an applied migration's
-- checksum is verified on every boot. TestPlatformTablesMatchMizan asserts they stay identical.

-- ─────────────────────────────────────────────────────────────────────────────
-- Migration bookkeeping (owned by platform/migrate) — verbatim from Mizan
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE schema_migrations (
  version      BIGINT       NOT NULL PRIMARY KEY,
  name         VARCHAR(200) NOT NULL,
  checksum     CHAR(64)     NOT NULL,               -- SHA-256 of the migration bytes
  applied_at   CHAR(24)     NOT NULL,
  duration_ms  BIGINT       NOT NULL DEFAULT 0
);

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
-- Durable jobs (owned by platform/jobs) — verbatim from Mizan
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
-- Settings (Lite's own shape)
-- ─────────────────────────────────────────────────────────────────────────────

-- Declared in Go, typed at the call site. A row exists only once a value differs from what was
-- declared; an unknown key is logged and ignored at load, never fatal (Mizan 0.5 D3), so an older
-- binary opening a newer database still starts.
CREATE TABLE settings (
  setting_key  VARCHAR(64)  NOT NULL PRIMARY KEY,
  value        TEXT         NOT NULL,
  updated_at   CHAR(24)     NOT NULL,
  CONSTRAINT ck_settings_key_present CHECK (setting_key <> '')
);
