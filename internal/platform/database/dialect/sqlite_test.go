package dialect_test

import (
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
