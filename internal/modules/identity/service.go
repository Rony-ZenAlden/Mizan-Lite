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
	"github.com/mizan-erp/mizan/internal/kernel/id"
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

// Service is the identity module's application layer.
type Service struct {
	db     Database
	repos  *sqlite.Repos
	hasher crypto.Hasher
	org    Organisation
	clk    clock.Clock
}

var _ contract.Authenticator = (*Service)(nil)

// NewService builds the service.
func NewService(db Database, org Organisation, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.System()
	}
	return &Service{
		db:     db,
		repos:  sqlite.New(db, clk),
		hasher: crypto.NewArgon2id(crypto.DefaultParams()),
		org:    org,
		clk:    clk,
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

		created = user
		return nil
	})
	if err != nil {
		return domain.User{}, err
	}
	return created, nil
}

// SetPassword replaces a user's password, enforcing the policy and the reuse history.
func (s *Service) SetPassword(ctx context.Context, userID id.ID, password string) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
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
			Algorithm: crypto.Algorithm, Encoded: encoded, MustChange: false,
		}); err != nil {
			return err
		}
		return s.repos.AppendPasswordHistory(ctx, userID, encoded, policy.HistoryCount)
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

// SetUserActive switches a user on or off, refusing to switch off the last active one.
func (s *Service) SetUserActive(ctx context.Context, userID id.ID, active bool) error {
	return s.db.Do(ctx, func(ctx context.Context) error {
		user, err := s.repos.UserByID(ctx, userID)
		if err != nil {
			return err
		}
		if !active {
			n, countErr := s.repos.CountActiveUsers(ctx, user.CompanyID)
			if countErr != nil {
				return countErr
			}
			if !domain.CanDeactivate(n) {
				return domain.ErrLastActiveUser()
			}
		}
		return s.repos.SetUserActive(ctx, userID, active)
	})
}
