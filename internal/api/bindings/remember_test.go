package bindings_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/platform/paths"
)

// "Stay signed in" must survive the process, not just the session row.
//
// # The defect this pins (1.12)
//
// Step 1.3 gave a remembered session a longer ABSOLUTE window, and 1.5 (D3) put the token in
// the Go process so JavaScript could never hold a credential. Both were right on their own, and
// together they made §13.1's "stay signed in" a promise the code could not keep: the row
// survived a restart, and the only thing that could present it died with the window.
//
// Nothing caught it for four steps because no test had ever restarted the application.
func TestStaySignedInSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(paths.EnvOverride, dir)

	first := attachedSet(t, dir)
	if result := first.Setup.Apply(validInput()); !result.OK {
		t.Fatalf("Apply: %+v", result.Error)
	}
	if login := first.Auth.Login("nadia", adminPassword, true); !login.OK {
		t.Fatalf("Login: %+v", login.Error)
	}

	// A NEW binding set over a NEW graph, reading the same data directory: a restart, as far as
	// anything in the process is concerned.
	second := attachedSet(t, dir)

	me := second.Auth.Me()
	if !me.OK {
		t.Fatalf("Me: %+v", me.Error)
	}
	if !me.Data.SignedIn {
		t.Fatal("a user who asked to stay signed in was shown the login screen after a restart")
	}
	if me.Data.Username != "nadia" {
		t.Errorf("username = %q, want the remembered user", me.Data.Username)
	}
}

// Without the opt-in, nothing is stored. "Stay signed in" is a choice, and a machine on a shop
// counter must not remember someone who did not ask it to.
func TestSigningInWithoutRememberStoresNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(paths.EnvOverride, dir)

	first := attachedSet(t, dir)
	if result := first.Setup.Apply(validInput()); !result.OK {
		t.Fatalf("Apply: %+v", result.Error)
	}
	if login := first.Auth.Login("nadia", adminPassword, false); !login.OK {
		t.Fatalf("Login: %+v", login.Error)
	}

	if _, err := os.Stat(filepath.Join(dir, "session.token")); !os.IsNotExist(err) {
		t.Fatal("a token was stored although the user did not ask to stay signed in")
	}
	if me := attachedSet(t, dir).Auth.Me(); me.Data.SignedIn {
		t.Error("the next launch was signed in anyway")
	}
}

// Signing out must leave nothing behind that could sign this machine back in.
func TestSigningOutForgetsTheRememberedSession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(paths.EnvOverride, dir)

	first := attachedSet(t, dir)
	if result := first.Setup.Apply(validInput()); !result.OK {
		t.Fatalf("Apply: %+v", result.Error)
	}
	if login := first.Auth.Login("nadia", adminPassword, true); !login.OK {
		t.Fatalf("Login: %+v", login.Error)
	}
	if logout := first.Auth.Logout(); !logout.OK {
		t.Fatalf("Logout: %+v", logout.Error)
	}

	if _, err := os.Stat(filepath.Join(dir, "session.token")); !os.IsNotExist(err) {
		t.Error("the remembered token outlived a deliberate sign-out")
	}
	if me := attachedSet(t, dir).Auth.Me(); me.Data.SignedIn {
		t.Error("the next launch was signed in after a sign-out")
	}
}

// An administrator's revocation must beat the file.
//
// The stored token is a convenience, never an authority: "sign this device out" has to work
// from another machine, and it does — the row is ended, so the restore validates and discards.
func TestARevokedSessionIsNotRestored(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(paths.EnvOverride, dir)

	first := attachedSet(t, dir)
	if result := first.Setup.Apply(validInput()); !result.OK {
		t.Fatalf("Apply: %+v", result.Error)
	}
	if login := first.Auth.Login("nadia", adminPassword, true); !login.OK {
		t.Fatalf("Login: %+v", login.Error)
	}

	sessions := first.Identity.Sessions()
	if !sessions.OK || len(sessions.Data) == 0 {
		t.Fatalf("Sessions: %+v", sessions)
	}
	if revoke := first.Identity.RevokeSession(sessions.Data[0].ID); !revoke.OK {
		t.Fatalf("RevokeSession: %+v", revoke.Error)
	}

	second := attachedSet(t, dir)
	if me := second.Auth.Me(); me.Data.SignedIn {
		t.Fatal("a revoked session was restored from disk")
	}
	// And the dead token is gone, rather than left to fail on every future call.
	if _, err := os.Stat(filepath.Join(dir, "session.token")); !os.IsNotExist(err) {
		t.Error("a revoked token was left on disk")
	}
}

// attachedSet boots a graph over dir and attaches a fresh binding set — one application launch.
func attachedSet(t *testing.T, dir string) *bindings.Set {
	t.Helper()
	resolved, err := paths.Resolve("Mizan")
	if err != nil {
		t.Fatalf("paths.Resolve: %v", err)
	}
	app, err := bootstrap.Start(context.Background(), bootstrap.Options{
		Paths: resolved, SkipBackup: true,
	})
	if err != nil {
		t.Fatalf("bootstrap.Start: %v", err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })

	set := bindings.New()
	set.Attach(app)
	return set
}
