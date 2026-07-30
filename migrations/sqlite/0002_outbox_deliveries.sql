-- 0002_outbox_deliveries — per-handler delivery tracking for the transactional outbox.
--
-- Step 0.6, decision D3 (amended to Option A). `outbox_events` records that something
-- happened; this table records, for each registered handler, whether that handler has
-- actually processed it.
--
-- Why per-handler rather than one status per event: with a single status, a retry after one
-- handler failed would re-run ALL of them, including the ones that already succeeded. The
-- first real consumer is accounting posting, where a duplicate is a wrong ledger that
-- balances — the hardest kind of error to notice. Correctness must not depend on every
-- handler author remembering to be idempotent.
--
-- Rows are created inside the SAME transaction as the event, one per handler registered for
-- that event type at publish time. That makes the rule exact: a subscriber receives only
-- events published after it was registered.

CREATE TABLE outbox_deliveries (
  event_id        CHAR(36)     NOT NULL,
  handler         VARCHAR(120) NOT NULL,
  status          VARCHAR(16)  NOT NULL DEFAULT 'pending',
  attempts        BIGINT       NOT NULL DEFAULT 0,
  -- next_attempt_at doubles as the visibility deadline while status = 'processing': a row
  -- past it is presumed abandoned by a crashed dispatcher and is reclaimed (design §5.3).
  next_attempt_at CHAR(24),
  last_error      TEXT,
  processed_at    CHAR(24),
  created_at      CHAR(24)     NOT NULL,
  updated_at      CHAR(24)     NOT NULL,

  -- Composite natural key rather than the usual surrogate id CHAR(36): it makes a duplicate
  -- delivery row impossible by construction instead of by convention. Platform bookkeeping
  -- tables already key this way (schema_migrations keys on version).
  CONSTRAINT pk_outbox_deliveries PRIMARY KEY (event_id, handler),
  CONSTRAINT fk_outbox_deliveries_event FOREIGN KEY (event_id) REFERENCES outbox_events(id),
  CONSTRAINT ck_outbox_deliveries_status
    CHECK (status IN ('pending', 'processing', 'done', 'failed', 'dead'))
);

-- The dispatcher's hot query: work that is due, oldest first.
CREATE INDEX ix_outbox_deliveries_due ON outbox_deliveries (status, next_attempt_at);

-- Supports the per-(aggregate, handler) ordering check and the diagnostics screen's
-- "what is stuck for this handler?" view.
CREATE INDEX ix_outbox_deliveries_handler ON outbox_deliveries (handler, status);
