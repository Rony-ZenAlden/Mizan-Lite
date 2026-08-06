-- 0008_audit — the append-only audit trail (§15.1).
--
-- Owned by the audit module. Identity owns 0005–0007, so audit owns 0008.
--
-- This migration and the READ path land in Step 1.6, because field-level redaction (§14.2)
-- needs a response with a field worth gating and audit payloads are the first real one. The
-- WRITE path — the Auditable event and its in-transaction subscribers — is Step 1.7, which is
-- what that step is actually about.

CREATE TABLE audit_log (
  id                   CHAR(36)    NOT NULL PRIMARY KEY,
  occurred_at          CHAR(24)    NOT NULL,

  -- Actor, with a SNAPSHOT of the name (§15.1).
  --
  -- The snapshot is the point: a five-year-old entry must stay readable after the user is
  -- renamed, deactivated, or replaced. Resolving the name through a join at read time would
  -- make the identity table load-bearing for history — the same reasoning that kept a foreign
  -- key off created_by in 0004 (Step 1.1, D4).
  actor_user_id        CHAR(36),
  actor_name_snapshot  VARCHAR(120),

  branch_id            CHAR(36),
  session_id           CHAR(36),
  -- Ties every record produced by ONE user action together (§15.1). Essential once posting an
  -- invoice cascades into stock movements and journal entries; stamped per call since 0.10.
  correlation_id       CHAR(36),

  action               VARCHAR(80) NOT NULL,     -- 'sales.invoice.posted'
  entity_type          VARCHAR(60),
  entity_id            CHAR(36),
  entity_label_snapshot VARCHAR(200),

  -- The restricted payload. Gated behind audit.entry.view_payload, because these columns hold
  -- exactly the detail a junior user should not read — an old salary, an old price, an old
  -- credit limit — while the FACT that a change happened is ordinary operational information.
  before_json          TEXT,
  after_json           TEXT,
  changed_fields       TEXT,

  source               VARCHAR(10) NOT NULL DEFAULT 'ui',   -- ui|job|import|system
  device_info          VARCHAR(200),

  -- Reserved for the enterprise edition's hash chain (§15.2, decision 8). Created nullable and
  -- left NULL in v1 on purpose: a chain CANNOT be backfilled — hashes computed retroactively
  -- over rows that were never protected prove nothing about the period before protection
  -- existed. When chaining is enabled it writes a genesis record and protects forward from
  -- there, reporting the protected range honestly.
  prev_hash            CHAR(64),
  row_hash             CHAR(64),

  created_at           CHAR(24)    NOT NULL,

  CONSTRAINT ck_audit_log_source CHECK (source IN ('ui', 'job', 'import', 'system'))
);

-- The list screen reads newest-first, optionally narrowed by entity or actor.
CREATE INDEX ix_audit_log_occurred ON audit_log (occurred_at);
CREATE INDEX ix_audit_log_entity   ON audit_log (entity_type, entity_id, occurred_at);
CREATE INDEX ix_audit_log_actor    ON audit_log (actor_user_id, occurred_at);
