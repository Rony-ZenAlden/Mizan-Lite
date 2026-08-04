// Package sqlite implements the identity module's repositories.
//
// The only place in the module that knows SQL, enforced by the `no-sql` rule from Step 1.1.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/identity/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Repos bundles the identity module's repositories over one database.
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
	return errs.Wrap(r.db.Dialect().TranslateError(err), errs.CategoryInternal,
		domain.CodeInvalidUser, what)
}

// ── users ───────────────────────────────────────────────────────────────────────

// InsertUser writes a user. It writes NO credential — see SetCredential.
func (r *Repos) InsertUser(ctx context.Context, u domain.User) error {
	now := r.now()
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO users (
			id, company_id, username, display_name, email, locale,
			is_system, is_active, created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		string(u.ID), string(u.CompanyID), u.Username, u.DisplayName,
		nullable(u.Email), nullable(u.Locale),
		boolToInt(u.IsSystem), boolToInt(u.IsActive), now, now)
	if err != nil {
		return r.wrap(err, "inserting the user")
	}
	return nil
}

// userColumns is the ONLY projection of a user.
//
// It names every column explicitly and includes none from user_credentials. A SELECT * here
// would still be safe today, but it would stop being safe the moment someone adds a column,
// and the point of splitting the tables is to make the safe thing structural.
const userColumns = `id, company_id, username, display_name, email, locale, is_system, is_active`

func scanUser(row interface{ Scan(...any) error }) (domain.User, error) {
	var (
		u             domain.User
		email, locale sql.NullString
		system, live  int
	)
	if err := row.Scan(&u.ID, &u.CompanyID, &u.Username, &u.DisplayName,
		&email, &locale, &system, &live); err != nil {
		return domain.User{}, err
	}
	u.Email, u.Locale = email.String, locale.String
	u.IsSystem, u.IsActive = system == 1, live == 1
	return u, nil
}

// UserByUsername finds a user by their (lower-cased) username.
//
// Returns sql.ErrNoRows unwrapped so the service can decide what an absent user means. It must
// NOT be turned into a typed NotFound here: the service deliberately treats "no such user" and
// "wrong password" identically, and a distinct error at this layer invites a caller to leak
// the difference.
func (r *Repos) UserByUsername(ctx context.Context, companyID id.ID, username string) (domain.User, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE company_id = ? AND username = ?`,
		string(companyID), username)
	return scanUser(row)
}

// UserByID finds a user.
func (r *Repos) UserByID(ctx context.Context, userID id.ID) (domain.User, error) {
	row := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = ?`, string(userID))
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, errs.NotFound(domain.CodeUserNotFound, "no such user")
	}
	if err != nil {
		return domain.User{}, r.wrap(err, "reading the user")
	}
	return u, nil
}

// Users lists a company's users, by username.
func (r *Repos) Users(ctx context.Context, companyID id.ID) ([]domain.User, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE company_id = ? ORDER BY username`,
		string(companyID))
	if err != nil {
		return nil, r.wrap(err, "listing users")
	}
	defer func() { _ = rows.Close() }()

	var out []domain.User
	for rows.Next() {
		u, scanErr := scanUser(rows)
		if scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a user")
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountActiveUsers supports the last-active-user guard.
func (r *Repos) CountActiveUsers(ctx context.Context, companyID id.ID) (int, error) {
	var n int
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE company_id = ? AND is_active = 1`,
		string(companyID)).Scan(&n)
	if err != nil {
		return 0, r.wrap(err, "counting active users")
	}
	return n, nil
}

// SetUserActive switches a user on or off.
func (r *Repos) SetUserActive(ctx context.Context, userID id.ID, active bool) error {
	_, err := r.db.Writer(ctx).ExecContext(ctx, `
		UPDATE users SET is_active = ?, updated_at = ?, row_version = row_version + 1
		 WHERE id = ?`, boolToInt(active), r.now(), string(userID))
	if err != nil {
		return r.wrap(err, "updating the user")
	}
	return nil
}

// ── credentials ─────────────────────────────────────────────────────────────────

// Credential is a stored credential. It never leaves the module.
type Credential struct {
	UserID     id.ID
	Type       string
	Algorithm  string
	Encoded    string
	MustChange bool
}

// CredentialFor loads a user's credential of the given type.
func (r *Repos) CredentialFor(ctx context.Context, userID id.ID, kind string) (Credential, error) {
	var (
		c      Credential
		change int
	)
	err := r.db.Reader(ctx).QueryRowContext(ctx, `
		SELECT user_id, credential_type, algorithm, encoded, must_change
		  FROM user_credentials WHERE user_id = ? AND credential_type = ?`,
		string(userID), kind).
		Scan(&c.UserID, &c.Type, &c.Algorithm, &c.Encoded, &change)
	if err != nil {
		return Credential{}, err // sql.ErrNoRows passes through; see UserByUsername
	}
	c.MustChange = change == 1
	return c, nil
}

// SetCredential inserts or replaces a user's credential of the given type.
func (r *Repos) SetCredential(ctx context.Context, c Credential) error {
	now := r.now()
	identifier, err := id.New()
	if err != nil {
		return err
	}
	// Delete-then-insert rather than the dialect upsert: the unique key is
	// (user_id, credential_type), and replacing a credential should reset every column
	// including row_version. An upsert that preserved the old row's history would be a
	// credential whose age no longer means anything.
	if _, err = r.db.Writer(ctx).ExecContext(ctx,
		`DELETE FROM user_credentials WHERE user_id = ? AND credential_type = ?`,
		string(c.UserID), c.Type); err != nil {
		return r.wrap(err, "replacing the credential")
	}
	_, err = r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO user_credentials (
			id, user_id, credential_type, algorithm, encoded, must_change,
			created_at, updated_at, row_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		string(identifier), string(c.UserID), c.Type, c.Algorithm, c.Encoded,
		boolToInt(c.MustChange), now, now)
	if err != nil {
		return r.wrap(err, "inserting the credential")
	}
	return nil
}

// ── password history ────────────────────────────────────────────────────────────

// RecentPasswords returns a user's most recent encoded passwords, newest first.
func (r *Repos) RecentPasswords(ctx context.Context, userID id.ID, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT encoded FROM password_history
		 WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		string(userID), limit)
	if err != nil {
		return nil, r.wrap(err, "reading password history")
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var encoded string
		if scanErr := rows.Scan(&encoded); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning password history")
		}
		out = append(out, encoded)
	}
	return out, rows.Err()
}

// AppendPasswordHistory records a password and prunes older entries beyond keep.
//
// Pruning here rather than in a job because the bound is per user and tiny: an unbounded
// history would accumulate a hash per password change forever, for no benefit past the
// configured window.
func (r *Repos) AppendPasswordHistory(ctx context.Context, userID id.ID, encoded string, keep int) error {
	identifier, err := id.New()
	if err != nil {
		return err
	}
	if _, err = r.db.Writer(ctx).ExecContext(ctx,
		`INSERT INTO password_history (id, user_id, encoded, created_at) VALUES (?, ?, ?, ?)`,
		string(identifier), string(userID), encoded, r.now()); err != nil {
		return r.wrap(err, "recording password history")
	}
	if keep <= 0 {
		keep = 0
	}
	// Keep the newest `keep` rows. Expressed as "delete the ids not in the newest set" because
	// OFFSET in a DELETE is not portable (§8.2).
	_, err = r.db.Writer(ctx).ExecContext(ctx, `
		DELETE FROM password_history
		 WHERE user_id = ?
		   AND id NOT IN (
		       SELECT id FROM password_history
		        WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?
		   )`, string(userID), string(userID), keep)
	if err != nil {
		return r.wrap(err, "pruning password history")
	}
	return nil
}

// ── helpers ─────────────────────────────────────────────────────────────────────

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
