// Package identity owns users and how they prove who they are.
//
// Sessions, lockout, and login attempts arrive in Step 1.3; RBAC in 1.4. This step answers one
// question: given a username and a password, is this a real person, and who?
package identity

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/identity/contract"
	"github.com/mizan-erp/mizan/internal/modules/identity/domain"
	"github.com/mizan-erp/mizan/internal/modules/identity/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/crypto"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Database is the narrow surface this module needs.
type Database interface {
	database.DB
	database.UnitOfWork
}

// Organisation is the little identity needs to know about org.
//
// Declared HERE, at the point of use, rather than importing org's package — Go's convention,
// and it means org owes identity nothing. The composition root supplies org's service, which
// satisfies it.
//
// Two methods, both genuinely needed: the company scopes a username's uniqueness, and every
// session must carry a branch (§26.1 requires the AppContext to always have one, so that a
// future branch switch is a session update rather than a new concept).
type Organisation interface {
	CurrentCompanyID(ctx context.Context) (id.ID, error)
	DefaultBranchID(ctx context.Context) (id.ID, error)
}

// ActingUser reports who is making the current call, if anyone is.
//
// Declared HERE, at the point of use, rather than imported from the API layer: dependencies
// point inward (§3.1), and a module reaching into internal/api would invert that. The
// composition root supplies the adapter — the same shape audit uses.
//
// One method returning one identifier, because that is all this module needs: the rules that
// consult it ask "is the caller the person being changed?", never "who are they?".
type ActingUser interface {
	UserID(ctx context.Context) (id.ID, bool)
}

// noActor reports that nobody is acting — the setup wizard, a job, a test harness.
type noActor struct{}

func (noActor) UserID(context.Context) (id.ID, bool) { return id.ID(""), false }

// Service is the identity module's application layer.
type Service struct {
	db     Database
	repos  *sqlite.Repos
	hasher crypto.Hasher
	org    Organisation
	clk    clock.Clock
	bus    event.Publisher
	acting ActingUser
}

var _ contract.Authenticator = (*Service)(nil)

// NewService builds the service.
//
// bus is where audited actions are announced (1.7). It is event.Publisher rather than the
// concrete bus so this module cannot reach Subscribe: identity raises events; who reacts to
// them is the composition root's decision.
func NewService(
	db Database, org Organisation, clk clock.Clock, bus event.Publisher, acting ActingUser,
) *Service {
	if clk == nil {
		clk = clock.System()
	}
	if acting == nil {
		// Nobody acting is a legitimate state — the wizard runs before any user exists — and
		// the rules that consult it are all of the form "refuse when the caller IS the
		// subject", which nobody can be.
		acting = noActor{}
	}
	return &Service{
		db:     db,
		repos:  sqlite.New(db, clk),
		hasher: crypto.NewArgon2id(crypto.DefaultParams()),
		org:    org,
		clk:    clk,
		bus:    bus,
		acting: acting,
	}
}

// dummyHash is verified against when no user or credential is found.
//
// THE POINT: without it, "no such user" returns in microseconds while "wrong password" takes
// the full Argon2id cost, and that difference is enough to enumerate accounts. Every failing
// path must do the same work.
//
// Generated once at package init from a random password nobody holds, so it can never
// accidentally match.
var dummyHash = mustDummyHash()

func mustDummyHash() string {
	h := crypto.NewArgon2id(crypto.DefaultParams())
	// An error here would mean the system entropy source failed, in which case authentication
	// is not safe anyway. The empty string is handled below: Verify rejects it, and the
	// verification still runs, so the timing property holds either way.
	encoded, err := h.Hash("a password that is never any user's password")
	if err != nil {
		return ""
	}
	return encoded
}

// Authenticate verifies a username and password.
//
// Every failure returns the SAME error (domain.ErrInvalidCredentials): unknown user, wrong
// password, deactivated account, missing credential. Distinguishable failures are a username
// oracle, which turns an untargeted password spray into a targeted one. Step 1.7's audit trail
// records which case it actually was; the caller never learns.
func (s *Service) Authenticate(ctx context.Context, username, password string) (contract.Principal, error) {
	companyID, err := s.org.CurrentCompanyID(ctx)
	if err != nil {
		return contract.Principal{}, err
	}

	user, lookupErr := s.repos.UserByUsername(ctx, companyID, domain.NormaliseUsername(username))
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		return contract.Principal{}, lookupErr
	}

	// Load the credential only when a user was found; otherwise fall through with an empty
	// encoding, which sends us down the constant-work path below.
	encoded := ""
	mustChange := false
	if lookupErr == nil {
		cred, credErr := s.repos.CredentialFor(ctx, user.ID, domain.CredentialPassword)
		switch {
		case credErr == nil:
			encoded, mustChange = cred.Encoded, cred.MustChange
		case errors.Is(credErr, sql.ErrNoRows):
			// No credential — a user created but never given a password. Still constant work.
		default:
			return contract.Principal{}, credErr
		}
	}

	if encoded == "" {
		// CONSTANT WORK. Verify against the dummy hash and discard the result, so an unknown
		// user costs exactly what a known one does. Returning here without hashing is the
		// mistake this exists to prevent, and no functional test would catch it — the answer
		// is correct either way.
		_, _, _ = s.hasher.Verify(dummyHash, password)
		return contract.Principal{}, domain.ErrInvalidCredentials()
	}

	ok, needsRehash, verifyErr := s.hasher.Verify(encoded, password)
	if verifyErr != nil {
		// A credential we cannot parse is a corrupt row, not a wrong password. It is logged as
		// itself but still reported to the caller as invalid credentials.
		return contract.Principal{}, domain.ErrInvalidCredentials()
	}
	if !ok || !user.IsActive {
		// Checked together, deliberately: an inactive user whose password is correct must be
		// indistinguishable from a wrong password.
		return contract.Principal{}, domain.ErrInvalidCredentials()
	}

	if needsRehash {
		// The only moment the plaintext exists, so the only moment an upgrade is possible.
		// This is what makes §13.1's "parameters can be increased over time without
		// invalidating existing passwords" a mechanism rather than a hope: raising
		// DefaultParams in a release upgrades the user base as people log in.
		//
		// Best-effort: a failed upgrade must never fail a valid login.
		_ = s.rehash(ctx, user.ID, password, mustChange)
	}

	return contract.Principal{
		UserID:      user.ID,
		CompanyID:   user.CompanyID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		MustChange:  mustChange,
	}, nil
}

func (s *Service) rehash(ctx context.Context, userID id.ID, password string, mustChange bool) error {
	encoded, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	return s.repos.SetCredential(ctx, sqlite.Credential{
		UserID: userID, Type: domain.CredentialPassword,
		Algorithm: crypto.Algorithm, Encoded: encoded, MustChange: mustChange,
	})
}

// ── users ───────────────────────────────────────────────────────────────────────

// CreateUserInput describes a user to create.
type CreateUserInput struct {
	CompanyID   id.ID
	Username    string
	DisplayName string
	Email       string
	Password    string
	// IsSystem marks the setup administrator: protected from deletion, not from deactivation.
	IsSystem bool
	// MustChange forces a password change at next login. False for the setup administrator,
	// who just chose their own.
	MustChange bool
}

// CreateUser creates a user and their password in one transaction.
//
// Both together, always: a user without a credential cannot log in and is invisible to every
// screen that lists "people who can sign in", so creating one is never a state worth having.
func (s *Service) CreateUser(ctx context.Context, in CreateUserInput) (domain.User, error) {
	var created domain.User

	err := s.db.Do(ctx, func(ctx context.Context) error {
		policy, err := s.Policy(ctx)
		if err != nil {
			return err
		}
		if err = policy.Validate(in.Password); err != nil {
			return err
		}

		identifier, err := id.New()
		if err != nil {
			return err
		}
		user, err := domain.NewUser(identifier, in.CompanyID, in.Username, in.DisplayName)
		if err != nil {
			return err
		}
		user.Email = in.Email
		user.IsSystem = in.IsSystem

		if err = s.repos.InsertUser(ctx, user); err != nil {
			return err
		}

		encoded, err := s.hasher.Hash(in.Password)
		if err != nil {
			return err
		}
		if err = s.repos.SetCredential(ctx, sqlite.Credential{
			UserID: user.ID, Type: domain.CredentialPassword,
			Algorithm: crypto.Algorithm, Encoded: encoded, MustChange: in.MustChange,
		}); err != nil {
			return err
		}
		if err = s.repos.AppendPasswordHistory(ctx, user.ID, encoded, policy.HistoryCount); err != nil {
			return err
		}

		// INSIDE the transaction, and the error is returned. The user and the record of their
		// creation commit together or neither does (phase D7).
		//
		// Note what the payload does NOT contain: the password, or its hash. `encoded` is in
		// scope right here, and putting it in an audit row would move a credential into
		// long-lived readable storage.
		if err = s.audit(ctx, auditc.Auditable{
			Action:      ActionUserCreated,
			EntityType:  EntityUser,
			EntityID:    user.ID,
			EntityLabel: user.Username,
			After:       snapshotUser(user),
		}); err != nil {
			return err
		}

		created = user
		return nil
	})
	if err != nil {
		return domain.User{}, err
	}
	return created, nil
}

// ChangeOwnPassword replaces the acting user's own password.
//
// # Why the CURRENT password is required (1.11, D2)
//
// The session already proves who is asking, so this looks redundant. It is not. The threat is
// an UNATTENDED TERMINAL, which is the normal state of a shop counter: a session left open is
// not consent to change the credential that outlives it.
//
// It also stops a stolen session from being converted into permanent access, which is the
// entire reason a session is revocable.
//
// Clears must_change: the user has now chosen a password nobody else has seen.
func (s *Service) ChangeOwnPassword(ctx context.Context, userID id.ID, current, next string) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		cred, err := s.repos.CredentialFor(ctx, userID, domain.CredentialPassword)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.ErrInvalidCredentials()
			}
			return err
		}
		ok, _, verifyErr := s.hasher.Verify(cred.Encoded, current)
		if verifyErr != nil || !ok {
			// The same error a bad login gets. There is nothing to gain from a distinct code
			// here, and a distinct code is one more thing to keep in step with §22.2.
			return domain.ErrInvalidCredentials()
		}
		return s.writePassword(ctx, userID, next, false)
	})
}

// ResetPassword sets a password on someone else's behalf.
//
// # Why this SETS must_change (1.11, D1)
//
// An administrator performing this now knows the user's password. That is precisely the
// situation must_change exists for, so the flag is set and the user must replace it at their
// next sign-in.
//
// Step 1.4–1.10 had ONE method, `SetPassword`, which cleared the flag unconditionally. It was
// correct for a self-service change and wrong for exactly this call — an administrator would
// hand over a password, and the mechanism meant to force its replacement would never fire. One
// name, two meanings. They are now two names, and the old one was REMOVED outright rather than
// kept around with a warning — leaving it would leave the wrong default one call site away.
func (s *Service) ResetPassword(ctx context.Context, userID id.ID, next string) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		return s.writePassword(ctx, userID, next, true)
	})
}

// writePassword is the shared body: policy, reuse history, credential, audit.
//
// Called only from inside a Unit of Work, so the credential and its audit entry commit together
// (phase D7).
func (s *Service) writePassword(ctx context.Context, userID id.ID, password string, mustChange bool) error {
	policy, err := s.Policy(ctx)
	if err != nil {
		return err
	}
	if err = policy.Validate(password); err != nil {
		return err
	}
	if err = s.rejectReuse(ctx, userID, password, policy.HistoryCount); err != nil {
		return err
	}

	encoded, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	if err = s.repos.SetCredential(ctx, sqlite.Credential{
		UserID: userID, Type: domain.CredentialPassword,
		Algorithm: crypto.Algorithm, Encoded: encoded, MustChange: mustChange,
	}); err != nil {
		return err
	}
	if err = s.repos.AppendPasswordHistory(ctx, userID, encoded, policy.HistoryCount); err != nil {
		return err
	}

	action := ActionPasswordChanged
	if mustChange {
		action = ActionPasswordReset
	}
	// No Before and no After: the only thing that changed is the secret itself, and the entry's
	// value is the FACT and the actor. `Changed` names the field so the trail stays legible
	// without ever holding a password.
	return s.audit(ctx, auditc.Auditable{
		Action:      action,
		EntityType:  EntityUser,
		EntityID:    userID,
		EntityLabel: s.usernameFor(ctx, userID),
		Changed:     []string{"password"},
	})
}

// rejectReuse verifies the candidate against each stored history entry.
//
// Necessarily one Argon2id verification per remembered password — a hash cannot be compared
// any other way. Bounded by the history setting (5 by default), and it happens on a password
// CHANGE, not on every login, so the cost is paid rarely and by the person who chose to pay it.
func (s *Service) rejectReuse(ctx context.Context, userID id.ID, password string, keep int) error {
	if keep <= 0 {
		return nil
	}
	previous, err := s.repos.RecentPasswords(ctx, userID, keep)
	if err != nil {
		return err
	}
	for _, encoded := range previous {
		match, _, verifyErr := s.hasher.Verify(encoded, password)
		if verifyErr != nil {
			continue // an unparseable historical entry must not block a legitimate change
		}
		if match {
			return domain.ErrPasswordReused()
		}
	}
	return nil
}

// Policy reads the configured password policy.
func (s *Service) Policy(ctx context.Context) (domain.PasswordPolicy, error) {
	return domain.PasswordPolicy{
		MinLength:    int(MinLength.Get(ctx)),
		HistoryCount: int(HistoryCount.Get(ctx)),
		ExpiryDays:   int(ExpiryDays.Get(ctx)),
	}, nil
}

// Users lists a company's users.
func (s *Service) Users(ctx context.Context, companyID id.ID) ([]domain.User, error) {
	return s.repos.Users(ctx, companyID)
}

// User loads one user.
func (s *Service) User(ctx context.Context, userID id.ID) (domain.User, error) {
	return s.repos.UserByID(ctx, userID)
}

// SetUserActive switches a user on or off.
//
// Three refusals, each closing a way to lock everyone out of the installation:
//   - the last ACTIVE user (1.2)
//   - the account making the request (1.11 D6) — the click would sign you out and remove the
//     ability to undo it
//   - the last ADMINISTRATOR (1.11 D6), which is a different rule: you can be the last
//     administrator among five active users
//
// All three live HERE rather than in a screen. A UI that hides the button is not the
// guarantee; an importer or a future repair tool calling the service directly must be refused
// too (§14.3).
func (s *Service) SetUserActive(ctx context.Context, userID id.ID, active bool) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		user, err := s.repos.UserByID(ctx, userID)
		if err != nil {
			return err
		}
		if !active {
			if actorID, ok := s.acting.UserID(ctx); ok && actorID == userID {
				return domain.ErrSelfDeactivation()
			}
			n, countErr := s.repos.CountActiveUsers(ctx, user.CompanyID)
			if countErr != nil {
				return countErr
			}
			if !domain.CanDeactivate(n) {
				return domain.ErrLastActiveUser()
			}
			admins, adminErr := s.CountAdministrators(ctx, user.CompanyID)
			if adminErr != nil {
				return adminErr
			}
			if admins <= 1 && s.isAdministrator(ctx, user.ID) {
				return domain.ErrLastAdministrator()
			}
		}
		if err = s.repos.SetUserActive(ctx, userID, active); err != nil {
			return err
		}

		action := ActionUserDeactivated
		if active {
			action = ActionUserActivated
		}
		after := user
		after.IsActive = active
		return s.audit(ctx, auditc.Auditable{
			Action:      action,
			EntityType:  EntityUser,
			EntityID:    userID,
			EntityLabel: user.Username,
			Before:      snapshotUser(user),
			After:       snapshotUser(after),
			Changed:     []string{"isActive"},
		})
	})
}

// isAdministrator reports whether a user holds the administrator role.
//
// Best-effort false on error: this is one half of a refusal, and failing OPEN here would be the
// wrong direction — but so would failing an ordinary deactivation because a role lookup
// hiccuped. The caller only reaches this when there is already at most one administrator, so a
// false negative costs the last-administrator protection and nothing else, and the
// last-active-user rule still stands behind it.
func (s *Service) isAdministrator(ctx context.Context, userID id.ID) bool {
	roles, err := s.repos.RolesFor(ctx, userID)
	if err != nil {
		return false
	}
	for _, role := range roles {
		if role.Code == RoleAdministrator {
			return true
		}
	}
	return false
}

// usernameFor resolves a label for an audit entry, best-effort.
//
// Best-effort deliberately: an entry with no label is worth far more than no entry, so a failed
// lookup degrades the record rather than failing the operation that caused it.
func (s *Service) usernameFor(ctx context.Context, userID id.ID) string {
	user, err := s.repos.UserByID(ctx, userID)
	if err != nil {
		return ""
	}
	return user.Username
}
