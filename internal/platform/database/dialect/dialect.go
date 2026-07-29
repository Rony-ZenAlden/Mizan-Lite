// Package dialect isolates every database-specific behaviour behind one small
// interface. It is the ENTIRE portability surface: a future PostgreSQL / MySQL /
// SQL Server port implements this interface (plus a migrations directory) and nothing
// else in the codebase is allowed to be dialect-aware.
//
// v1 ships the SQLite implementation only.
package dialect

// Stable error codes produced by TranslateError. They double as i18n keys and are
// part of the public contract — never rename once shipped.
const (
	CodeDuplicate              = "database.duplicate"
	CodeReferenceViolation     = "database.reference_violation"
	CodeConstraintViolation    = "database.constraint_violation"
	CodeConcurrentModification = "database.concurrent_modification"
	CodeInternal               = "database.internal"
)

// UpsertSpec describes an insert-or-update. Upsert is abstracted because it is the
// single most divergent portable operation across engines (SQLite/Postgres ON
// CONFLICT, MySQL ON DUPLICATE KEY, SQL Server MERGE).
type UpsertSpec struct {
	Table        string   // target table (unquoted)
	Columns      []string // all columns to insert (unquoted), in placeholder order
	ConflictCols []string // the unique key columns that trigger the update
	UpdateCols   []string // columns to overwrite with the attempted-insert values
}

// Dialect is the complete set of database-specific behaviour.
type Dialect interface {
	// Name identifies the dialect, e.g. "sqlite".
	Name() string

	// Placeholder returns the nth positional placeholder (1-based): "?" for
	// SQLite/MySQL, "$n" for PostgreSQL. For building dynamic clauses (e.g. IN).
	Placeholder(n int) string

	// Rebind converts a query written with '?' placeholders to the dialect's native
	// form. Identity for SQLite/MySQL; '?'→'$n' for PostgreSQL.
	Rebind(query string) string

	// QuoteIdentifier quotes a table/column name safely for the dialect.
	QuoteIdentifier(name string) string

	// LimitOffset renders a portable LIMIT/OFFSET clause. limit and offset are
	// integers (never user strings), so embedding them is injection-safe.
	LimitOffset(limit, offset int64) string

	// Upsert renders a dialect-correct insert-or-update statement for spec.
	Upsert(spec UpsertSpec) string

	// ForUpdateClause returns the row-locking clause for gapless sequences:
	// "" on SQLite (the single writer already serialises), "FOR UPDATE" on Postgres.
	ForUpdateClause() string

	// BooleanLiteral renders a boolean per the portable SMALLINT(0/1) contract (§8.1).
	BooleanLiteral(b bool) string

	// TranslateError maps a driver error to a typed kernel/errs value with a stable
	// category and Code, so business code branches identically on every engine.
	// Non-driver errors (e.g. sql.ErrNoRows, context errors) are returned unchanged.
	TranslateError(err error) error

	// ── Schema-administration surface (used by platform/migrate) ────────────────
	//
	// These exist so the migration runner contains no engine-specific SQL. Without
	// them the runner would reach for sqlite_master, PRAGMA, and VACUUM INTO
	// directly, and a PostgreSQL port would become a diff across migrate/ instead
	// of a new Dialect implementation.

	// TableExistsQuery returns a query reporting whether one table exists. It takes
	// exactly one '?' parameter — the unquoted table name — and yields at least one
	// row when the table is present and no rows when it is absent.
	TableExistsQuery() string

	// IntegrityCheckStatement returns a statement that verifies physical database
	// integrity: one row per problem found, or a single row reading "ok" when the
	// database is healthy.
	//
	// Empty when the engine offers no such check (PostgreSQL, MySQL). Callers must
	// treat empty as "this engine cannot self-check" and skip the step, rather than
	// substituting a query that proves nothing.
	IntegrityCheckStatement() string

	// OnlineBackupStatement returns a statement writing a consistent snapshot of the
	// live database to dest while connections are open. dest is escaped for the
	// dialect by the implementation.
	//
	// Empty when the engine has no in-engine equivalent (PostgreSQL and MySQL back up
	// through external tooling). Callers must treat empty as "no backup was taken"
	// and refuse to proceed, never as success — a safety net nobody verified is worse
	// than none, because it is trusted.
	OnlineBackupStatement(dest string) string
}
