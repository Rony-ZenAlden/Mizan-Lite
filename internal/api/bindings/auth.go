package bindings

import (
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/modules/identity"
)

// SessionDTO is who the frontend thinks it is.
//
// It carries NO token (Step 1.5, D3). The token lives in the process, so it cannot be written
// to localStorage, printed to a console, or read by an injected script — the frontend never
// holds a credential it could leak.
type SessionDTO struct {
	SignedIn    bool   `json:"signedIn"`
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	// MustChange forces a password change before anything else is allowed. The 1.10 login flow
	// is what acts on it.
	MustChange bool `json:"mustChange"`
	// Permissions lets the UI hide unavailable actions. COSMETIC ONLY — the backend is the sole
	// enforcement point (§14.3), and every guarded method re-checks.
	Permissions []string `json:"permissions"`
}

// Auth is signing in and out.
type Auth struct {
	graph
	session *currentSession
	// remember persists the token across restarts when the user asked to stay signed in.
	//
	// A VALUE, filled in by Attach. A pointer would have to be non-nil before the data
	// directory is known, and the zero value is already correct: a store with no path is
	// disabled, so every method on it is a safe no-op.
	remember rememberedToken
}

// authPolicies declares what Auth's methods require.
//
// All three are Public, each for a reason worth stating rather than assuming:
//
//   - Login must be reachable by an unauthenticated caller; that is its entire purpose.
//   - Logout is public because a session that has JUST expired must still be able to clear
//     itself. Requiring a valid session to log out would strand the frontend holding a dead
//     token with no way to discard it.
//   - Me answers "am I signed in, and as whom?" — the question the frontend asks BEFORE it
//     knows the answer.
func authPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		"Login":  policy.Public(),
		"Logout": policy.Public(),
		"Me":     policy.Public(),
	}
}

// Login signs a user in and stores the session for this window.
func (a *Auth) Login(username, password string, remember bool) envelope.Result[SessionDTO] {
	ctx, app, err := a.guard("Login")
	if err != nil {
		return envelope.Fail[SessionDTO](err)
	}

	result, err := app.Identity.Login(ctx, identity.LoginInput{
		Username: username, Password: password, Remember: remember,
		DeviceInfo: "desktop",
	})
	if err != nil {
		// Every failure — unknown user, wrong password, inactive account — crosses as the same
		// code (1.2), and a throttled attempt crosses as its own with a retryAfter (1.3 D6).
		return envelope.Fail[SessionDTO](err)
	}

	a.session.set(result.Token)
	if remember {
		// §13.1's "stay signed in" only means something if the token outlives the process.
		// Best-effort: a failed write costs one sign-in tomorrow, and refusing a successful
		// login over it would be the wrong trade entirely.
		_ = a.remember.write(result.Token)
	}

	// Permissions are read through a context carrying the new actor, not the pre-login one.
	dto := SessionDTO{
		SignedIn:    true,
		UserID:      string(result.Principal.UserID),
		Username:    result.Principal.Username,
		DisplayName: result.Principal.DisplayName,
		MustChange:  result.Principal.MustChange,
	}
	if _, signedIn, meErr := a.currentPrincipal(); meErr == nil && signedIn.SignedIn {
		dto.Permissions = signedIn.Permissions
	}
	return envelope.Ok(dto)
}

// Logout ends the current session.
//
// Succeeds even when the session is already gone: logging out of nothing is not a failure, and
// the frontend must always be able to reach a signed-out state.
func (a *Auth) Logout() envelope.Result[bool] {
	ctx, app, err := a.guard("Logout")
	if err != nil {
		return envelope.Fail[bool](err)
	}
	token := a.session.get()
	a.session.clear()
	// Always, even when there was no live token: a stored one may outlive the in-memory copy
	// after a restore that failed, and "sign out" must leave nothing behind that could sign
	// this machine back in.
	a.remember.clear()
	if token == "" {
		return envelope.Ok(true)
	}
	if logoutErr := app.Identity.Logout(ctx, token); logoutErr != nil {
		return envelope.Fail[bool](logoutErr)
	}
	return envelope.Ok(true)
}

// Me reports the current session, or a signed-out result.
//
// Returns Ok with SignedIn false rather than an error when nobody is signed in: "not signed in"
// is an answer, not a failure, and the frontend's first call on every launch is this one.
func (a *Auth) Me() envelope.Result[SessionDTO] {
	if _, _, err := a.guard("Me"); err != nil {
		return envelope.Fail[SessionDTO](err)
	}
	_, dto, err := a.currentPrincipal()
	if err != nil {
		return envelope.Fail[SessionDTO](err)
	}
	return envelope.Ok(dto)
}

// currentPrincipal resolves the window's session into a DTO.
func (a *Auth) currentPrincipal() (bool, SessionDTO, error) {
	app, ok := a.resolve()
	if !ok {
		return false, SessionDTO{}, notReady()
	}
	token := a.session.get()
	if token == "" {
		return false, SessionDTO{}, nil
	}

	ctx := app.Context()
	principal, session, validateErr := app.Identity.Validate(ctx, token)
	if validateErr != nil {
		// An expired or revoked session reads as SIGNED OUT, not as an error. The frontend's
		// correct response is the login screen either way, and returning an error here would
		// make "your session ended" indistinguishable from "the app is broken".
		//
		// nilerr is right to flag a discarded error; this one is discarded on purpose.
		a.session.clear()
		return false, SessionDTO{}, nil //nolint:nilerr // an ended session is a state, not a failure
	}

	ctx = withActor(ctx, principal, session)
	permissions, err := app.Identity.Effective(ctx)
	if err != nil {
		return false, SessionDTO{}, err
	}

	return true, SessionDTO{
		SignedIn:    true,
		UserID:      string(principal.UserID),
		Username:    principal.Username,
		DisplayName: principal.DisplayName,
		MustChange:  principal.MustChange,
		Permissions: permissions,
	}, nil
}
