package identity_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
)

// ── the guarantee: an entry is written, with everything needed to read it later ──

func TestAuditRecordsAUserBeingCreated(t *testing.T) {
	f := newAuditedFixture(t, clock.System())
	ctx := actingAs(t, bound(t, f.store), "n. hassan")

	user, err := f.identity.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: f.companyID, Username: "amina", DisplayName: "Amina", Password: goodPassword,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	entry := onlyEntry(t, f, identity.ActionUserCreated)

	if entry.EntityID != user.ID {
		t.Errorf("entity id = %q, want %q", entry.EntityID, user.ID)
	}
	if entry.EntityLabel != "amina" {
		t.Errorf("entity label = %q, want the username", entry.EntityLabel)
	}
	if entry.ActorName != "n. hassan" {
		t.Errorf("actor snapshot = %q, want the display name from the context", entry.ActorName)
	}
	if entry.Source != auditc.SourceUI {
		t.Errorf("source = %q, want %q", entry.Source, auditc.SourceUI)
	}
	if entry.Correlation.IsZero() {
		t.Error("no correlation id: entries from one action could not be tied together")
	}
	if !strings.Contains(entry.AfterJSON, `"username":"amina"`) {
		t.Errorf("after payload = %q, want the created user", entry.AfterJSON)
	}
}

// A password must never reach the trail. It is the one thing in scope at the write site that
// would turn an audit row into a credential store, so it is asserted rather than assumed.
func TestAuditNeverRecordsACredential(t *testing.T) {
	f := newAuditedFixture(t, clock.System())
	ctx := actingAs(t, bound(t, f.store), "admin")

	user, err := f.identity.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: f.companyID, Username: "amina", DisplayName: "Amina", Password: goodPassword,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err = f.identity.ResetPassword(ctx, user.ID, "another entirely different passphrase"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}

	for _, entry := range allEntries(t, f) {
		payload := entry.BeforeJSON + entry.AfterJSON
		for _, forbidden := range []string{goodPassword, "another entirely different passphrase", "$argon2"} {
			if strings.Contains(payload, forbidden) {
				t.Fatalf("action %q leaked %q into the audit trail", entry.Action, forbidden)
			}
		}
	}

	changed := onlyEntry(t, f, identity.ActionPasswordReset)
	if changed.BeforeJSON != "" || changed.AfterJSON != "" {
		t.Errorf("password change carries a payload (%q/%q); it should carry only the fact",
			changed.BeforeJSON, changed.AfterJSON)
	}
	if changed.ChangedFields != `["password"]` {
		t.Errorf("changed fields = %q, want the field name", changed.ChangedFields)
	}
}

// ── the atomicity drill (D7) ────────────────────────────────────────────────────

// A business operation that fails AFTER its audit entry was published leaves no entry.
//
// The drill that proves the entry is written through the caller's transaction rather than on a
// connection of its own. Publishing outside the Unit of Work would leave an audit row claiming
// a user was created who does not exist — a trail that is worse than no trail, because it is
// confidently wrong.
func TestAuditEntryRollsBackWithTheChange(t *testing.T) {
	f := newAuditedFixture(t, clock.System())
	ctx := actingAs(t, bound(t, f.store), "admin")

	wanted := errBusinessRuleFailed{}
	err := f.store.Do(ctx, func(ctx context.Context) error {
		if _, createErr := f.identity.CreateUser(ctx, identity.CreateUserInput{
			CompanyID: f.companyID, Username: "amina", DisplayName: "Amina", Password: goodPassword,
		}); createErr != nil {
			return createErr
		}
		// Something later in the same unit of work fails — a stock check, a period lock,
		// anything. The user must not exist, and neither must its audit entry.
		return wanted
	})
	if err != wanted {
		t.Fatalf("Do = %v, want the business failure to propagate", err)
	}

	users, err := f.identity.Users(context.Background(), f.companyID)
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	for _, u := range users {
		if u.Username == "amina" {
			t.Fatal("the user survived a rolled-back transaction")
		}
	}
	if entries := allEntries(t, f); len(entries) != 0 {
		t.Fatalf("%d audit entries survived a rolled-back transaction: %v",
			len(entries), actions(entries))
	}
}

// A failed audit write aborts the business change (D3).
//
// The audit table is removed, so the subscriber's INSERT genuinely fails — no fake, no stub.
// The password change must not take effect, and the old password must still work.
func TestFailedAuditWriteAbortsTheOperation(t *testing.T) {
	f := newAuditedFixture(t, clock.System())
	ctx := actingAs(t, bound(t, f.store), "admin")

	user, err := f.identity.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: f.companyID, Username: "amina", DisplayName: "Amina", Password: goodPassword,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if _, err = f.store.Writer(ctx).ExecContext(ctx, `DROP TABLE audit_log`); err != nil {
		t.Fatalf("dropping the audit table: %v", err)
	}

	const newPassword = "a completely different passphrase"
	if err = f.identity.ResetPassword(ctx, user.ID, newPassword); err == nil {
		t.Fatal("ResetPassword succeeded although the audit write failed")
	}

	// The real assertion. An error returned while the change committed anyway would be the
	// worst of both worlds: the caller told it failed, the password changed regardless.
	if _, err = f.identity.Authenticate(ctx, "amina", goodPassword); err != nil {
		t.Errorf("the old password no longer authenticates: the change committed despite the "+
			"audit failure (%v)", err)
	}
	if _, err = f.identity.Authenticate(ctx, "amina", newPassword); err == nil {
		t.Error("the new password authenticates: the change committed despite the audit failure")
	}
}

// A panicking subscriber aborts too, rather than being swallowed (0.6 §3.3).
func TestPanickingSubscriberAbortsTheOperation(t *testing.T) {
	f := newAuditedFixture(t, clock.System())
	ctx := actingAs(t, bound(t, f.store), "admin")

	if err := eventbus.Subscribe(f.bus, "test.panics",
		func(context.Context, auditc.Auditable) error { panic("the subscriber is broken") },
	); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if _, err := f.identity.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: f.companyID, Username: "amina", DisplayName: "Amina", Password: goodPassword,
	}); err == nil {
		t.Fatal("CreateUser succeeded although a subscriber panicked")
	}

	users, err := f.identity.Users(context.Background(), f.companyID)
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("the user survived a panicking subscriber: %d rows", len(users))
	}
}

// ── the actor ───────────────────────────────────────────────────────────────────

// With nobody acting, the entry is unattributed and marked as the system's doing — honest,
// where a fabricated actor would not be. This is the setup wizard's case.
func TestAuditWithoutAnActorRecordsTheSystem(t *testing.T) {
	f := newAuditedFixture(t, clock.System())
	ctx := bound(t, f.store) // deliberately no actor

	if _, err := f.identity.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: f.companyID, Username: "amina", DisplayName: "Amina", Password: goodPassword,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	entry := onlyEntry(t, f, identity.ActionUserCreated)
	if !entry.ActorUserID.IsZero() {
		t.Errorf("actor = %q, want none", entry.ActorUserID)
	}
	if entry.Source != auditc.SourceSystem {
		t.Errorf("source = %q, want %q", entry.Source, auditc.SourceSystem)
	}
}

// Two entries from ONE user action share a correlation id, which is what makes "what did this
// click actually do?" answerable.
func TestAuditTiesOneActionsEntriesTogether(t *testing.T) {
	f := newAuditedFixture(t, clock.System())
	ctx := actingAs(t, bound(t, f.store), "admin")

	err := f.store.Do(ctx, func(ctx context.Context) error {
		for _, username := range []string{"amina", "karim"} {
			if _, createErr := f.identity.CreateUser(ctx, identity.CreateUserInput{
				CompanyID: f.companyID, Username: username, DisplayName: username,
				Password: goodPassword,
			}); createErr != nil {
				return createErr
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	entries := allEntries(t, f)
	if len(entries) != 2 {
		t.Fatalf("%d entries, want 2: %v", len(entries), actions(entries))
	}
	if entries[0].Correlation != entries[1].Correlation {
		t.Errorf("correlation ids differ (%q, %q): the two entries cannot be tied to one action",
			entries[0].Correlation, entries[1].Correlation)
	}
}

// ── what is NOT audited ─────────────────────────────────────────────────────────

// Reads write nothing (D6). Auditing a list screen would put a row in the table per page view
// and make it useless for the question it exists to answer.
func TestReadsAreNotAudited(t *testing.T) {
	f := newAuditedFixture(t, clock.System())
	ctx := actingAs(t, bound(t, f.store), "admin")

	if _, err := f.identity.Users(ctx, f.companyID); err != nil {
		t.Fatalf("Users: %v", err)
	}
	if _, err := f.identity.Roles(ctx, f.companyID); err != nil {
		t.Fatalf("Roles: %v", err)
	}
	if entries := allEntries(t, f); len(entries) != 0 {
		t.Fatalf("reading wrote %d audit entries: %v", len(entries), actions(entries))
	}
}

// ── sign-in ─────────────────────────────────────────────────────────────────────

func TestAuditRecordsSignIn(t *testing.T) {
	f := newAuditedFixture(t, clock.System())
	ctx := bound(t, f.store)

	if _, err := f.identity.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: f.companyID, Username: "amina", DisplayName: "Amina", Password: goodPassword,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if _, err := f.identity.Login(ctx, identity.LoginInput{
		Username: "amina", Password: "not the password",
	}); err == nil {
		t.Fatal("a wrong password logged in")
	}
	result, err := f.identity.Login(ctx, identity.LoginInput{
		Username: "amina", Password: goodPassword,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	failed := onlyEntry(t, f, identity.ActionLoginFailed)
	// The reason IS recorded here, although Authenticate refuses to expose it to the caller:
	// an administrator reading the trail is entitled to the distinction the login screen is not.
	if !strings.Contains(failed.AfterJSON, `"reason"`) {
		t.Errorf("failed login records no reason: %q", failed.AfterJSON)
	}
	if strings.Contains(failed.AfterJSON, "not the password") {
		t.Errorf("the attempted password reached the trail: %q", failed.AfterJSON)
	}

	succeeded := onlyEntry(t, f, identity.ActionLoginSucceeded)
	if succeeded.EntityID != result.Session.ID {
		t.Errorf("entity id = %q, want the session %q", succeeded.EntityID, result.Session.ID)
	}
	if strings.Contains(succeeded.AfterJSON, result.Token) {
		t.Error("the session token reached the audit trail")
	}
}

// ── payloads ────────────────────────────────────────────────────────────────────

// A creation has no before state, and that must be storable as "absent" rather than as the
// four characters "null" — otherwise a viewer cannot tell "nothing was there" from "the value
// was null".
func TestAbsentPayloadIsStoredAsNull(t *testing.T) {
	f := newAuditedFixture(t, clock.System())
	ctx := actingAs(t, bound(t, f.store), "admin")

	if _, err := f.identity.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: f.companyID, Username: "amina", DisplayName: "Amina", Password: goodPassword,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	var beforeIsNull int
	if err := f.store.Reader(ctx).QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE action = ? AND before_json IS NULL`,
		identity.ActionUserCreated).Scan(&beforeIsNull); err != nil {
		t.Fatalf("query: %v", err)
	}
	if beforeIsNull != 1 {
		t.Errorf("before_json is not SQL NULL for a creation (%d rows)", beforeIsNull)
	}

	entry := onlyEntry(t, f, identity.ActionUserCreated)
	if !json.Valid([]byte(entry.AfterJSON)) {
		t.Errorf("after payload is not valid JSON: %q", entry.AfterJSON)
	}
}

// ── the wiring itself ───────────────────────────────────────────────────────────

// A service built without a publisher must fail at the first audited write, loudly.
//
// The alternative — recording nothing — would produce an empty trail in exactly the deployment
// where someone is relying on it, and nothing would ever say so.
func TestServiceWithoutAPublisherRefusesToWrite(t *testing.T) {
	f := newAuditedFixture(t, clock.System())
	ctx := bound(t, f.store)

	unwired := identity.NewService(f.store, stubOrg{f.companyID}, clock.System(), nil, nil)
	if _, err := unwired.CreateUser(ctx, identity.CreateUserInput{
		CompanyID: f.companyID, Username: "amina", DisplayName: "Amina", Password: goodPassword,
	}); err == nil {
		t.Fatal("a service with no publisher created a user, recording nothing")
	}
}

type stubOrg struct{ companyID id.ID }

func (s stubOrg) CurrentCompanyID(context.Context) (id.ID, error) { return s.companyID, nil }
func (s stubOrg) DefaultBranchID(context.Context) (id.ID, error)  { return id.ID(""), nil }

// ── helpers ─────────────────────────────────────────────────────────────────────

type errBusinessRuleFailed struct{}

func (errBusinessRuleFailed) Error() string { return "a business rule failed" }

// actingAs stamps an actor and a correlation id, as the binding guard does at runtime.
func actingAs(t *testing.T, ctx context.Context, displayName string) context.Context {
	t.Helper()
	userID, err := id.New()
	if err != nil {
		t.Fatalf("id: %v", err)
	}
	correlation, err := id.New()
	if err != nil {
		t.Fatalf("id: %v", err)
	}
	ctx = withActor(ctx, audit.Actor{UserID: userID, DisplayName: displayName})
	return event.WithCorrelationID(ctx, correlation)
}

func allEntries(t *testing.T, f fixture) []audit.Entry {
	t.Helper()
	entries, err := f.audit.Entries(context.Background(), audit.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	return entries
}

// onlyEntry returns the one entry for an action, failing if there is not exactly one — which
// also catches a subscriber registered twice, a class of bug that otherwise looks like success.
func onlyEntry(t *testing.T, f fixture, action string) audit.Entry {
	t.Helper()
	var found []audit.Entry
	for _, e := range allEntries(t, f) {
		if e.Action == action {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d entries for %q, want exactly 1", len(found), action)
	}
	return found[0]
}

func actions(entries []audit.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Action)
	}
	return out
}
