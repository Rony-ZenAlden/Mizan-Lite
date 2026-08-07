package bindings_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/internal/platform/paths"
	"github.com/mizan-erp/mizan/internal/platform/ui"
)

// ── harness ─────────────────────────────────────────────────────────────────────

func boot(t *testing.T) *bootstrap.App { return bootWithSeeds(t, nil) }

// bootWithSeeds boots an app, first writing any given files into the data directory.
//
// The files go where a customer's would (1.8's overlay layer), so a test fixture and a
// customer's own profile take exactly the same path into the system.
func bootWithSeeds(t *testing.T, files map[string]string) *bootstrap.App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(paths.EnvOverride, dir)

	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("seed dir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("seed file: %v", err)
		}
	}

	resolved, err := paths.Resolve("Mizan")
	if err != nil {
		t.Fatalf("paths.Resolve: %v", err)
	}
	app, err := bootstrap.Start(context.Background(), bootstrap.Options{
		Paths:      resolved,
		SkipBackup: true,
	})
	if err != nil {
		t.Fatalf("bootstrap.Start: %v", err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	return app
}

// signedIn returns a binding set with an administrator logged in.
//
// The full path a real launch takes: provision the company, seed the roles, create the
// administrator, and sign in through the Auth binding — so the token the guard reads is one
// Login actually issued.
func signedIn(t *testing.T) (*bindings.Set, *bootstrap.App) {
	t.Helper()
	app := boot(t)
	ctx := app.Context()

	result, err := app.Org.Provision(ctx, org.ProvisionInput{
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
	if err = app.Identity.SeedRoles(ctx, result.CompanyID); err != nil {
		t.Fatalf("SeedRoles: %v", err)
	}
	user, err := app.Identity.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: result.CompanyID, Username: "admin", DisplayName: "Admin",
		Password: "a sufficiently long passphrase", IsSystem: true,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err = app.Identity.AssignRoleByCode(ctx, user.ID, result.CompanyID, "administrator"); err != nil {
		t.Fatalf("AssignRoleByCode: %v", err)
	}

	set := bindings.New()
	set.Attach(app)

	login := set.Auth.Login("admin", "a sufficiently long passphrase", false)
	if !login.OK {
		t.Fatalf("Login failed: %+v", login.Error)
	}
	return set, app
}

// probe describes one guarded binding method, so the not-ready rule can be asserted over ALL
// of them rather than over whichever one someone remembered to test.
type probe struct {
	name string
	// call invokes the method and returns (ok, errorCode) from its envelope.
	call func(*bindings.Set) (bool, string)
}

func guardedMethods() []probe {
	return []probe{
		{"System.Health", func(s *bindings.Set) (bool, string) {
			r := s.System.Health()
			return r.OK, codeOf(r.Error)
		}},
		{"Config.Preferences", func(s *bindings.Set) (bool, string) {
			r := s.Config.Preferences()
			return r.OK, codeOf(r.Error)
		}},
		{"Config.SetLocale", func(s *bindings.Set) (bool, string) {
			r := s.Config.SetLocale("ar")
			return r.OK, codeOf(r.Error)
		}},
		{"Config.SetTheme", func(s *bindings.Set) (bool, string) {
			r := s.Config.SetTheme(ui.ThemeDark)
			return r.OK, codeOf(r.Error)
		}},
		{"Ops.Jobs", func(s *bindings.Set) (bool, string) {
			r := s.Ops.Jobs()
			return r.OK, codeOf(r.Error)
		}},
		{"Ops.Runs", func(s *bindings.Set) (bool, string) {
			r := s.Ops.Runs("", 10)
			return r.OK, codeOf(r.Error)
		}},
		{"Money.Currencies", func(s *bindings.Set) (bool, string) {
			r := s.Money.Currencies()
			return r.OK, codeOf(r.Error)
		}},
	}
}

func codeOf(e *envelope.APIError) string {
	if e == nil {
		return ""
	}
	return e.Code
}

// ── the not-ready guard ─────────────────────────────────────────────────────────

// TestEveryGuardedMethodReportsNotReadyBeforeAttach is the drill for D2's central risk.
//
// The window now opens before the object graph exists, so every binding is reachable from
// JavaScript while app is nil. If any method dereferenced it, a customer's first launch would
// crash the moment the webview finished loading — the exact failure the façade guard exists to
// prevent. A table over every method means a NEW binding is covered the day it is added,
// rather than the day someone remembers to test it.
//
// Mutation check: delete the resolve() guard from any one method and its row fails (with a
// panic recovered by the test, not a nil result).
func TestEveryGuardedMethodReportsNotReadyBeforeAttach(t *testing.T) {
	for _, p := range guardedMethods() {
		t.Run(p.name, func(t *testing.T) {
			set := bindings.New() // deliberately NOT attached

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked on a nil graph: %v", p.name, r)
				}
			}()

			ok, code := p.call(set)
			if ok {
				t.Fatalf("%s succeeded before Attach; it must report not-ready", p.name)
			}
			if code != bindings.CodeNotReady {
				t.Errorf("%s code = %q, want %q", p.name, code, bindings.CodeNotReady)
			}
		})
	}
}

// TestGuardedMethodsStillNeedASessionAfterAttach.
//
// Until Step 1.5 this test asserted that attaching the graph was ENOUGH — every guarded method
// then succeeded. That is no longer true, and the change is the point of the step: the graph
// being ready says nothing about who is asking.
func TestGuardedMethodsStillNeedASessionAfterAttach(t *testing.T) {
	app := boot(t)
	set := bindings.New()
	set.Attach(app)

	for _, p := range guardedMethods() {
		t.Run(p.name, func(t *testing.T) {
			ok, code := p.call(set)
			if ok {
				t.Fatalf("%s succeeded with no session — the surface is unauthenticated", p.name)
			}
			if code != "identity.session_invalid" {
				t.Errorf("%s code = %q, want a session error", p.name, code)
			}
		})
	}
}

// TestGuardedMethodsWorkForASignedInUser is the positive half.
func TestGuardedMethodsWorkForASignedInUser(t *testing.T) {
	set, _ := signedIn(t)

	for _, p := range guardedMethods() {
		t.Run(p.name, func(t *testing.T) {
			ok, code := p.call(set)
			if !ok {
				t.Fatalf("%s failed for a signed-in administrator: %s", p.name, code)
			}
		})
	}
}

// TestAttachIsSafeUnderConcurrentCalls exercises the real handoff: Attach runs on the boot
// goroutine while the webview may already be calling bindings.
func TestAttachIsSafeUnderConcurrentCalls(t *testing.T) {
	app := boot(t)
	set := bindings.New()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				// Whether these succeed depends on the race; that they never panic or tear
				// is the property under test (run with -race).
				_ = set.System.Health()
				_ = set.Boot.Status()
			}
		}()
	}
	set.Attach(app)
	wg.Wait()

	// Health still needs a session; what this test asserts is that the concurrent Attach did
	// not tear or panic, and that the façade is functional afterwards.
	if r := set.System.Health(); r.OK {
		t.Error("Health succeeded with no session")
	} else if r.Error.Code != "identity.session_invalid" {
		t.Errorf("Health after Attach = %q, want a session error", r.Error.Code)
	}
}

// ── boot status ─────────────────────────────────────────────────────────────────

// TestBootStatusIsCallableBeforeAttach: Boot is the ONE binding that must work with no graph,
// because it is what reports whether the graph exists.
func TestBootStatusIsCallableBeforeAttach(t *testing.T) {
	set := bindings.New()

	r := set.Boot.Status()
	if !r.OK {
		t.Fatalf("Boot.Status failed before Attach: %+v", r.Error)
	}
	if r.Data.State != bindings.BootStarting {
		t.Errorf("state = %q, want %q", r.Data.State, bindings.BootStarting)
	}
}

func TestBootProgressThenReady(t *testing.T) {
	app := boot(t)
	set := bindings.New()

	set.Progress(migrate.Progress{
		Phase: migrate.PhaseMigrating, Current: 2, Total: 3, Name: "0002_outbox_deliveries.sql",
	})

	got := set.Boot.Status().Data
	if got.State != bindings.BootStarting {
		t.Errorf("state = %q, want still starting", got.State)
	}
	if got.Current != 2 || got.Total != 3 || got.Name != "0002_outbox_deliveries.sql" {
		t.Errorf("progress not recorded: %+v", got)
	}

	set.Attach(app)

	got = set.Boot.Status().Data
	if got.State != bindings.BootReady {
		t.Errorf("state = %q, want %q", got.State, bindings.BootReady)
	}
	if got.Error != nil {
		t.Errorf("ready status carries an error: %+v", got.Error)
	}
}

func TestBootFailureCarriesCodeAndBackupPath(t *testing.T) {
	set := bindings.New()

	failure := errs.Conflict("migrate.migration_failed", "developer-facing text only").
		WithParam("migration", "0003_currency.sql").
		WithParam("backup", "/data/backups/pre-migration-1.db")

	set.Fail(failure, "/data/backups/pre-migration-1.db")

	got := set.Boot.Status().Data
	if got.State != bindings.BootFailed {
		t.Fatalf("state = %q, want %q", got.State, bindings.BootFailed)
	}
	if got.Error == nil || got.Error.Code != "migrate.migration_failed" {
		t.Fatalf("error = %+v", got.Error)
	}
	if got.Error.Params["migration"] != "0003_currency.sql" {
		t.Errorf("params lost: %+v", got.Error.Params)
	}
	if got.BackupPath != "/data/backups/pre-migration-1.db" {
		t.Errorf("backupPath = %q", got.BackupPath)
	}
}

// TestBootFailureCarriesNoEnglishProse applies the Step 0.10 envelope rule to the boot path.
//
// This is the message a customer is most likely to read, and most likely to read under stress.
// If developer text leaked anywhere, it would leak here first.
func TestBootFailureCarriesNoEnglishProse(t *testing.T) {
	set := bindings.New()
	set.Fail(errs.Conflict("migrate.migration_failed", "the tell-tale developer sentence"), "")

	raw, err := json.Marshal(set.Boot.Status())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "tell-tale") {
		t.Errorf("developer prose crossed the boundary: %s", raw)
	}
}

// TestFailedBootLeavesEveryBindingGuarded: a failed start must not leave a half-usable app.
// The database has just failed a migration and may have been restored; a shell that started
// querying it is precisely what Step 0.10's ordering existed to prevent.
func TestFailedBootLeavesEveryBindingGuarded(t *testing.T) {
	set := bindings.New()
	set.Fail(errs.Conflict("migrate.migration_failed", "developer text"), "/tmp/backup.db")

	for _, p := range guardedMethods() {
		if ok, code := p.call(set); ok || code != bindings.CodeNotReady {
			t.Errorf("%s = (ok=%v, code=%q) after a failed boot; want not-ready", p.name, ok, code)
		}
	}
}

// ── the binding set ─────────────────────────────────────────────────────────────

func TestAllReturnsEveryBinding(t *testing.T) {
	set := bindings.New()
	// Asserted by PRESENCE and non-nilness, not by count.
	//
	// The count form has now broken twice on legitimate growth (Auth in 1.5, Audit in 1.6) —
	// the same maintenance tax 0.6 §11.2 removed from the migration test and 1.1 removed from
	// the module test. What matters is that every declared binding is there and bindable.
	seen := map[string]bool{}
	for _, b := range set.All() {
		if b == nil {
			t.Fatal("a nil binding was handed to Wails")
		}
		seen[reflect.TypeOf(b).Elem().Name()] = true
	}
	for _, want := range []string{"Boot", "System", "Auth", "Config", "Ops", "Money", "Audit"} {
		if !seen[want] {
			t.Errorf("binding %q is missing from All(); the frontend cannot call it", want)
		}
	}
	for i, b := range set.All() {
		if b == nil {
			t.Errorf("binding %d is nil; Wails would fail to bind it", i)
		}
	}
}
