package dialect

import (
	"errors"
	"fmt"
	"strings"

	sqlite "modernc.org/sqlite"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

// SQLite extended result codes (primary code 19 = SQLITE_CONSTRAINT; the extended
// code is primary | (sub << 8)). Named here rather than pulled from the driver's
// internal lib so the mapping is explicit and self-documenting.
const (
	sqliteConstraint           = 19   // SQLITE_CONSTRAINT (primary)
	sqliteConstraintCheck      = 275  // 19 | (1<<8)
	sqliteConstraintForeignKey = 787  // 19 | (3<<8)
	sqliteConstraintNotNull    = 1299 // 19 | (5<<8)
	sqliteConstraintPrimaryKey = 1555 // 19 | (6<<8)
	sqliteConstraintUnique     = 2067 // 19 | (8<<8)
)

type sqliteDialect struct{}

// NewSQLite returns the SQLite dialect.
func NewSQLite() Dialect { return sqliteDialect{} }

var _ Dialect = sqliteDialect{}

func (sqliteDialect) Name() string { return "sqlite" }

// Placeholder ignores n: SQLite uses positional '?'.
func (sqliteDialect) Placeholder(int) string { return "?" }

// Rebind is identity: repositories already write '?' placeholders.
func (sqliteDialect) Rebind(query string) string { return query }

func (sqliteDialect) QuoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (sqliteDialect) LimitOffset(limit, offset int64) string {
	return fmt.Sprintf("LIMIT %d OFFSET %d", limit, offset)
}

func (d sqliteDialect) Upsert(spec UpsertSpec) string {
	cols := make([]string, len(spec.Columns))
	ph := make([]string, len(spec.Columns))
	for i, c := range spec.Columns {
		cols[i] = d.QuoteIdentifier(c)
		ph[i] = "?"
	}
	conflict := make([]string, len(spec.ConflictCols))
	for i, c := range spec.ConflictCols {
		conflict[i] = d.QuoteIdentifier(c)
	}
	sets := make([]string, len(spec.UpdateCols))
	for i, c := range spec.UpdateCols {
		q := d.QuoteIdentifier(c)
		sets[i] = q + " = excluded." + q
	}
	return fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
		d.QuoteIdentifier(spec.Table),
		strings.Join(cols, ", "),
		strings.Join(ph, ", "),
		strings.Join(conflict, ", "),
		strings.Join(sets, ", "),
	)
}

// ForUpdateClause is empty: SQLite's single writer already serialises.
func (sqliteDialect) ForUpdateClause() string { return "" }

func (sqliteDialect) BooleanLiteral(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (sqliteDialect) TranslateError(err error) error {
	if err == nil {
		return nil
	}
	var se *sqlite.Error
	if !errors.As(err, &se) {
		// Not a driver error (sql.ErrNoRows, context cancellation, …) — leave as-is;
		// a missing row is a domain concept the repository translates, not the driver.
		return err
	}
	switch code := se.Code(); {
	case code == sqliteConstraintUnique || code == sqliteConstraintPrimaryKey:
		return errs.Wrap(err, errs.CategoryConflict, CodeDuplicate, "unique constraint violated")
	case code == sqliteConstraintForeignKey:
		return errs.Wrap(err, errs.CategoryConflict, CodeReferenceViolation, "foreign key constraint violated")
	case code == sqliteConstraintNotNull || code == sqliteConstraintCheck:
		return errs.Wrap(err, errs.CategoryValidation, CodeConstraintViolation, "constraint violated")
	case code&0xFF == sqliteConstraint:
		// Any other constraint breach (e.g. a plain UNIQUE index without a subcode).
		return errs.Wrap(err, errs.CategoryConflict, CodeConstraintViolation, "constraint violated")
	default:
		return errs.Wrap(err, errs.CategoryInternal, CodeInternal, "database error")
	}
}
