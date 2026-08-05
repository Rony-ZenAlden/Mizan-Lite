-- 0007_rbac — permissions, roles, grants, and assignments.
--
-- Owned by identity, which owns 0005 and 0006. Third migration of the module, split so each
-- answers one question: who exists (0005), are they still signed in (0006), what may they do.

-- ─────────────────────────────────────────────────────────────────────────────
-- permissions — CODE-DEFINED, synced at startup (§14.1)
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Never user-created: each module declares what it protects through Module.Permissions(), and
-- startup reconciles the table. That is what stops the permission list drifting from what the
-- code actually checks.
--
-- is_obsolete rather than DELETE (Step 1.4, D2). A permission the running build no longer
-- declares is a DATA LEFTOVER, not a code defect (0.10 D3): the row is inert once nothing
-- checks it, while deleting it would cascade to role_permissions and silently change what
-- every role grants. Marking it also lets a downgrade-then-upgrade restore the permission with
-- its grants intact.

CREATE TABLE permissions (
  id          CHAR(36)     NOT NULL PRIMARY KEY,
  code        VARCHAR(80)  NOT NULL,
  module      VARCHAR(40)  NOT NULL,
  description VARCHAR(200),                 -- i18n key, never prose (§22.2)
  is_obsolete SMALLINT     NOT NULL DEFAULT 0,
  created_at  CHAR(24)     NOT NULL,
  updated_at  CHAR(24)     NOT NULL,
  row_version BIGINT       NOT NULL DEFAULT 1,

  CONSTRAINT ux_permissions_code   UNIQUE (code),
  CONSTRAINT ck_permissions_obsolete CHECK (is_obsolete IN (0, 1))
);

-- ─────────────────────────────────────────────────────────────────────────────
-- roles — user-created, seeded with sensible defaults (§14.1)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE roles (
  id          CHAR(36)     NOT NULL PRIMARY KEY,
  company_id  CHAR(36)     NOT NULL REFERENCES companies(id),
  code        VARCHAR(40)  NOT NULL,
  name        VARCHAR(120) NOT NULL,
  description VARCHAR(200),
  -- Seeded roles are freely EDITABLE and never deletable: a shop that wants its Cashier to do
  -- more should say so, but deleting the role would orphan everyone assigned to it.
  is_system   SMALLINT     NOT NULL DEFAULT 0,
  is_active   SMALLINT     NOT NULL DEFAULT 1,
  created_at  CHAR(24)     NOT NULL,
  created_by  CHAR(36),
  updated_at  CHAR(24)     NOT NULL,
  updated_by  CHAR(36),
  row_version BIGINT       NOT NULL DEFAULT 1,

  CONSTRAINT ux_roles_code    UNIQUE (company_id, code),
  CONSTRAINT ck_roles_system  CHECK (is_system IN (0, 1)),
  CONSTRAINT ck_roles_active  CHECK (is_active IN (0, 1))
);

-- ─────────────────────────────────────────────────────────────────────────────
-- role_permissions — grants, BY CODE
-- ─────────────────────────────────────────────────────────────────────────────
--
-- permission_code, not permission_id (§CFG.3): code can then depend on
-- "sales.invoice.void" existing without hardcoding a UUID, and a grant survives its permission
-- being re-synced.
--
-- The value may be a WILDCARD — '*' or 'sales.*'. Wildcards are legal only here, on the grant
-- side (§14.1). A check must always name a concrete permission, or a careless call site could
-- ask "may I do sales.*?" and be told yes.

CREATE TABLE role_permissions (
  id              CHAR(36)    NOT NULL PRIMARY KEY,
  role_id         CHAR(36)    NOT NULL REFERENCES roles(id),
  permission_code VARCHAR(80) NOT NULL,
  created_at      CHAR(24)    NOT NULL,

  CONSTRAINT ux_role_permissions UNIQUE (role_id, permission_code)
);

CREATE INDEX ix_role_permissions_role ON role_permissions (role_id);

-- ─────────────────────────────────────────────────────────────────────────────
-- user_roles — assignments, optionally scoped (§14.2)
-- ─────────────────────────────────────────────────────────────────────────────
--
-- scope_kind NULL means the role is held EVERYWHERE. v1 seeds every assignment that way, but
-- the columns exist from the first migration because §14.2 is right that retrofitting scope
-- into every check later is the expensive path.

CREATE TABLE user_roles (
  id         CHAR(36)    NOT NULL PRIMARY KEY,
  user_id    CHAR(36)    NOT NULL REFERENCES users(id),
  role_id    CHAR(36)    NOT NULL REFERENCES roles(id),
  scope_kind VARCHAR(10),
  scope_id   CHAR(36),
  created_at CHAR(24)    NOT NULL,

  CONSTRAINT ux_user_roles UNIQUE (user_id, role_id, scope_kind, scope_id),
  CONSTRAINT ck_user_roles_scope CHECK (
    (scope_kind IS NULL AND scope_id IS NULL) OR
    (scope_kind IN ('branch', 'warehouse') AND scope_id IS NOT NULL)
  )
);

CREATE INDEX ix_user_roles_user ON user_roles (user_id);
