// Package sqlite is the owner's tables.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/owner"
	"github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Store implements owner.Store.
type Store struct{ db database.DB }

// NewStore builds the store.
func NewStore(db database.DB) *Store { return &Store{db: db} }

func (s *Store) Credentials(ctx context.Context) (owner.Credentials, bool, error) {
	var (
		c      owner.Credentials
		locked sql.NullString
	)
	// Read through the WRITER: inside a lockout attempt this runs in the transaction that will write the
	// incremented counter, and the writer is the executor that joins it.
	err := s.db.Writer(ctx).QueryRowContext(ctx, `
		SELECT pin_hash, recovery_hash, failed_attempts, locked_until, row_version
		  FROM owner_credentials WHERE singleton = 1`).
		Scan(&c.PINHash, &c.RecoveryHash, &c.FailedAttempts, &locked, &c.RowVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return owner.Credentials{}, false, nil
	}
	if err != nil {
		return owner.Credentials{}, false, s.db.Dialect().TranslateError(err)
	}
	if locked.Valid {
		if t, ok := clock.ParseTimestamp(locked.String); ok {
			c.LockedUntil = t
		}
	}
	return c, true, nil
}

func lockedValue(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return clock.Format(t)
}

func (s *Store) CreateCredentials(ctx context.Context, c owner.Credentials, at time.Time) error {
	now := clock.Format(at)
	_, err := s.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO owner_credentials (singleton, pin_hash, recovery_hash, failed_attempts, locked_until,
		                               created_at, updated_at, row_version)
		VALUES (1, ?, ?, 0, NULL, ?, ?, 1)`, c.PINHash, c.RecoveryHash, now, now)
	return s.db.Dialect().TranslateError(err)
}

func (s *Store) UpdateCredentials(ctx context.Context, c owner.Credentials, at time.Time) (owner.Credentials, error) {
	res, err := s.db.Writer(ctx).ExecContext(ctx, `
		UPDATE owner_credentials
		   SET pin_hash = ?, recovery_hash = ?, failed_attempts = ?, locked_until = ?,
		       updated_at = ?, row_version = row_version + 1
		 WHERE singleton = 1 AND row_version = ?`,
		c.PINHash, c.RecoveryHash, c.FailedAttempts, lockedValue(c.LockedUntil), clock.Format(at), c.RowVersion)
	if err != nil {
		return owner.Credentials{}, s.db.Dialect().TranslateError(err)
	}
	if err := database.VersionedUpdateResult(res); err != nil {
		return owner.Credentials{}, err
	}
	c.RowVersion++
	return c, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) AppendEvent(ctx context.Context, e owner.Event) error {
	var subject any
	if !e.SubjectID.IsZero() {
		subject = e.SubjectID.String()
	}
	_, err := s.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO owner_events (id, occurred_at, kind, action, subject_id, before_value, after_value)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ID.String(), clock.Format(e.OccurredAt), string(e.Kind), nullable(e.Action), subject,
		nullable(e.Before), nullable(e.After))
	return s.db.Dialect().TranslateError(err)
}

func (s *Store) Events(ctx context.Context, limit int) ([]owner.Event, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, occurred_at, kind, action, subject_id, before_value, after_value
		  FROM owner_events ORDER BY occurred_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []owner.Event
	for rows.Next() {
		var (
			e                                owner.Event
			rawID, occurred, kind            string
			action, subject, beforeV, afterV sql.NullString
		)
		if err = rows.Scan(&rawID, &occurred, &kind, &action, &subject, &beforeV, &afterV); err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		if e.ID, err = id.Parse(rawID); err != nil {
			return nil, err
		}
		e.OccurredAt, _ = clock.ParseTimestamp(occurred)
		e.Kind = domain.EventKind(kind)
		e.Action, e.Before, e.After = action.String, beforeV.String, afterV.String
		if subject.Valid {
			if e.SubjectID, err = id.Parse(subject.String); err != nil {
				return nil, err
			}
		}
		out = append(out, e)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}
