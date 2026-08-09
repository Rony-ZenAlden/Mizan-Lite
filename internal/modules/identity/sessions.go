package identity

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/identity/contract"
	"github.com/mizan-erp/mizan/internal/modules/identity/domain"
	"github.com/mizan-erp/mizan/internal/modules/identity/infra/sqlite"
)

// touchGranularity is how stale last_seen_at may get before it is written (D4).
//
// Session upkeep would otherwise put a write on every binding call, and SQLite has exactly one
// writer (0.3) — so a read-only screen would queue behind session bookkeeping. Idle timeout is
// measured in tens of minutes; recording it to the minute loses nothing.
const touchGranularity = time.Minute

// LoginInput is a sign-in request.
type LoginInput struct {
	Username string
	Password string
	// BranchID the session acts in. Empty means the company's default branch.
	BranchID id.ID
	// Remember selects the longer absolute window ("stay signed in", §13.1).
	Remember   bool
	DeviceInfo string
}

// LoginResult carries the session token — the ONLY time it exists in readable form.
type LoginResult struct {
	Token     string
	Principal contract.Principal
	Session   domain.Session
}

// Login authenticates and issues a session.
//
// Order matters: the THROTTLE is checked before the password is verified. That is what makes
// throttling cheap under attack — the point is to stop doing expensive Argon2 work for someone
// who is guessing, and checking afterwards would mean paying the full cost on every attempt.
func (s *Service) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	companyID, err := s.org.CurrentCompanyID(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	username := domain.NormaliseUsername(in.Username)

	if err = s.checkThrottle(ctx, companyID, username); err != nil {
		// Recorded so a throttled burst is visible in the attempt history as itself, rather
		// than looking like a quiet period.
		s.recordFailure(ctx, companyID, username, id.ID(""), domain.ReasonThrottled, in.DeviceInfo)
		return LoginResult{}, err
	}

	principal, authErr := s.Authenticate(ctx, in.Username, in.Password)
	if authErr != nil {
		if !errs.IsCategory(authErr, errs.CategoryPermission) {
			return LoginResult{}, authErr // an infrastructure failure, not a bad password
		}
		// The attempt history is where the distinction Authenticate refuses to expose is
		// written down. Resolving it costs one read and happens only on failure.
		s.recordFailure(ctx, companyID, username, s.userIDFor(ctx, companyID, username),
			s.failureReason(ctx, companyID, username), in.DeviceInfo)
		return LoginResult{}, authErr
	}

	branchID := in.BranchID
	if branchID.IsZero() {
		branchID, err = s.defaultBranch(ctx)
		if err != nil {
			return LoginResult{}, err
		}
	}

	token, err := domain.NewToken()
	if err != nil {
		return LoginResult{}, err
	}
	sessionID, err := id.New()
	if err != nil {
		return LoginResult{}, err
	}

	session := domain.NewSession(sessionID, principal.UserID, branchID,
		domain.HashToken(token), s.clk.Now(), s.SessionPolicy(ctx), in.Remember, in.DeviceInfo)

	err = s.db.Do(ctx, func(ctx context.Context) error {
		if insertErr := s.repos.InsertSession(ctx, session); insertErr != nil {
			return insertErr
		}
		if attemptErr := s.recordAttempt(ctx, companyID, username, principal.UserID,
			true, domain.ReasonOK, in.DeviceInfo); attemptErr != nil {
			return attemptErr
		}
		// The actor is not on the context yet — the caller is signing IN, so no session has
		// been validated. Named explicitly here, since deriving it would produce an
		// unattributed entry for the one action whose whole point is who performed it.
		return s.audit(ctx, auditc.Auditable{
			Action:      ActionLoginSucceeded,
			EntityType:  EntitySession,
			EntityID:    session.ID,
			EntityLabel: principal.Username,
			After: loginSnapshot{
				UserID: principal.UserID, Username: principal.Username,
				BranchID: branchID, Remember: in.Remember, DeviceInfo: in.DeviceInfo,
			},
		})
	})
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{Token: token, Principal: principal, Session: session}, nil
}

// Validate resolves a session token to its principal.
//
// There is deliberately NO in-memory session cache: an administrator's "sign this device out"
// must take effect on the very next call, and a cache would make revocation a lie for as long
// as the entry lived. The lookup is one indexed read.
func (s *Service) Validate(ctx context.Context, token string) (contract.Principal, domain.Session, error) {
	if token == "" {
		return contract.Principal{}, domain.Session{}, domain.ErrSessionInvalid()
	}

	session, err := s.repos.SessionByTokenHash(ctx, domain.HashToken(token))
	if errors.Is(err, sql.ErrNoRows) {
		// Indistinguishable from an expired session, on purpose: telling the holder of a
		// stolen token that it was "expired" rather than "unknown" confirms it was once real.
		return contract.Principal{}, domain.Session{}, domain.ErrSessionInvalid()
	}
	if err != nil {
		return contract.Principal{}, domain.Session{}, err
	}

	now := s.clk.Now()
	if reason := session.ExpiryReason(now); reason != "" {
		// Record why, so an administrator can see it, then refuse.
		if session.EndReason == "" {
			_ = s.repos.EndSession(ctx, session.ID, reason, now)
		}
		return contract.Principal{}, domain.Session{}, domain.ErrSessionInvalid()
	}

	user, err := s.repos.UserByID(ctx, session.UserID)
	if err != nil {
		return contract.Principal{}, domain.Session{}, domain.ErrSessionInvalid()
	}
	if !user.IsActive {
		// Deactivating a user must end their session immediately, not at the next expiry.
		_ = s.repos.EndSession(ctx, session.ID, domain.EndRevoked, now)
		return contract.Principal{}, domain.Session{}, domain.ErrSessionInvalid()
	}

	if session.NeedsTouch(now, touchGranularity) {
		policy := s.SessionPolicy(ctx)
		idleExpires := now.Add(policy.Idle)
		if err = s.repos.TouchSession(ctx, session.ID, now, idleExpires); err != nil {
			return contract.Principal{}, domain.Session{}, err
		}
		session.LastSeen, session.IdleExpires = now, idleExpires
	}

	return contract.Principal{
		UserID:      user.ID,
		CompanyID:   user.CompanyID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
	}, session, nil
}

// Logout ends the caller's own session.
func (s *Service) Logout(ctx context.Context, token string) error {
	session, err := s.repos.SessionByTokenHash(ctx, domain.HashToken(token))
	if errors.Is(err, sql.ErrNoRows) {
		return nil // logging out of a session that is already gone is a success
	}
	if err != nil {
		return err
	}
	return s.db.Do(ctx, func(ctx context.Context) error {
		if endErr := s.repos.EndSession(ctx, session.ID, domain.EndLogout, s.clk.Now()); endErr != nil {
			return endErr
		}
		return s.audit(ctx, auditc.Auditable{
			Action:     ActionLogout,
			EntityType: EntitySession,
			EntityID:   session.ID,
		})
	})
}

// Revoke ends someone else's session. Permission-gated in Step 1.5.
func (s *Service) Revoke(ctx context.Context, sessionID id.ID) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		if err := s.repos.EndSession(ctx, sessionID, domain.EndRevoked, s.clk.Now()); err != nil {
			return err
		}
		return s.audit(ctx, auditc.Auditable{
			Action:     ActionSessionRevoked,
			EntityType: EntitySession,
			EntityID:   sessionID,
		})
	})
}

// ActiveSessions lists a user's live sessions.
func (s *Service) ActiveSessions(ctx context.Context, userID id.ID) ([]domain.Session, error) {
	return s.repos.ActiveSessions(ctx, userID)
}

// SweepSessions is the body of the session_sweep job.
func (s *Service) SweepSessions(ctx context.Context) (ended, deleted int, err error) {
	const retain = 90 * 24 * time.Hour
	return s.repos.SweepSessions(ctx, s.clk.Now(), retain)
}

// ── policy ──────────────────────────────────────────────────────────────────────

// SessionPolicy reads the configured session lifetimes.
func (s *Service) SessionPolicy(ctx context.Context) domain.SessionPolicy {
	return domain.SessionPolicy{
		Idle:        time.Duration(IdleMinutes.Get(ctx)) * time.Minute,
		Absolute:    time.Duration(AbsoluteHours.Get(ctx)) * time.Hour,
		RememberFor: time.Duration(RememberDays.Get(ctx)) * 24 * time.Hour,
	}
}

// ThrottlePolicy reads the configured lockout behaviour.
func (s *Service) ThrottlePolicy(ctx context.Context) domain.ThrottlePolicy {
	return domain.ThrottlePolicy{
		Threshold: int(LockoutThreshold.Get(ctx)),
		Base:      time.Duration(LockoutBaseSeconds.Get(ctx)) * time.Second,
		Max:       time.Duration(LockoutMaxSeconds.Get(ctx)) * time.Second,
	}
}

// checkThrottle refuses a login that is still inside its backoff window.
func (s *Service) checkThrottle(ctx context.Context, companyID id.ID, username string) error {
	policy := s.ThrottlePolicy(ctx)
	if policy.Threshold <= 0 {
		return nil
	}
	failures, last, err := s.repos.ConsecutiveFailures(ctx, companyID, username)
	if err != nil {
		return err
	}
	delay := policy.Delay(failures)
	if delay == 0 {
		return nil
	}
	if retryAt := last.Add(delay); s.clk.Now().Before(retryAt) {
		return domain.ErrTooManyAttempts(retryAt.Sub(s.clk.Now()))
	}
	return nil
}

// ── helpers ─────────────────────────────────────────────────────────────────────

func (s *Service) recordAttempt(
	ctx context.Context, companyID id.ID, username string, userID id.ID,
	ok bool, reason, device string,
) error {
	return s.repos.RecordAttempt(ctx, sqlite.Attempt{
		CompanyID: companyID, Username: username, UserID: userID,
		Succeeded: ok, Reason: reason, DeviceInfo: device,
	})
}

// userIDFor resolves a username to an id for the attempt record, or empty if unknown.
func (s *Service) userIDFor(ctx context.Context, companyID id.ID, username string) id.ID {
	user, err := s.repos.UserByUsername(ctx, companyID, username)
	if err != nil {
		return ""
	}
	return user.ID
}

// failureReason classifies a failure FOR THE RECORD ONLY.
//
// The caller is never told this — Authenticate returns one error for every case (1.2). The
// audit trail knows; the person at the keyboard does not.
func (s *Service) failureReason(ctx context.Context, companyID id.ID, username string) string {
	user, err := s.repos.UserByUsername(ctx, companyID, username)
	if err != nil {
		return domain.ReasonUnknownUser
	}
	if !user.IsActive {
		return domain.ReasonInactive
	}
	return domain.ReasonBadPassword
}

// defaultBranch resolves the branch a session acts in when none was named.
func (s *Service) defaultBranch(ctx context.Context) (id.ID, error) {
	return s.org.DefaultBranchID(ctx)
}

// loginSnapshot is what a sign-in looks like in an audit payload. The token is absent, and
// deliberately so: it is a live credential, and an audit row is long-lived readable storage.
type loginSnapshot struct {
	UserID     id.ID  `json:"userId,omitempty"`
	Username   string `json:"username"`
	BranchID   id.ID  `json:"branchId,omitempty"`
	Remember   bool   `json:"remember,omitempty"`
	DeviceInfo string `json:"deviceInfo,omitempty"`
}

// recordFailure writes the attempt row and its audit entry, together.
//
// A failed login has no business change to abort, so "atomic with the change" would be vacuous
// — but the attempt row IS a write, and the same guarantee applies to it: the attempt and its
// audit entry commit together (1.7 §4.4).
//
// Errors are swallowed HERE and only here, which needs justifying since the rest of the module
// treats a failed audit write as fatal. The alternative is worse in a specific way: a database
// hiccup while recording a bad password would turn "wrong password" into "internal error", and
// the honest answer to the caller is still that their credentials were wrong. The failure is
// also self-limiting — the login already failed, so nothing incorrect can be committed on the
// strength of a missing record.
func (s *Service) recordFailure(
	ctx context.Context, companyID id.ID, username string, userID id.ID, reason, device string,
) {
	_ = s.db.Do(ctx, func(ctx context.Context) error {
		if err := s.recordAttempt(ctx, companyID, username, userID, false, reason, device); err != nil {
			return err
		}
		return s.audit(ctx, auditc.Auditable{
			Action:      ActionLoginFailed,
			EntityType:  EntitySession,
			EntityLabel: username,
			// The reason IS recorded, unlike the error returned to the caller, which is
			// identical for every failure so as not to be a username oracle (service.go). An
			// administrator reading the trail is entitled to the distinction; whoever is at
			// the login screen is not.
			After: failureSnapshot{Username: username, UserID: userID, Reason: reason, DeviceInfo: device},
		})
	})
}

type failureSnapshot struct {
	Username   string `json:"username"`
	UserID     id.ID  `json:"userId,omitempty"`
	Reason     string `json:"reason"`
	DeviceInfo string `json:"deviceInfo,omitempty"`
}

// ── the sessions screen (1.11) ──────────────────────────────────────────────────

// SessionView is a live session with the person it belongs to.
//
// A view rather than domain.Session (1.11 D4): the screen shows WHO beside the device, and
// domain.Session carries only a user id — the screen would otherwise resolve names itself, one
// query per row.
type SessionView struct {
	ID          id.ID
	UserID      id.ID
	Username    string
	DisplayName string
	BranchID    id.ID
	StartedAt   time.Time
	LastSeen    time.Time
	DeviceInfo  string
	// ExpiresAt is the ABSOLUTE expiry, not the idle one: the idle window rolls forward on
	// every call, so showing it would tell an administrator only that the session is in use.
	ExpiresAt time.Time
}

// AllActiveSessions lists every live session in the company.
//
// Ordered newest-first. The administrator reading this is usually answering "who is signed in
// right now?", and the most recent arrival is the most likely subject of that question.
func (s *Service) AllActiveSessions(ctx context.Context, companyID id.ID) ([]SessionView, error) {
	users, err := s.repos.Users(ctx, companyID)
	if err != nil {
		return nil, err
	}

	var out []SessionView
	for _, user := range users {
		sessions, sessionErr := s.repos.ActiveSessions(ctx, user.ID)
		if sessionErr != nil {
			return nil, sessionErr
		}
		for _, session := range sessions {
			out = append(out, SessionView{
				ID: session.ID, UserID: user.ID,
				Username: user.Username, DisplayName: user.DisplayName,
				BranchID: session.BranchID, StartedAt: session.CreatedAt,
				LastSeen: session.LastSeen, DeviceInfo: session.DeviceInfo,
				ExpiresAt: session.AbsoluteExpires,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}
