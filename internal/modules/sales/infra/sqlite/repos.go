// Package sqlite is the sales module's persistence.
package sqlite

import (
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeStorage is the stable code for a persistence failure.
const CodeStorage = "sales.storage"

// Repos is the module's repository set.
//
// It lived in numbering.go until Phase 6 moved the allocator to `internal/platform/numbering` —
// purchasing needed one too, and could neither import sales nor safely run a second against the
// same table. This type stayed; only the series went.
type Repos struct {
	db  database.DB
	clk clock.Clock
}

// New builds the repositories.
func New(db database.DB, clk clock.Clock) *Repos {
	if clk == nil {
		clk = clock.System()
	}
	return &Repos{db: db, clk: clk}
}

func (r *Repos) now() string { return clock.Format(r.clk.Now()) }

func (r *Repos) wrap(err error, what string) error {
	return errs.Wrap(r.db.Dialect().TranslateError(err), errs.CategoryInternal, CodeStorage, what)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
