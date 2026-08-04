package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/identity/domain"
)

// ── sessions ────────────────────────────────────────────────────────────────────

const sessionColumns = `id, user_id, branch_id, token_hash, created_at, last_seen_at,
	idle_expires_at, absolute_expires_at, ended_at, end_reason, device_info`

func scanSession(row interface{ Scan(...any) error }) (domain.Session, error) {
	var (
		s                                  domain.Session
		created, lastSeen, idleExp, absExp string
		ended, reason, device              sql.NullString
	)
	if err := row.Scan(&s.ID, &s.UserID, &s.BranchID, &s.TokenHash,
		&created, &lastSeen, &idleExp, &absExp, &ended, &reason, &device); err != nil {
		return domain.Session{}, err
	}
	s.CreatedAt, _ = clock.ParseTimestamp(created)
	s.LastSeen, _ = clock.ParseTimestamp(lastSeen)
	s.IdleExpires, _ = clock.ParseTimestamp(idleExp)
	s.AbsoluteExpires, _ = clock.ParseTimestamp(absExp)
	if ended.Valid {
		s.EndedAt, _ = clock.ParseTimestamp(ended.String)
	}
	s.EndReason, s.DeviceInfo = reason.String, device.String
	return s, nil
}

// InsertSession writes a new session.
func (r *Repos) InsertSession(ctx context.Context, s domain.Session) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO sessions (
			id, user_id, branch_id, token_hash, created_at, last_seen_at,
			idle_expires_at, absolute_expires_at, device_info
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(s.ID), string(s.UserID), string(s.BranchID), s.TokenHash,
		clock.Format(s.CreatedAt), clock.Format(s.LastSeen),
		clock.Format(s.IdleExpires), clock.Format(s.AbsoluteExpires),
		nullable(s.DeviceInfo))
	if err != nil {
		return r.wrap(err, "creating the session")
	}
	return nil
}

// SessionByTokenHash finds a session. Returns sql.ErrNoRows when there is none, so the service
// can treat "no such session" and "expired session" identically.
func (r *Repos) SessionByTokenHash(ctx context.Context, tokenHash string) (domain.Session, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE token_hash = ?`, tokenHash)
	return scanSession(row)
}

// SessionByID finds a session by identifier, for administrative revocation.
func (r *Repos) SessionByID(ctx context.Context, sessionID id.ID) (domain.Session, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, string(sessionID))
	return scanSession(row)
}

// TouchSession rolls the idle window forward.
func (r *Repos) TouchSession(ctx context.Context, sessionID id.ID, now, idleExpires time.Time) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE sessions SET last_seen_at = ?, idle_expires_at = ? WHERE id = ?`,
		clock.Format(now), clock.Format(idleExpires), string(sessionID))
	if err != nil {
		return r.wrap(err, "updating the session")
	}
	return nil
}

// EndSession marks a session finished. Idempotent: ending an already-ended session leaves the
// original reason, because the FIRST reason is the true one.
func (r *Repos) EndSession(ctx context.Context, sessionID id.ID, reason string, now time.Time) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE sessions SET ended_at = ?, end_reason = ?
		 WHERE id = ? AND ended_at IS NULL`,
		clock.Format(now), reason, string(sessionID))
	if err != nil {
		return r.wrap(err, "ending the session")
	}
	return nil
}

// ActiveSessions lists a user's live sessions, for an administrative view.
func (r *Repos) ActiveSessions(ctx context.Context, userID id.ID) ([]domain.Session, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions
		  WHERE user_id = ? AND ended_at IS NULL ORDER BY created_at DESC`,
		string(userID))
	if err != nil {
		return nil, r.wrap(err, "listing sessions")
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Session
	for rows.Next() {
		s, scanErr := scanSession(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a session")
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SweepSessions ends sessions whose windows have passed and deletes long-dead rows.
//
// Housekeeping only: a session is validated against its own timestamps on every use, so a
// missed sweep is never a correctness problem — which is why the job that drives it uses the
// Skip catch-up policy (0.7 §5.2).
func (r *Repos) SweepSessions(ctx context.Context, now time.Time, retain time.Duration) (ended, deleted int, err error) {
	stamp := clock.Format(now)

	res, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE sessions SET ended_at = ?, end_reason = ?
		 WHERE ended_at IS NULL AND (absolute_expires_at <= ? OR idle_expires_at <= ?)`,
		stamp, domain.EndAbsolute, stamp, stamp)
	if err != nil {
		return 0, 0, r.wrap(err, "sweeping sessions")
	}
	if n, rowsErr := res.RowsAffected(); rowsErr == nil {
		ended = int(n)
	}

	cutoff := clock.Format(now.Add(-retain))
	res, err = r.db.Writer(ctx).ExecContext(ctx,
		`DELETE FROM sessions WHERE ended_at IS NOT NULL AND ended_at < ?`, cutoff)
	if err != nil {
		return ended, 0, r.wrap(err, "pruning sessions")
	}
	if n, rowsErr := res.RowsAffected(); rowsErr == nil {
		deleted = int(n)
	}
	return ended, deleted, nil
}

// ── login attempts ──────────────────────────────────────────────────────────────

// Attempt is one recorded login attempt.
type Attempt struct {
	CompanyID  id.ID
	Username   string
	UserID     id.ID // empty when the username does not exist
	Succeeded  bool
	Reason     string
	DeviceInfo string
}

// RecordAttempt appends a login attempt.
//
// Every attempt, including one against a username that does not exist — that is the shape of
// an attack, and discarding it discards the evidence (§13.1).
func (r *Repos) RecordAttempt(ctx context.Context, a Attempt) error {
	identifier, err := id.New()
	if err != nil {
		return err
	}
	var userID any
	if !a.UserID.IsZero() {
		userID = string(a.UserID)
	}
	_, err = r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO login_attempts (
			id, company_id, username, user_id, succeeded, reason, device_info, attempted_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(identifier), string(a.CompanyID), a.Username, userID,
		boolToInt(a.Succeeded), nullable(a.Reason), nullable(a.DeviceInfo), r.now())
	if err != nil {
		return r.wrap(err, "recording the login attempt")
	}
	return nil
}

// ConsecutiveFailures counts failures since the last success for a username, and reports when
// the most recent failure happened.
//
// "Since the last success" is the definition that makes a successful login clear the counter
// without a separate reset write — the history itself carries the state.
func (r *Repos) ConsecutiveFailures(
	ctx context.Context, companyID id.ID, username string,
) (count int, last time.Time, err error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT succeeded, attempted_at FROM login_attempts
		 WHERE company_id = ? AND username = ?
		 ORDER BY attempted_at DESC, id DESC
		 LIMIT 100`, string(companyID), username)
	if err != nil {
		return 0, time.Time{}, r.wrap(err, "reading login attempts")
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			succeeded int
			at        string
		)
		if scanErr := rows.Scan(&succeeded, &at); scanErr != nil {
			return 0, time.Time{}, r.wrap(scanErr, "scanning a login attempt")
		}
		if succeeded == 1 {
			break // a success ends the streak
		}
		if count == 0 {
			last, _ = clock.ParseTimestamp(at)
		}
		count++
	}
	return count, last, rows.Err()
}
