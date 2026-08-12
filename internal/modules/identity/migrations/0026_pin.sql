-- 0026_pin — PIN sessions for the till (§979).
--
-- # What a PIN is, and what it is not
--
-- It is NOT a shorter password. A four-digit PIN has ten thousand possibilities; no hashing
-- parameter makes that safe on its own, and pretending otherwise is how a system ends up with a
-- credential that looks protected and is not.
--
-- What makes it safe is three things together, and it needs all three:
--
--   1. It is only usable on a device that ALREADY holds a full session. A PIN is never a way in
--      from nothing — it is a way to change who is standing at a till the shop has already
--      unlocked with a real password.
--   2. The session it issues is SCOPED to point-of-sale permissions. If a PIN is guessed, the
--      attacker gets a till, not the admin screen — and that bound holds however weak the PIN is.
--   3. It is throttled harder than a password, per user AND per device, because the search space
--      is small enough to exhaust in an afternoon otherwise.
--
-- The credential itself needs no new table: `user_credentials` already accepts `credential_type
-- = 'pin'` (0005), which Phase 1 left deliberately. What is new is the SESSION's knowledge of how
-- it was authenticated, because that is what scoping depends on.

-- How a session was authenticated.
--
-- `password` is a full session; `pin` is a till session and carries the scoping above. Defaulting
-- to `password` means every session that existed before this migration keeps exactly the
-- authority it had.
ALTER TABLE sessions ADD COLUMN auth_method VARCHAR(12) NOT NULL DEFAULT 'password';

-- The device session a PIN session was issued from.
--
-- Recorded rather than merely checked, for two reasons: ending the device session must end every
-- till session it authorised (a shop that logs out its terminal should not leave four cashiers
-- signed in), and "which terminal was this sale rung up on" is an audit question that a foreign
-- key answers and a log line does not.
ALTER TABLE sessions ADD COLUMN device_session_id CHAR(36) REFERENCES sessions(id);

CREATE INDEX ix_sessions_device ON sessions (device_session_id);
