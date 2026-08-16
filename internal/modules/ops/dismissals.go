// Package ops owns the operational surface a person looks after: notice dismissals, and the rules
// that decide what to tell them.
package ops

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"io/fs"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/notify"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrations returns this package's schema.
//
// Falling back to the un-subbed FS rather than panicking, exactly as every module does: an
// embedded directory that cannot be sub-ed is a build-time impossibility, and a panic in
// production code is forbidden for the good reason that it takes a shop's application down over
// something no operator can act on.
func Migrations() fs.FS {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return migrations
	}
	return sub
}

// CodeDismissalFailed is this store's only failure.
const CodeDismissalFailed = "ops.dismissal_failed"

// Dismissals stores what a user has chosen not to be told again.
type Dismissals struct {
	db  database.DB
	clk clock.Clock
}

var _ notify.Dismissals = (*Dismissals)(nil)

// NewDismissals builds the store.
func NewDismissals(db database.DB, clk clock.Clock) *Dismissals {
	if clk == nil {
		clk = clock.System()
	}
	return &Dismissals{db: db, clk: clk}
}

// Dismissed returns the keys this user has dismissed.
func (d *Dismissals) Dismissed(
	ctx context.Context, companyID, userID id.ID,
) (map[string]bool, error) {
	rows, err := d.db.Reader(ctx).QueryContext(ctx,
		`SELECT notice_key FROM notice_dismissals WHERE company_id = ? AND user_id = ?`,
		string(companyID), string(userID))
	if err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeDismissalFailed,
			"reading dismissed notices")
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]bool, 16)
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return nil, errs.Wrap(err, errs.CategoryInternal, CodeDismissalFailed,
				"reading dismissed notices")
		}
		out[key] = true
	}
	if err = rows.Err(); err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeDismissalFailed,
			"reading dismissed notices")
	}
	return out, nil
}

// Dismiss records one, idempotently.
//
// Dismissing twice is not an error: two windows open on the same screen is ordinary, and a
// conflict returned to the second would be an error message about nothing.
func (d *Dismissals) Dismiss(
	ctx context.Context, companyID, userID id.ID, key string,
) error {
	identifier, err := id.New()
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeDismissalFailed,
			"minting an identity")
	}
	// The rule name is the key's first segment, stored so a screen can group without parsing.
	rule := key
	for i := 0; i < len(key); i++ {
		if key[i] == '.' {
			rule = key[:i]
			break
		}
	}

	_, err = d.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO notice_dismissals
		    (id, company_id, user_id, notice_key, rule_name, dismissed_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (company_id, user_id, notice_key) DO NOTHING`,
		string(identifier), string(companyID), string(userID), key, rule,
		clock.Format(d.clk.Now()))
	if err != nil {
		return errs.Wrap(err, errs.CategoryInternal, CodeDismissalFailed,
			"dismissing a notice")
	}
	return nil
}

// Restore forgets a dismissal, so the notice returns if its condition still holds.
//
// # Why this exists at all
//
// A dismissal is permanent otherwise, and a user who silenced something by accident has no way
// back — they would have to be told to edit a database. Restoring does not GUARANTEE the notice
// comes back: if the condition resolved meanwhile, nothing returns, which is correct.
func (d *Dismissals) Restore(
	ctx context.Context, companyID, userID id.ID, key string,
) error {
	_, err := d.db.Writer(ctx).ExecContext(ctx,
		`DELETE FROM notice_dismissals
		  WHERE company_id = ? AND user_id = ? AND notice_key = ?`,
		string(companyID), string(userID), key)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errs.Wrap(err, errs.CategoryInternal, CodeDismissalFailed,
			"restoring a notice")
	}
	return nil
}
