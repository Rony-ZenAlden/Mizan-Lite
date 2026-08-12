package identity

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/identity/contract"
	"github.com/mizan-erp/mizan/internal/modules/identity/domain"
	"github.com/mizan-erp/mizan/internal/modules/identity/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/platform/auth"
)

// The audited actions and permission this step adds.
const (
	ActionPINSet            = "identity.pin.set"
	ActionPINCleared        = "identity.pin.cleared"
	ActionPINLoginSucceeded = "identity.pin_login.succeeded"
	ActionPINLoginFailed    = "identity.pin_login.failed"

	// PermPINManage gates setting somebody ELSE's PIN. Setting your own needs nothing, for the
	// reason 1.11 gave about passwords: the person who most needs it may hold almost nothing.
	PermPINManage = "identity.pin.manage"
)

// SetOwnPIN gives the signed-in user a till PIN.
//
// No permission required. A cashier setting the PIN they will type forty times a shift is not an
// administrative act, and requiring a grant would mean the people who need it most are the ones
// who cannot do it — the same reasoning 1.11 applied to changing your own password.
func (s *Service) SetOwnPIN(ctx context.Context, pin string) error {
	actor, ok := appctx.ActorFrom(ctx)
	if !ok || actor.UserID.IsZero() {
		return domain.ErrSessionInvalid()
	}
	return s.setPIN(ctx, actor.UserID, pin, ActionPINSet)
}

// SetPINFor gives another user a till PIN.
//
// A manager setting a new cashier's first PIN. Gated, because setting somebody else's credential
// is an administrative act — and because a PIN set by somebody else is one they should change.
func (s *Service) SetPINFor(ctx context.Context, userID id.ID, pin string) error {
	if !s.Can(ctx, PermPINManage, auth.Global()) {
		return errs.Permission(domain.CodePINScope, "you may not set another user's PIN")
	}
	return s.setPIN(ctx, userID, pin, ActionPINSet)
}

func (s *Service) setPIN(ctx context.Context, userID id.ID, pin, action string) error {
	if err := domain.ValidatePIN(pin); err != nil {
		return err
	}
	// Hashed with the same parameters a password gets. A PIN's small search space is not an
	// argument for hashing it more cheaply — it is an argument for the scoping and throttling
	// that surround it, and a fast hash would simply add a second weakness to the first.
	encoded, err := s.hasher.Hash(pin)
	if err != nil {
		return err
	}

	return s.db.Do(ctx, func(txCtx context.Context) error {
		if err = s.repos.SetCredential(txCtx, sqlite.Credential{
			UserID: userID, Type: domain.CredentialPIN, Encoded: encoded,
		}); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: action, EntityType: EntityUser, EntityID: userID,
			// The PIN itself is never in the payload. Not even hashed: an audit trail is read by
			// more people than a credential store, and a hash in it is a hash somebody can take
			// away and attack offline.
			After: map[string]any{"credential": domain.CredentialPIN},
		})
	})
}

// ClearPIN removes a user's till PIN.
func (s *Service) ClearPIN(ctx context.Context, userID id.ID) error {
	if !s.Can(ctx, PermPINManage, auth.Global()) {
		return errs.Permission(domain.CodePINScope, "you may not clear another user's PIN")
	}
	return s.db.Do(ctx, func(txCtx context.Context) error {
		if err := s.repos.DeleteCredential(txCtx, userID, domain.CredentialPIN); err != nil {
			return err
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionPINCleared, EntityType: EntityUser, EntityID: userID,
		})
	})
}

// PINLoginInput switches the person at a till.
type PINLoginInput struct {
	// DeviceToken is the session that already unlocked this terminal. REQUIRED.
	//
	// This is what makes a four-digit credential defensible: a PIN is never a way in from
	// nothing. Somebody signed the terminal in with a real password, and a PIN changes who is
	// standing at it.
	DeviceToken string
	Username    string
	PIN         string
}

// PINLogin issues a till session for a different user, on an already-unlocked terminal.
//
// # Three protections, and it needs all three
//
//  1. The device must already hold a valid full session (`DeviceToken`).
//  2. The session issued is SCOPED to point-of-sale permissions — see `Can`.
//  3. It is throttled per user, on the same mechanism a password login uses, checked BEFORE the
//     hash so that guessing costs the attacker time rather than costing us Argon2 work.
func (s *Service) PINLogin(ctx context.Context, in PINLoginInput) (LoginResult, error) {
	// The device first. A PIN attempt with no unlocked terminal is refused before anything else
	// happens — including before the throttle, because there is nothing to throttle: this is not
	// a guess at a PIN, it is a request that cannot be made at all.
	device, deviceSession, err := s.Validate(ctx, in.DeviceToken)
	if err != nil {
		return LoginResult{}, errs.Permission(domain.CodeDeviceRequired,
			"a till PIN can only be used on a terminal that is already signed in")
	}
	if deviceSession.AuthMethod == domain.AuthPIN {
		// A till session cannot authorise another one. Otherwise one guessed PIN would let an
		// attacker walk the whole staff list, each hop looking as legitimate as the last.
		return LoginResult{}, errs.Permission(domain.CodeDeviceRequired,
			"a till session cannot sign in another user")
	}

	companyID, err := s.org.CurrentCompanyID(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	username := domain.NormaliseUsername(in.Username)

	if err = s.checkThrottle(ctx, companyID, username); err != nil {
		s.recordFailure(ctx, companyID, username, id.ID(""), domain.ReasonThrottled, "pin")
		return LoginResult{}, err
	}

	principal, err := s.authenticatePIN(ctx, companyID, username, in.PIN)
	if err != nil {
		if errs.IsCategory(err, errs.CategoryPermission) {
			s.recordFailure(ctx, companyID, username,
				s.userIDFor(ctx, companyID, username), domain.ReasonBadPassword, "pin")
		}
		return LoginResult{}, err
	}

	token, err := domain.NewToken()
	if err != nil {
		return LoginResult{}, err
	}
	sessionID, err := id.New()
	if err != nil {
		return LoginResult{}, err
	}

	// The till session inherits the DEVICE's branch, not the user's default. A cashier signing in
	// at the second shop's till is working at the second shop, whatever their record says.
	session := domain.NewSession(sessionID, principal.UserID, deviceSession.BranchID,
		domain.HashToken(token), s.clk.Now(), s.SessionPolicy(ctx), false, "pin")
	session.AuthMethod = domain.AuthPIN
	session.DeviceSessionID = deviceSession.ID

	err = s.db.Do(ctx, func(txCtx context.Context) error {
		if insertErr := s.repos.InsertSession(txCtx, session); insertErr != nil {
			return insertErr
		}
		if attemptErr := s.recordAttempt(txCtx, companyID, username, principal.UserID,
			true, domain.ReasonOK, "pin"); attemptErr != nil {
			return attemptErr
		}
		return s.audit(txCtx, auditc.Auditable{
			Action: ActionPINLoginSucceeded, EntityType: EntitySession, EntityID: session.ID,
			EntityLabel: principal.Username,
			After: map[string]any{
				"user_id": string(principal.UserID), "username": principal.Username,
				"device_session_id": string(deviceSession.ID),
				"device_user":       device.Username,
				"branch_id":         string(deviceSession.BranchID),
			},
		})
	})
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{Token: token, Principal: principal, Session: session}, nil
}

// authenticatePIN verifies a PIN, in constant work.
//
// The same shape Authenticate uses, and for the same reason: an unknown username must cost
// exactly what a known one does, or the timing difference is a way to enumerate the staff list.
func (s *Service) authenticatePIN(
	ctx context.Context, companyID id.ID, username, pin string,
) (contract.Principal, error) {
	user, lookupErr := s.repos.UserByUsername(ctx, companyID, username)
	if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
		return contract.Principal{}, lookupErr
	}

	encoded := ""
	if lookupErr == nil {
		cred, credErr := s.repos.CredentialFor(ctx, user.ID, domain.CredentialPIN)
		switch {
		case credErr == nil:
			encoded = cred.Encoded
		case errors.Is(credErr, sql.ErrNoRows):
			// A user with no PIN. Falls through to the constant-work path: "this person has no
			// PIN" and "that PIN is wrong" must be indistinguishable, or the till becomes a way
			// to discover who has one.
		default:
			return contract.Principal{}, credErr
		}
	}

	if encoded == "" {
		_, _, _ = s.hasher.Verify(dummyHash, pin)
		return contract.Principal{}, domain.ErrInvalidCredentials()
	}

	ok, _, verifyErr := s.hasher.Verify(encoded, pin)
	if verifyErr != nil {
		return contract.Principal{}, domain.ErrInvalidCredentials()
	}
	if !ok || !user.IsActive {
		return contract.Principal{}, domain.ErrInvalidCredentials()
	}

	return contract.Principal{
		UserID: user.ID, CompanyID: user.CompanyID,
		Username: user.Username, DisplayName: user.DisplayName,
	}, nil
}
