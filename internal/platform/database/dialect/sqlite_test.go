package dialect_test

import (
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/platform/database/dialect"
)

func TestSQLiteStringMethods(t *testing.T) {
	d := dialect.NewSQLite()

	if d.Name() != "sqlite" {
		t.Errorf("Name = %q", d.Name())
	}
	if d.Placeholder(3) != "?" {
		t.Errorf("Placeholder = %q, want ?", d.Placeholder(3))
	}
	if d.Rebind("SELECT ? WHERE x = ?") != "SELECT ? WHERE x = ?" {
		t.Error("Rebind should be identity for SQLite")
	}
	if got := d.QuoteIdentifier(`sales"orders`); got != `"sales""orders"` {
		t.Errorf("QuoteIdentifier = %q", got)
	}
	if got := d.LimitOffset(2, 1); got != "LIMIT 2 OFFSET 1" {
		t.Errorf("LimitOffset = %q", got)
	}
	if d.ForUpdateClause() != "" {
		t.Error("ForUpdateClause should be empty for SQLite")
	}
	if d.BooleanLiteral(true) != "1" || d.BooleanLiteral(false) != "0" {
		t.Error("BooleanLiteral wrong")
	}
}

func TestSQLiteUpsert(t *testing.T) {
	d := dialect.NewSQLite()
	got := d.Upsert(dialect.UpsertSpec{
		Table:        "widgets",
		Columns:      []string{"id", "code", "qty"},
		ConflictCols: []string{"code"},
		UpdateCols:   []string{"qty"},
	})
	want := `INSERT INTO "widgets" ("id", "code", "qty") VALUES (?, ?, ?) ` +
		`ON CONFLICT ("code") DO UPDATE SET "qty" = excluded."qty"`
	if got != want {
		t.Errorf("Upsert =\n  %q\nwant\n  %q", got, want)
	}
}

func TestTranslateErrorPassthrough(t *testing.T) {
	d := dialect.NewSQLite()
	if d.TranslateError(nil) != nil {
		t.Error("nil should translate to nil")
	}
	// A non-driver error is returned unchanged (the DB contract suite covers the
	// real driver-error mappings against live SQLite).
	plain := errorString("not a driver error")
	if d.TranslateError(plain) != error(plain) {
		t.Error("non-driver error should pass through unchanged")
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

// ── schema-administration surface (used by platform/migrate) ────────────────────

func TestSQLiteTableExistsQueryTakesOneParameter(t *testing.T) {
	q := dialect.NewSQLite().TableExistsQuery()
	if n := strings.Count(q, "?"); n != 1 {
		t.Errorf("TableExistsQuery has %d placeholders, want exactly 1: %q", n, q)
	}
	// The contract is "at least one row when present" — the runner must not have to know
	// which catalogue it came from.
	if !strings.Contains(strings.ToUpper(q), "SELECT") {
		t.Errorf("TableExistsQuery is not a SELECT: %q", q)
	}
}

func TestSQLiteIntegrityCheckStatementIsPresent(t *testing.T) {
	// Empty would mean "this engine cannot self-check", which is false for SQLite and
	// would silently disable the migration pre-flight.
	if got := dialect.NewSQLite().IntegrityCheckStatement(); got != "PRAGMA integrity_check" {
		t.Errorf("IntegrityCheckStatement = %q, want PRAGMA integrity_check", got)
	}
}

func TestSQLiteOnlineBackupStatementEscapesDestination(t *testing.T) {
	d := dialect.NewSQLite()

	if got := d.OnlineBackupStatement("/tmp/backup.db"); got != `VACUUM INTO '/tmp/backup.db'` {
		t.Errorf("OnlineBackupStatement = %q", got)
	}
	// A single quote in the path must not be able to terminate the literal. Paths are
	// application-controlled, but this is the injection boundary and it is one line.
	got := d.OnlineBackupStatement("/tmp/o'brien/backup.db")
	if got != `VACUUM INTO '/tmp/o''brien/backup.db'` {
		t.Errorf("single quote not escaped: %q", got)
	}
	if strings.Count(got, "'")%2 != 0 {
		t.Errorf("unbalanced quoting would break the statement: %q", got)
	}
}
