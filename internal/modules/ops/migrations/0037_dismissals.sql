-- 0037_dismissals — what a user has chosen not to be told again.
--
-- # Why this table holds dismissals and NOT notifications
--
-- The obvious design is a `notifications` table: something detects a condition, writes a row, a
-- screen reads it, the user dismisses it.
--
-- That table would be a projection of facts that live elsewhere — due dates, stock levels, job
-- outcomes — with no rebuild, no verifier and no drift report. Phase 8 refused exactly that shape
-- for reporting, and the reason is recorded there: the moment a fact needs a third home, the
-- second home was the wrong one.
--
-- It would also go STALE. "Invoice 4471 is overdue" survives the invoice being paid, and a user
-- told three times about something they already fixed stops reading notifications at all — which
-- costs more than never having had them.
--
-- So notices are computed from the current state every time, and a condition that resolves takes
-- its notice with it. What genuinely needs storing is this: "I have seen it, do not tell me
-- again" is a fact about the USER, and lives nowhere else.

CREATE TABLE notice_dismissals (
  id          CHAR(36)     NOT NULL PRIMARY KEY,
  company_id  CHAR(36)     NOT NULL REFERENCES companies(id),

  -- PER USER, not per company.
  --
  -- One person deciding they do not need to see the overdue-invoice warning must not silence it
  -- for the owner. The two have different jobs and different reasons to look.
  user_id     CHAR(36)     NOT NULL REFERENCES users(id),

  -- The rule-derived key, e.g. "sales.overdue.<documentId>". Derived rather than generated, so
  -- the same condition produces the same key on every run — a key that changed between runs
  -- would make dismissing useless.
  notice_key  VARCHAR(200) NOT NULL,

  -- Which rule produced it, so a screen can offer "stop telling me about overdue invoices"
  -- without parsing the key.
  rule_name   VARCHAR(80)  NOT NULL,

  dismissed_at VARCHAR(32) NOT NULL,

  CONSTRAINT ux_notice_dismissals UNIQUE (company_id, user_id, notice_key)
);

CREATE INDEX ix_notice_dismissals_user ON notice_dismissals (company_id, user_id);
