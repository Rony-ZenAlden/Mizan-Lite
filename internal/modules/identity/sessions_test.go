package identity_test

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/identity/domain"
)

func login(t *testing.T, svc *identity.Service, ctx context.Context, username, password string) identity.LoginResult {
	t.Helper()
	result, err := svc.Login(ctx, identity.LoginInput{Username: username, Password: password})
	if err != nil {
		t.Fatalf("Login(%s): %v", username, err)
	}
	return result
}

// ── tokens ──────────────────────────────────────────────────────────────────────

// TestTokenIsNeverStored is the D3 guarantee: a backup must not be a set of working logins.
func TestTokenIsNeverStored(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)

	result := login(t, svc, ctx, "admin", goodPassword)
	if result.Token == "" {
		t.Fatal("no token was issued")
	}

	var stored string
	if err := store.Reader(ctx).QueryRowContext(ctx,
		`SELECT token_hash FROM sessions WHERE id = ?`, string(result.Session.ID)).
		Scan(&stored); err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	if stored == result.Token {
		t.Fatal("the raw token is stored in the database — a backup would contain working logins")
	}
	if stored != domain.HashToken(result.Token) {
		t.Errorf("stored value is not the token's hash")
	}
}

func TestTwoSessionsNeverShareAToken(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)

	first := login(t, svc, ctx, "admin", goodPassword)
	second := login(t, svc, ctx, "admin", goodPassword)
	if first.Token == second.Token {
		t.Fatal("two sessions share a token")
	}
}

// ── validation and expiry ───────────────────────────────────────────────────────

func TestValidateAcceptsALiveSession(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)
	result := login(t, svc, ctx, "admin", goodPassword)

	principal, _, err := svc.Validate(ctx, result.Token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if principal.Username != "admin" {
		t.Errorf("principal = %+v", principal)
	}
}

func TestValidateRejectsAnUnknownToken(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)

	_, _, err := svc.Validate(ctx, "not-a-real-token")
	if err == nil {
		t.Fatal("an unknown token validated")
	}
	if code := errs.CodeOf(err); code != domain.CodeSessionInvalid {
		t.Errorf("code = %q, want %q", code, domain.CodeSessionInvalid)
	}
}

// TestAbsoluteExpirySurvivesActivity is the mutation target.
//
// A session used constantly for thirteen hours is still over its absolute limit. Rolling the
// absolute window forward on use — the obvious mistake — would let a session live forever, and
// this is the test that catches it.
func TestAbsoluteExpirySurvivesActivity(t *testing.T) {
	fixed := clock.NewFixed(time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC))
	svc, store, companyID := newFixtureAt(t, fixed)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)

	result := login(t, svc, ctx, "admin", goodPassword)

	// Use it every 30 minutes for 13 hours — always inside the 60-minute idle window, so idle
	// expiry never fires, but well past the 12-hour absolute cap.
	var lastErr error
	for elapsed := time.Duration(0); elapsed <= 13*time.Hour; elapsed += 30 * time.Minute {
		_, _, lastErr = svc.Validate(ctx, result.Token)
		fixed.Advance(30 * time.Minute)
	}
	if lastErr == nil {
		t.Fatal("a session survived past its absolute expiry because activity kept extending it")
	}

	var reason string
	if err := store.Reader(ctx).QueryRowContext(ctx,
		`SELECT end_reason FROM sessions WHERE id = ?`, string(result.Session.ID)).
		Scan(&reason); err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	if reason != domain.EndAbsolute {
		t.Errorf("end_reason = %q, want %q", reason, domain.EndAbsolute)
	}
}

func TestIdleExpiryEndsAQuietSession(t *testing.T) {
	fixed := clock.NewFixed(time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC))
	svc, store, companyID := newFixtureAt(t, fixed)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)
	result := login(t, svc, ctx, "admin", goodPassword)

	fixed.Advance(61 * time.Minute) // past the 60-minute idle window

	if _, _, err := svc.Validate(ctx, result.Token); err == nil {
		t.Fatal("an idle session past its window still validated")
	}
}

func TestUseRollsTheIdleWindowForward(t *testing.T) {
	fixed := clock.NewFixed(time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC))
	svc, store, companyID := newFixtureAt(t, fixed)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)
	result := login(t, svc, ctx, "admin", goodPassword)

	// Use it at 50 minutes, then again 50 minutes later: both inside a rolling window, but
	// 100 minutes past login, so a non-rolling idle window would have expired.
	fixed.Advance(50 * time.Minute)
	if _, _, err := svc.Validate(ctx, result.Token); err != nil {
		t.Fatalf("validate at 50m: %v", err)
	}
	fixed.Advance(50 * time.Minute)
	if _, _, err := svc.Validate(ctx, result.Token); err != nil {
		t.Errorf("the idle window did not roll forward on use: %v", err)
	}
}

// TestTouchIsCoalesced pins D4: session upkeep must not put a write on every call, because
// SQLite has exactly one writer.
func TestTouchIsCoalesced(t *testing.T) {
	fixed := clock.NewFixed(time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC))
	svc, store, companyID := newFixtureAt(t, fixed)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)
	result := login(t, svc, ctx, "admin", goodPassword)

	readLastSeen := func() string {
		var v string
		if err := store.Reader(ctx).QueryRowContext(ctx,
			`SELECT last_seen_at FROM sessions WHERE id = ?`, string(result.Session.ID)).
			Scan(&v); err != nil {
			t.Fatalf("reading last_seen_at: %v", err)
		}
		return v
	}

	before := readLastSeen()
	// Several validations within the granularity: the row must not move.
	for i := 0; i < 5; i++ {
		if _, _, err := svc.Validate(ctx, result.Token); err != nil {
			t.Fatalf("validate: %v", err)
		}
	}
	if readLastSeen() != before {
		t.Error("last_seen_at was written on every call; upkeep is not coalesced")
	}

	// Past the granularity, it must move.
	fixed.Advance(2 * time.Minute)
	if _, _, err := svc.Validate(ctx, result.Token); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if readLastSeen() == before {
		t.Error("last_seen_at never moved; the idle window would never roll forward")
	}
}

// ── revocation ──────────────────────────────────────────────────────────────────

// TestRevocationTakesEffectImmediately: an administrator's "sign this device out" must not be
// a lie for as long as a cache entry lives. There is deliberately no session cache.
func TestRevocationTakesEffectImmediately(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)
	result := login(t, svc, ctx, "admin", goodPassword)

	if _, _, err := svc.Validate(ctx, result.Token); err != nil {
		t.Fatalf("the session did not start valid: %v", err)
	}
	if err := svc.Revoke(ctx, result.Session.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, _, err := svc.Validate(ctx, result.Token); err == nil {
		t.Error("a revoked session still validates")
	}
}

func TestLogoutEndsTheSessionButKeepsTheRow(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)
	result := login(t, svc, ctx, "admin", goodPassword)

	if err := svc.Logout(ctx, result.Token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, _, err := svc.Validate(ctx, result.Token); err == nil {
		t.Error("a logged-out session still validates")
	}

	// The row survives: "who was signed in when this happened" is an audit question.
	var reason string
	if err := store.Reader(ctx).QueryRowContext(ctx,
		`SELECT end_reason FROM sessions WHERE id = ?`, string(result.Session.ID)).
		Scan(&reason); err != nil {
		t.Fatalf("the session row was deleted on logout: %v", err)
	}
	if reason != domain.EndLogout {
		t.Errorf("end_reason = %q, want %q", reason, domain.EndLogout)
	}
}

func TestDeactivatingAUserEndsTheirSession(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)
	second := createUser(t, svc, ctx, companyID, "cashier", goodPassword)
	result := login(t, svc, ctx, "cashier", goodPassword)

	if err := svc.SetUserActive(ctx, second.ID, false); err != nil {
		t.Fatalf("SetUserActive: %v", err)
	}
	if _, _, err := svc.Validate(ctx, result.Token); err == nil {
		t.Error("a deactivated user's session still validates")
	}
}

// ── throttling ──────────────────────────────────────────────────────────────────

func failLogin(svc *identity.Service, ctx context.Context, username string) error {
	_, err := svc.Login(ctx, identity.LoginInput{Username: username, Password: "wrong password!!"})
	return err
}

func TestFailuresBelowTheThresholdDoNotThrottle(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)

	for i := 0; i < 4; i++ { // threshold is 5
		if err := failLogin(svc, ctx, "admin"); errs.CodeOf(err) == domain.CodeTooManyAttempts {
			t.Fatalf("throttled after only %d failures", i+1)
		}
	}
	// The correct password still works.
	if _, err := svc.Login(ctx, identity.LoginInput{Username: "admin", Password: goodPassword}); err != nil {
		t.Errorf("a valid login was refused below the threshold: %v", err)
	}
}

func TestThrottleEngagesAtTheThreshold(t *testing.T) {
	fixed := clock.NewFixed(time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC))
	svc, store, companyID := newFixtureAt(t, fixed)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)

	for i := 0; i < 5; i++ {
		_ = failLogin(svc, ctx, "admin")
	}

	err := failLogin(svc, ctx, "admin")
	if code := errs.CodeOf(err); code != domain.CodeTooManyAttempts {
		t.Fatalf("code = %q, want %q after reaching the threshold", code, domain.CodeTooManyAttempts)
	}
	e, _ := errs.AsError(err)
	if e.Params["retryAfter"] == "" {
		t.Error("the throttle did not tell the user how long to wait")
	}

	// Even the CORRECT password is refused while throttled.
	if _, loginErr := svc.Login(ctx, identity.LoginInput{
		Username: "admin", Password: goodPassword,
	}); errs.CodeOf(loginErr) != domain.CodeTooManyAttempts {
		t.Error("the throttle did not apply to a correct password")
	}
}

// TestTheThrottleAlwaysElapses is the D2 guarantee, and the most important test in this step.
//
// A desktop ERP has no password-reset email. If failing repeatedly could lock the sole
// administrator out permanently, an attacker could take the business offline with five wrong
// guesses against a username printed on its own invoices. The delay must always run out.
func TestTheThrottleAlwaysElapses(t *testing.T) {
	fixed := clock.NewFixed(time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC))
	svc, store, companyID := newFixtureAt(t, fixed)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)

	// Hammer it well past the threshold.
	for i := 0; i < 25; i++ {
		_ = failLogin(svc, ctx, "admin")
		fixed.Advance(time.Second)
	}

	// Wait out the capped delay (max 900s).
	fixed.Advance(16 * time.Minute)

	if _, err := svc.Login(ctx, identity.LoginInput{
		Username: "admin", Password: goodPassword,
	}); err != nil {
		t.Fatalf("the account was still locked after the cap elapsed — "+
			"an attacker can take the business offline permanently: %v", err)
	}
}

// TestASuccessClearsTheCounter — mutation target: the counter is defined as "failures since
// the last success", so a success must reset it with no separate write.
func TestASuccessClearsTheCounter(t *testing.T) {
	fixed := clock.NewFixed(time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC))
	svc, store, companyID := newFixtureAt(t, fixed)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)

	for i := 0; i < 4; i++ {
		_ = failLogin(svc, ctx, "admin")
		fixed.Advance(time.Second)
	}
	login(t, svc, ctx, "admin", goodPassword) // success clears
	fixed.Advance(time.Second)

	// Four more failures must again be below the threshold, not above it.
	for i := 0; i < 4; i++ {
		if err := failLogin(svc, ctx, "admin"); errs.CodeOf(err) == domain.CodeTooManyAttempts {
			t.Fatalf("throttled after %d failures following a success — the counter did not clear", i+1)
		}
		fixed.Advance(time.Second)
	}
}

func TestThrottleIsPerUsername(t *testing.T) {
	fixed := clock.NewFixed(time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC))
	svc, store, companyID := newFixtureAt(t, fixed)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)
	createUser(t, svc, ctx, companyID, "cashier", goodPassword)

	for i := 0; i < 6; i++ {
		_ = failLogin(svc, ctx, "admin")
	}
	// The other account is untouched: one user's failures must not lock out another.
	if _, err := svc.Login(ctx, identity.LoginInput{
		Username: "cashier", Password: goodPassword,
	}); err != nil {
		t.Errorf("one account's failures throttled a different account: %v", err)
	}
}

// ── login attempts ──────────────────────────────────────────────────────────────

func TestAttemptsAreRecordedIncludingUnknownUsernames(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)

	login(t, svc, ctx, "admin", goodPassword)
	_ = failLogin(svc, ctx, "admin")
	_ = failLogin(svc, ctx, "ghost") // no such user

	type row struct {
		username  string
		succeeded int
		reason    string
		hasUser   bool
	}
	rows, err := store.Reader(ctx).QueryContext(ctx,
		`SELECT username, succeeded, reason, user_id IS NOT NULL FROM login_attempts ORDER BY attempted_at`)
	if err != nil {
		t.Fatalf("reading attempts: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var got []row
	for rows.Next() {
		var r row
		if scanErr := rows.Scan(&r.username, &r.succeeded, &r.reason, &r.hasUser); scanErr != nil {
			t.Fatalf("scan: %v", scanErr)
		}
		got = append(got, r)
	}
	if len(got) != 3 {
		t.Fatalf("recorded %d attempts, want 3", len(got))
	}

	// An attempt against a username that does not exist IS the shape of an attack; discarding
	// it discards the evidence.
	last := got[len(got)-1]
	if last.username != "ghost" || last.hasUser {
		t.Errorf("unknown-username attempt = %+v, want username=ghost with a NULL user_id", last)
	}
	if last.reason != domain.ReasonUnknownUser {
		t.Errorf("reason = %q, want %q — the record knows what the caller is not told",
			last.reason, domain.ReasonUnknownUser)
	}
}

// ── the sweep job ───────────────────────────────────────────────────────────────

func TestSweepEndsExpiredSessions(t *testing.T) {
	fixed := clock.NewFixed(time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC))
	svc, store, companyID := newFixtureAt(t, fixed)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)
	result := login(t, svc, ctx, "admin", goodPassword)

	fixed.Advance(13 * time.Hour) // past the absolute window

	ended, _, err := svc.SweepSessions(ctx)
	if err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	if ended != 1 {
		t.Errorf("ended = %d, want 1", ended)
	}

	var reason string
	if scanErr := store.Reader(ctx).QueryRowContext(ctx,
		`SELECT end_reason FROM sessions WHERE id = ?`, string(result.Session.ID)).
		Scan(&reason); scanErr != nil {
		t.Fatalf("reading the session: %v", scanErr)
	}
	if reason == "" {
		t.Error("the sweep did not mark the session ended")
	}
}

// TestModuleDeclaresTheSweepJobWithAHandler pins the contract defect this step found: the
// Module.Jobs() path registered a nil handler, which Register rejects — so no module could
// ever have declared a job.
func TestModuleDeclaresTheSweepJobWithAHandler(t *testing.T) {
	svc, _, _ := newFixture(t)
	registrations := identity.NewModule(svc).Jobs()

	if len(registrations) != 1 {
		t.Fatalf("jobs = %d, want 1", len(registrations))
	}
	if registrations[0].Def.Key != "identity.session_sweep" {
		t.Errorf("key = %q", registrations[0].Def.Key)
	}
	if registrations[0].Handler == nil {
		t.Fatal("the job has no handler; Register would refuse it and startup would fail")
	}
}

// ── appctx ──────────────────────────────────────────────────────────────────────

// TestScopesReportTheActor closes the item 0.10 §6.2 and 0.11 D4 both carried.
func TestScopesReportTheActor(t *testing.T) {
	var scopes appctx.Scopes

	// With no actor — the login screen, the setup wizard — every scope is absent, and settings
	// resolve at system scope exactly as they did through Phase 0.
	plain := context.Background()
	if _, ok := scopes.UserID(plain); ok {
		t.Error("a context with no actor reported a user")
	}
	if _, ok := scopes.CompanyID(plain); ok {
		t.Error("a context with no actor reported a company")
	}

	userID, _ := id.New()
	companyID, _ := id.New()
	branchID, _ := id.New()
	ctx := appctx.WithActor(plain, appctx.Actor{
		UserID: userID, CompanyID: companyID, BranchID: branchID, Username: "admin",
	})

	if got, ok := scopes.UserID(ctx); !ok || got != userID {
		t.Errorf("UserID = (%v, %v), want (%v, true)", got, ok, userID)
	}
	if got, ok := scopes.CompanyID(ctx); !ok || got != companyID {
		t.Errorf("CompanyID = (%v, %v)", got, ok)
	}
	if got, ok := scopes.BranchID(ctx); !ok || got != branchID {
		t.Errorf("BranchID = (%v, %v)", got, ok)
	}
}
