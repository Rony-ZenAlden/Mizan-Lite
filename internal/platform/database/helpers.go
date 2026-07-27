package database

import (
	"database/sql"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/database/dialect"
)

// VersionedUpdateResult interprets the result of an optimistic-concurrency UPDATE of
// the form:
//
//	UPDATE <t> SET ..., row_version = row_version + 1 WHERE id = ? AND row_version = ?
//
// A zero-row result means the row was changed or removed by another transaction since
// it was loaded, which is reported as a typed Conflict the use case can retry or
// surface. This lives in the platform so every module gets identical optimistic-lock
// behaviour.
func VersionedUpdateResult(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, dialect.CodeInternal, "reading rows affected")
	}
	if n == 0 {
		return errs.Conflict(dialect.CodeConcurrentModification,
			"row was modified or removed by another transaction")
	}
	return nil
}
