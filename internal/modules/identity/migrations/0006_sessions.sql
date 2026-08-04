-- 0006_sessions — sessions and the record of every login attempt.
--
-- Owned by identity, which also owns 0005. Split from it deliberately: 0005 is about who
-- exists and how they prove it; this is about staying proven.

-- ─────────────────────────────────────────────────────────────────────────────
-- sessions
-- ─────────────────────────────────────────────────────────────────────────────
--
-- token_hash, not the token (Step 1.3, D3). The row id is NOT the credential: a separate
-- 256-bit random token is returned to the caller and only its SHA-256 is stored.
--
-- Why hash it when the database sits on the user's own machine? Because databases travel — a
-- backup, a support copy, the portable .mizanbak export of Phase 9 — and a backup that
-- contains live session tokens is a backup that contains working logins.
--
-- Why SHA-256 rather than the Argon2id that 0005 insisted on for passwords? Different threat.
-- A session token is 256 bits of uniform randomness: there is no dictionary to attack and
-- nothing to slow down. Argon2 exists to protect LOW-ENTROPY secrets; using it here would cost
-- 19 MiB per request to defend against an attack that cannot work.
--
-- branch_id is carried because §26.1 requires the AppContext to always have a current branch,
-- so a future branch switch is a session update rather than a new concept.

CREATE TABLE sessions (
  id                  CHAR(36) NOT NULL PRIMARY KEY,
  user_id             CHAR(36) NOT NULL REFERENCES users(id),
  branch_id           CHAR(36) NOT NULL REFERENCES branches(id),
  token_hash          CHAR(64) NOT NULL,
  created_at          CHAR(24) NOT NULL,
  last_seen_at        CHAR(24) NOT NULL,
  -- Rolls forward on use.
  idle_expires_at     CHAR(24) NOT NULL,
  -- Fixed at creation and never extended: activity must not let a session live forever.
  absolute_expires_at CHAR(24) NOT NULL,
  -- Sessions are ENDED, never deleted on logout: "who was signed in when this happened" is an
  -- audit question, and a deleted row cannot answer it.
  ended_at            CHAR(24),
  end_reason          VARCHAR(12),
  device_info         VARCHAR(200),

  CONSTRAINT ux_sessions_token UNIQUE (token_hash),
  CONSTRAINT ck_sessions_reason CHECK (
    end_reason IS NULL OR end_reason IN ('logout', 'idle', 'absolute', 'revoked')
  )
);

CREATE INDEX ix_sessions_user ON sessions (user_id, ended_at);

-- ─────────────────────────────────────────────────────────────────────────────
-- login_attempts
-- ─────────────────────────────────────────────────────────────────────────────
--
-- user_id is NULLABLE and attempts against a username that does not exist ARE recorded
-- (§13.1): an attempt on a non-existent account is the shape of an attack, and discarding it
-- discards the evidence.
--
-- `reason` is the one place the distinction 0005 deliberately refuses to tell the caller is
-- written down. The audit trail knows whether it was an unknown user or a wrong password; the
-- person at the keyboard does not.

CREATE TABLE login_attempts (
  id           CHAR(36)    NOT NULL PRIMARY KEY,
  company_id   CHAR(36)    NOT NULL REFERENCES companies(id),
  username     VARCHAR(60) NOT NULL,
  user_id      CHAR(36),
  succeeded    SMALLINT    NOT NULL,
  reason       VARCHAR(24),
  device_info  VARCHAR(200),
  attempted_at CHAR(24)    NOT NULL,

  CONSTRAINT ck_login_attempts_ok CHECK (succeeded IN (0, 1))
);

-- The throttle reads "recent failures for this username", which is exactly this index.
CREATE INDEX ix_login_attempts_throttle
  ON login_attempts (company_id, username, attempted_at);
