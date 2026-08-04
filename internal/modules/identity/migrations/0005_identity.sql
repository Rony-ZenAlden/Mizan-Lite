-- 0005_identity — users and their credentials.
--
-- Owned by the module (§10.3). Org owns 0004, so identity owns 0005.
--
-- Sessions, lockout, and login_attempts are NOT here: they arrive with 0006 in Step 1.3.
-- Splitting them keeps this migration about one thing — who exists and how they prove it.

-- ─────────────────────────────────────────────────────────────────────────────
-- users
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Note what is ABSENT: any credential column. Hashes live in user_credentials so that no
-- ordinary user query — a repository read, a report, a future export, a support session —
-- can return one. That is a structural guarantee rather than a review convention, and it
-- costs exactly one join on the single code path that needs it.

CREATE TABLE users (
  id           CHAR(36)     NOT NULL PRIMARY KEY,
  company_id   CHAR(36)     NOT NULL REFERENCES companies(id),
  username     VARCHAR(60)  NOT NULL,
  display_name VARCHAR(120) NOT NULL,
  email        VARCHAR(160),
  -- Per-user language. The scope chain from 0.5 already resolves ui.locale at user scope;
  -- this is the column that finally makes that real (Step 0.11 D4 wrote at system scope
  -- because no user existed).
  locale       VARCHAR(20),
  -- The setup administrator. Protected from deletion, never from deactivation — an account
  -- that cannot be switched off would be a liability of its own.
  is_system    SMALLINT     NOT NULL DEFAULT 0,
  is_active    SMALLINT     NOT NULL DEFAULT 1,
  created_at   CHAR(24)     NOT NULL,
  created_by   CHAR(36),
  updated_at   CHAR(24)     NOT NULL,
  updated_by   CHAR(36),
  row_version  BIGINT       NOT NULL DEFAULT 1,

  CONSTRAINT ux_users_username UNIQUE (company_id, username),
  CONSTRAINT ck_users_system   CHECK (is_system IN (0, 1)),
  CONSTRAINT ck_users_active   CHECK (is_active IN (0, 1))
);

CREATE INDEX ix_users_company ON users (company_id, is_active);

-- ─────────────────────────────────────────────────────────────────────────────
-- user_credentials
-- ─────────────────────────────────────────────────────────────────────────────
--
-- `encoded` holds a PHC string: $argon2id$v=19$m=…,t=…,p=…$salt$hash (Step 1.2, D2). It is
-- self-describing, so the salt and every cost parameter travel WITH the hash — which is what
-- makes §13.1's promise real: raising the cost later cannot invalidate existing passwords,
-- because each one still carries the parameters it was made with.
--
-- There is deliberately no params_json column. The PHC string already carries them, and a
-- second copy of one fact is what 0.4 rejected goose over and what 0.7 D2 refused for job
-- state. `algorithm` is kept only as a discriminator for a future migration off Argon2,
-- because parsing a PHC prefix in SQL is not portable.
--
-- credential_type reserves 'pin' for Phase 5 (phase design D2): a PIN is meaningless without
-- the POS screen that switches cashiers, and its rules are that design's to make.

CREATE TABLE user_credentials (
  id              CHAR(36)    NOT NULL PRIMARY KEY,
  user_id         CHAR(36)    NOT NULL REFERENCES users(id),
  credential_type VARCHAR(12) NOT NULL DEFAULT 'password',
  algorithm       VARCHAR(20) NOT NULL,
  encoded         TEXT        NOT NULL,
  must_change     SMALLINT    NOT NULL DEFAULT 0,
  expires_at      CHAR(24),
  created_at      CHAR(24)    NOT NULL,
  updated_at      CHAR(24)    NOT NULL,
  row_version     BIGINT      NOT NULL DEFAULT 1,

  -- One password and (later) one PIN per user, enforced by the database rather than by
  -- whichever code happens to write it.
  CONSTRAINT ux_user_credentials UNIQUE (user_id, credential_type),
  CONSTRAINT ck_user_credentials_type   CHECK (credential_type IN ('password', 'pin')),
  CONSTRAINT ck_user_credentials_change CHECK (must_change IN (0, 1))
);

-- ─────────────────────────────────────────────────────────────────────────────
-- password_history
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Stops the trivial "change it, then change it straight back". Bounded by the
-- identity.password.history setting; older rows are pruned on each successful change so this
-- table cannot grow without limit.

CREATE TABLE password_history (
  id         CHAR(36) NOT NULL PRIMARY KEY,
  user_id    CHAR(36) NOT NULL REFERENCES users(id),
  encoded    TEXT     NOT NULL,
  created_at CHAR(24) NOT NULL
);

CREATE INDEX ix_password_history_user ON password_history (user_id, created_at);
