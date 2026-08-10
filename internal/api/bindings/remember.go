package bindings

import (
	"os"
	"path/filepath"
	"strings"
)

// rememberFile is the name of the "stay signed in" token, under the data directory.
const rememberFile = "session.token"

// rememberedToken persists the session token across restarts, for users who asked to stay
// signed in (§13.1).
//
// # Why this exists at all
//
// Step 1.5 (D3) put the session token in the Go process and kept it out of the frontend, which
// is right: JavaScript in a webview must never hold a credential. But a process dies when the
// window closes, so "stay signed in" was writing a longer absolute expiry onto a session row
// that nothing could ever present again. The row survived; the ability to use it did not.
//
// Found by the Phase 1 Definition-of-Done review (1.12): DoD 3 requires a session to "survive a
// restart when 'stay signed in' is set", and it did not. The feature was a promise the code
// could not keep.
//
// # Why a file next to the database is an acceptable place for it
//
// The token is a live credential, and writing one to disk deserves a reason rather than a
// shrug. The reason is that it sits beside `mizan.db`, which holds every price, customer,
// balance, and audit row in plain SQLite. Anyone who can read this file can already read all of
// that directly — so the token adds no meaningful exposure to the threat model this
// application actually has.
//
// What it does add is convenience for an attacker who is already there: a token can be used
// through the application's own UI rather than requiring SQL. That is why it is written ONLY
// when the user opts in, removed on sign-out, and still subject to every check a live session
// faces — idle expiry, absolute expiry, and an administrator's revocation, none of which this
// file can influence.
//
// The stronger option is the OS keychain. It is not taken here because it means cgo and a
// per-platform dependency in a project whose hard constraint is that it builds and runs
// offline with pure Go. Worth revisiting if the threat model changes.
type rememberedToken struct {
	path string
}

// newRememberedToken locates the file, or returns a disabled one when there is no data
// directory (tests, and the browser-dev shell).
func newRememberedToken(dataDir string) rememberedToken {
	if dataDir == "" {
		return rememberedToken{}
	}
	return rememberedToken{path: filepath.Join(dataDir, rememberFile)}
}

// enabled reports whether there is anywhere to write.
func (r rememberedToken) enabled() bool { return r.path != "" }

// read returns the stored token, or empty.
func (r rememberedToken) read() string {
	if !r.enabled() {
		return ""
	}
	raw, err := os.ReadFile(r.path)
	if err != nil {
		// Absent is the normal case: nobody has asked to stay signed in. An unreadable file is
		// treated the same — the cost is one sign-in, and refusing to start over it would be
		// wildly out of proportion.
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// write stores a token, readable only by its owner.
func (r rememberedToken) write(token string) error {
	if !r.enabled() || token == "" {
		return nil
	}
	// 0600 rather than 0644: every other user account on the machine is outside the threat
	// model this is worth defending against, but there is no reason to hand it to them.
	return os.WriteFile(r.path, []byte(token), 0o600)
}

// clear removes the token.
//
// Called on sign-out, and on a restart that finds the stored token dead. Leaving a revoked
// token on disk would mean an administrator's "sign this device out" left its artefact behind —
// harmless, since the row is ended, but it is the kind of residue that makes a support engineer
// doubt the revocation worked.
func (r rememberedToken) clear() {
	if !r.enabled() {
		return
	}
	_ = os.Remove(r.path)
}
