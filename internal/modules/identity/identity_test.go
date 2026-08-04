package identity_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/identity/domain"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/migrations"
)

// ── harness ─────────────────────────────────────────────────────────────────────

const goodPassword = "a sufficiently long passphrase"

// newFixture builds a provisioned company with an identity service over the real merged
// schema (0001–0005), so every foreign key and unique constraint is genuinely exercised.
func newFixture(t *testing.T) (*identity.Service, *database.Store, id.ID) {
	t.Helper()
	return newFixtureAt(t, clock.System())
}

// newFixtureAt is newFixture with an injected clock, so session expiry and throttle backoff can
// be exercised against controlled time rather than by sleeping.
func newFixtureAt(t *testing.T, clk clock.Clock) (*identity.Service, *database.Store, id.ID) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")

	store, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	merged := migrate.Merge(
		migrations.SQLite(),
		currency.NewModule(nil).Migrations(),
		org.NewModule(nil).Migrations(),
		identity.NewModule(nil).Migrations(),
	)
	runner, err := migrate.New(store, migrate.Options{FS: merged, DBPath: path, SkipBackup: true})
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	if _, err = runner.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// A currency, then a provisioned company: identity's users carry a FK to companies(id).
	identifier, _ := id.New()
	now := clock.Format(clock.System().Now())
	if _, err = store.Writer(ctx).ExecContext(ctx,
		`INSERT INTO currencies (id, code, name, symbol, decimal_places, created_at, updated_at)
		 VALUES (?, 'SYP', 'Syrian Pound', 'SYP', 0, ?, ?)`,
		string(identifier), now, now); err != nil {
		t.Fatalf("seeding currency: %v", err)
	}

	orgSvc := org.NewService(store, clk)
	result, err := orgSvc.Provision(ctx, org.ProvisionInput{
		Company: org.CompanyInput{
			Code: "MAIN", Name: "Demo", CountryCode: "SY", FunctionalCurrency: "SYP",
		},
		Branch:               org.LocationInput{Code: "HQ", Name: "Head Office"},
		Warehouse:            org.LocationInput{Code: "WH1", Name: "Main"},
		FiscalYearStartMonth: time.January,
		FiscalYearStartYear:  2026,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	return identity.NewService(store, orgSvc, clk), store, result.CompanyID
}

// bound returns a context with settings bound, so the password policy resolves.
func bound(t *testing.T, store *database.Store) context.Context {
	t.Helper()
	settings, err := config.Open(context.Background(), store, config.Options{})
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}
	return config.Bind(context.Background(), settings)
}

func createUser(t *testing.T, svc *identity.Service, ctx context.Context, companyID id.ID,
	username, password string) domain.User {
	t.Helper()
	user, err := svc.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: companyID, Username: username, DisplayName: "Test User",
		Password: password, IsSystem: false,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	return user
}

// ── authentication ──────────────────────────────────────────────────────────────

func TestAuthenticateAcceptsCorrectCredentials(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)

	principal, err := svc.Authenticate(ctx, "admin", goodPassword)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if principal.Username != "admin" || principal.CompanyID != companyID {
		t.Errorf("principal = %+v", principal)
	}
}

// TestUsernameIsCaseInsensitive: the unique constraint is on the stored (lower-cased) value,
// so login must normalise identically or an account could be created but never signed into.
func TestUsernameIsCaseInsensitive(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "Admin", goodPassword)

	if _, err := svc.Authenticate(ctx, "  ADMIN  ", goodPassword); err != nil {
		t.Errorf("a differently-cased username failed to authenticate: %v", err)
	}
}

// TestEveryFailureIsIndistinguishable is the username-oracle guard.
//
// Unknown user, wrong password, and deactivated account must be the SAME error. Anything else
// turns an untargeted password spray into a targeted one.
func TestEveryFailureIsIndistinguishable(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)
	other := createUser(t, svc, ctx, companyID, "disabled", goodPassword)
	if err := svc.SetUserActive(ctx, other.ID, false); err != nil {
		t.Fatalf("deactivating: %v", err)
	}

	cases := map[string][2]string{
		"unknown user":   {"nobody", goodPassword},
		"wrong password": {"admin", "the wrong passphrase entirely"},
		"inactive user":  {"disabled", goodPassword},
	}
	for name, pair := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Authenticate(ctx, pair[0], pair[1])
			if err == nil {
				t.Fatal("authentication succeeded when it must not have")
			}
			if code := errs.CodeOf(err); code != domain.CodeInvalidCredentials {
				t.Errorf("code = %q, want %q — failures must be indistinguishable",
					code, domain.CodeInvalidCredentials)
			}
			if !errs.IsCategory(err, errs.CategoryPermission) {
				t.Errorf("category = %v, want Permission", errs.CategoryOf(err))
			}
		})
	}
}

// TestUnknownUserStillDoesTheHashingWork is the timing drill.
//
// Returning early for an unknown user makes "no such user" microseconds and "wrong password"
// the full Argon2id cost — enough to enumerate accounts. Asserted as a RATIO against the
// known-user path rather than an absolute time, so it does not become flaky on a loaded
// machine.
//
// Mutation check: return before the dummy Verify in Authenticate and this fails.
func TestUnknownUserStillDoesTheHashingWork(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)

	measure := func(username string) time.Duration {
		// Several attempts, taking the best, so scheduler noise inflates rather than deflates
		// the fast path — which is the direction that makes a false PASS impossible.
		best := time.Hour
		for i := 0; i < 3; i++ {
			start := time.Now()
			_, _ = svc.Authenticate(ctx, username, "some wrong password value")
			if elapsed := time.Since(start); elapsed < best {
				best = elapsed
			}
		}
		return best
	}

	known := measure("admin")
	unknown := measure("definitely-not-a-user")

	// The unknown path must cost the same order of magnitude. A missing dummy verification
	// makes it hundreds of times faster, so a generous bound still catches it decisively.
	if unknown < known/4 {
		t.Errorf("an unknown username returned in %v against %v for a known one — "+
			"the constant-work verification is missing, and usernames can be enumerated",
			unknown, known)
	}
}

// ── the leak drill ──────────────────────────────────────────────────────────────

// TestNoCredentialMaterialReachesADTO asserts on the MARSHALLED bytes, not on struct fields.
//
// The single worst leak in the system would be a password hash crossing the API boundary. The
// structural defence is that credentials live in their own table and `userColumns` names every
// column it reads — this is the assertion that the defence holds end to end.
func TestNoCredentialMaterialReachesADTO(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)

	users, err := svc.Users(ctx, companyID)
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("users = %d", len(users))
	}

	raw, err := json.Marshal(users)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	serialised := string(raw)

	for _, forbidden := range []string{"argon2", "$", "hash", "encoded", "password", "credential"} {
		if strings.Contains(strings.ToLower(serialised), forbidden) {
			t.Errorf("a user read serialised something containing %q: %s", forbidden, serialised)
		}
	}
}

// ── password policy ─────────────────────────────────────────────────────────────

func TestPasswordPolicyRejectsShortPasswords(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)

	_, err := svc.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: companyID, Username: "shorty", DisplayName: "Short", Password: "abc",
	})
	if err == nil {
		t.Fatal("a three-character password was accepted")
	}
	if code := errs.CodeOf(err); code != domain.CodePasswordTooShort {
		t.Errorf("code = %q, want %q", code, domain.CodePasswordTooShort)
	}
}

// TestPolicyAcceptsALongPassphrase pins the NIST-aligned choice (D3): length over composition,
// and no maximum. A passphrase of ordinary words with no symbol must be accepted.
func TestPolicyAcceptsALongPassphrase(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)

	if _, err := svc.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: companyID, Username: "passphrase", DisplayName: "P",
		Password: "correct horse battery staple with no symbols at all",
	}); err != nil {
		t.Errorf("a long passphrase was rejected: %v", err)
	}
}

// TestPasswordReuseIsRejected — mutation check: skip the history check in SetPassword and this
// fails.
func TestPasswordReuseIsRejected(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	user := createUser(t, svc, ctx, companyID, "admin", goodPassword)

	// Changing to something new is fine.
	const second = "a different long passphrase"
	if err := svc.SetPassword(ctx, user.ID, second); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	// Going back to the original must be refused.
	err := svc.SetPassword(ctx, user.ID, goodPassword)
	if err == nil {
		t.Fatal("a recently-used password was accepted")
	}
	if code := errs.CodeOf(err); code != domain.CodePasswordReused {
		t.Errorf("code = %q, want %q", code, domain.CodePasswordReused)
	}

	// And the current password still works.
	if _, err = svc.Authenticate(ctx, "admin", second); err != nil {
		t.Errorf("the current password stopped working: %v", err)
	}
}

func TestSetPasswordChangesTheCredential(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	user := createUser(t, svc, ctx, companyID, "admin", goodPassword)

	const replacement = "an entirely different phrase"
	if err := svc.SetPassword(ctx, user.ID, replacement); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "admin", goodPassword); err == nil {
		t.Error("the old password still authenticates")
	}
	if _, err := svc.Authenticate(ctx, "admin", replacement); err != nil {
		t.Errorf("the new password does not authenticate: %v", err)
	}
}

// ── invariants ──────────────────────────────────────────────────────────────────

func TestTheLastActiveUserCannotBeDeactivated(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	user := createUser(t, svc, ctx, companyID, "admin", goodPassword)

	err := svc.SetUserActive(ctx, user.ID, false)
	if err == nil {
		t.Fatal("the only user was deactivated; nobody can ever log in again")
	}
	if code := errs.CodeOf(err); code != domain.CodeLastActiveUser {
		t.Errorf("code = %q, want %q", code, domain.CodeLastActiveUser)
	}
}

func TestASecondUserCanBeDeactivated(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)
	second := createUser(t, svc, ctx, companyID, "cashier", goodPassword)

	if err := svc.SetUserActive(ctx, second.ID, false); err != nil {
		t.Errorf("deactivating one of two users failed: %v", err)
	}
}

func TestDuplicateUsernamesAreRefused(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)
	createUser(t, svc, ctx, companyID, "admin", goodPassword)

	_, err := svc.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: companyID, Username: "ADMIN", DisplayName: "Impostor", Password: goodPassword,
	})
	if err == nil {
		t.Fatal("a duplicate username (differing only in case) was accepted")
	}
}

// TestAFailedUserCreationLeavesNothing: a user without a credential cannot log in and is
// invisible to every "who can sign in" screen, so a partial create is never worth having.
func TestAFailedUserCreationLeavesNothing(t *testing.T) {
	svc, store, companyID := newFixture(t)
	ctx := bound(t, store)

	// No display name: the domain rejects it AFTER the policy check passes.
	if _, err := svc.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: companyID, Username: "ghost", DisplayName: "", Password: goodPassword,
	}); err == nil {
		t.Fatal("an invalid user was created")
	}

	users, err := svc.Users(ctx, companyID)
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("users = %d after a failed create, want 0 — the transaction leaked", len(users))
	}
}

// ── module wiring ───────────────────────────────────────────────────────────────

func TestModuleDeclaresItsDependency(t *testing.T) {
	m := identity.NewModule(nil)
	if m.Name() != "identity" {
		t.Errorf("name = %q", m.Name())
	}
	if deps := m.DependsOn(); len(deps) != 1 || deps[0] != "org" {
		t.Errorf("DependsOn = %v, want [org]", deps)
	}
}

func TestModuleSeedsNoUsers(t *testing.T) {
	// A seeded account is a default credential, and §13.1 is explicit that none ships.
	if specs := identity.NewModule(nil).Metadata(); len(specs) != 0 {
		t.Errorf("identity seeds %d metadata specs; no default account may ship", len(specs))
	}
}
