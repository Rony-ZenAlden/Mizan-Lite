package audit

import (
	"context"
	"database/sql"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// CodeReadFailed is the stable code for an audit read failure.
const CodeReadFailed = "audit.read_failed"

// defaultLimit bounds an unfiltered query. The audit log is the one table guaranteed to grow
// forever, so "give me everything" must never be the default.
const defaultLimit = 100

// maxLimit caps what a caller may ask for.
const maxLimit = 1000

// repo reads the audit log.
//
// Note what is absent: any Insert, Update, or Delete. The write path arrives in 1.7; update and
// delete never do. Append-only (§15.1) is enforced by there being no method, which is stronger
// than a convention and visible in one glance at this file.
type repo struct {
	db  database.DB
	clk clock.Clock
}

func newRepo(db database.DB, clk clock.Clock) *repo { return &repo{db: db, clk: clk} }

const entryColumns = `id, occurred_at, actor_user_id, actor_name_snapshot, branch_id,
	session_id, correlation_id, action, entity_type, entity_id, entity_label_snapshot,
	before_json, after_json, changed_fields, source, device_info`

func (r *repo) entries(ctx context.Context, f Filter) ([]Entry, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	var (
		where []string
		args  []any
	)
	if f.EntityType != "" {
		where = append(where, "entity_type = ?")
		args = append(args, f.EntityType)
	}
	if !f.EntityID.IsZero() {
		where = append(where, "entity_id = ?")
		args = append(args, string(f.EntityID))
	}
	if !f.ActorID.IsZero() {
		where = append(where, "actor_user_id = ?")
		args = append(args, string(f.ActorID))
	}

	query := `SELECT ` + entryColumns + ` FROM audit_log`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	// Newest first, with id as the tiebreaker so a page is stable when two entries share an
	// instant — which they will, since one user action writes several (§15.1's correlation).
	query += ` ORDER BY occurred_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, r.wrap(err, "listing audit entries")
	}
	defer func() { _ = rows.Close() }()

	var out []Entry
	for rows.Next() {
		entry, scanErr := scanEntry(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "scanning an audit entry")
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func scanEntry(row interface{ Scan(...any) error }) (Entry, error) {
	var (
		e                                                    Entry
		occurredAt                                           string
		actorID, actorName, branchID, sessionID, correlation sql.NullString
		entityType, entityID, entityLabel                    sql.NullString
		before, after, changed, device                       sql.NullString
	)
	if err := row.Scan(&e.ID, &occurredAt, &actorID, &actorName, &branchID,
		&sessionID, &correlation, &e.Action, &entityType, &entityID, &entityLabel,
		&before, &after, &changed, &e.Source, &device); err != nil {
		return Entry{}, err
	}
	e.OccurredAt, _ = clock.ParseTimestamp(occurredAt)
	e.ActorUserID = id.ID(actorID.String)
	e.ActorName = actorName.String
	e.BranchID = id.ID(branchID.String)
	e.SessionID = id.ID(sessionID.String)
	e.Correlation = id.ID(correlation.String)
	e.EntityType = entityType.String
	e.EntityID = id.ID(entityID.String)
	e.EntityLabel = entityLabel.String
	e.BeforeJSON = before.String
	e.AfterJSON = after.String
	e.ChangedFields = changed.String
	e.DeviceInfo = device.String
	return e, nil
}

func (r *repo) wrap(err error, what string) error {
	return errs.Wrap(r.db.Dialect().TranslateError(err), errs.CategoryInternal,
		CodeReadFailed, what)
}
