-- 0009_profiles — the business-profile catalogue (§16.4).
--
-- Owned by the profile module. Audit owns 0008, so profile owns 0009.
--
-- Note what is NOT here: country_profiles.
--
-- Country profiles are files and are never stored (Step 1.8, D1). They are read once, at setup,
-- to supply defaults; the wizard then copies every value into a setting or a company column,
-- which from that moment is the only source of truth. A stored profile row would be a SECOND
-- answer to "what is this company's date format?" — one that never changes, and that some
-- future query would eventually read instead of the setting. §C.2 is explicit that the wizard
-- must not become a hidden source of truth, and a stored profile is precisely how that happens.
--
-- Note also what this table does NOT hold: the bundle. The settings and flags a profile applies
-- stay in the file. Storing them would invite editing the stored copy, which would then
-- disagree with the file it came from, and nothing could say which had been applied.

CREATE TABLE business_profiles (
  id           CHAR(36)     NOT NULL PRIMARY KEY,

  -- The metadata contract (§CFG.3). `code` is the stable machine key every reference uses;
  -- `is_system` marks a row the product ships and therefore maintains, which is what lets a
  -- re-seed correct it while leaving an administrator's own rows untouched.
  code         VARCHAR(40)  NOT NULL,
  name         VARCHAR(120) NOT NULL,
  is_system    INTEGER      NOT NULL DEFAULT 0,
  is_active    INTEGER      NOT NULL DEFAULT 1,

  -- name_key is the translation key for `name` (§22). The English name is kept as a fallback
  -- for a locale with no translation, never as the source of truth.
  name_key     VARCHAR(120),
  description  VARCHAR(400),

  created_at   CHAR(24)     NOT NULL,
  updated_at   CHAR(24)     NOT NULL,

  CONSTRAINT ck_business_profiles_system CHECK (is_system IN (0, 1)),
  CONSTRAINT ck_business_profiles_active CHECK (is_active IN (0, 1))
);

-- The catalogue is keyed by code everywhere: seeds match on it, and the wizard selects by it.
CREATE UNIQUE INDEX ux_business_profiles_code ON business_profiles (code);
