package bindings_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/audit"
)

// seedAuditEntry writes one row directly.
//
// Directly, because the WRITE path is Step 1.7 — this step is about whether the READ path
// redacts, and inventing a writer here would be building 1.7 badly and early.
func seedAuditEntry(t *testing.T, app *bootstrap.App, before, after string) id.ID {
	t.Helper()
	ctx := app.Context()
	entryID, _ := id.New()
	now := clock.Format(clock.System().Now())

	_, err := app.DB.Writer(ctx).ExecContext(ctx, `
		INSERT INTO audit_log (
			id, occurred_at, actor_user_id, actor_name_snapshot, action,
			entity_type, entity_id, entity_label_snapshot,
			before_json, after_json, changed_fields, source, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ui', ?)`,
		string(entryID), now, "", "Alice Example", "identity.user.password_changed",
		"user", string(entryID), "alice",
		before, after, `["salary"]`, now)
	if err != nil {
		t.Fatalf("seeding an audit entry: %v", err)
	}
	return entryID
}

// stripPayloadPermission removes audit.entry.view_payload from every role, leaving the list
// permission intact — the Manager configuration, reproduced.
func stripPayloadPermission(t *testing.T, app *bootstrap.App) {
	t.Helper()
	ctx := app.Context()
	company, err := app.Org.Company(ctx)
	if err != nil {
		t.Fatalf("Company: %v", err)
	}
	roles, err := app.Identity.Roles(ctx, company.ID)
	if err != nil {
		t.Fatalf("Roles: %v", err)
	}
	for _, r := range roles {
		grants, grantErr := app.Identity.RoleGrants(ctx, r.ID)
		if grantErr != nil {
			t.Fatalf("RoleGrants: %v", grantErr)
		}
		for _, g := range grants {
			// The administrator's "*" covers the payload permission too, so it has to go.
			if g == audit.PermViewPayload || g == "*" {
				if err = app.Identity.RevokeFromRole(ctx, r.ID, g); err != nil {
					t.Fatalf("RevokeFromRole: %v", err)
				}
			}
		}
		// Keep the list permission so the method itself is still reachable.
		if err = app.Identity.GrantToRole(ctx, r.ID, audit.PermView); err != nil {
			t.Fatalf("GrantToRole: %v", err)
		}
	}
}

// TestRestrictedPayloadKeysAreAbsentNotBlank is the requirement of this step.
//
// Asserted on the MARSHALLED BYTES, not on struct fields — a struct assertion passes just as
// happily for a blanked field, which is precisely the design being rejected. What matters is
// that the JSON object the frontend receives has no such key at all.
func TestRestrictedPayloadKeysAreAbsentNotBlank(t *testing.T) {
	set, app := signedIn(t)
	seedAuditEntry(t, app, `{"salary":50000}`, `{"salary":75000}`)
	stripPayloadPermission(t, app)

	result := set.Audit.Entries(bindings.AuditFilterDTO{})
	if !result.OK {
		t.Fatalf("Entries failed: %+v", result.Error)
	}
	if len(result.Data) != 1 {
		t.Fatalf("entries = %d, want 1", len(result.Data))
	}

	raw, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	payload := string(raw)

	// The value must not be there...
	for _, secret := range []string{"50000", "75000", "salary"} {
		if strings.Contains(payload, secret) {
			t.Errorf("a restricted value crossed the boundary (%q): %s", secret, payload)
		}
	}
	// ...and neither must the KEY. A present-but-empty key is indistinguishable from an entry
	// that genuinely had no before-state, so the user cannot tell "nothing here" from
	// "something here you may not see".
	for _, key := range []string{"beforeJson", "afterJson", "changedFields"} {
		if strings.Contains(payload, key) {
			t.Errorf("the %q key is present for a caller without the permission — "+
				"it must be ABSENT, not blank: %s", key, payload)
		}
	}

	// The rest of the row is still there: the FACT of the change is ordinary operational
	// information, and hiding it would make the list useless to the manager who needs it.
	if !strings.Contains(payload, "identity.user.password_changed") {
		t.Errorf("the action was redacted along with the payload: %s", payload)
	}
	if !strings.Contains(payload, "Alice Example") {
		t.Errorf("the actor snapshot was redacted along with the payload: %s", payload)
	}
}

// TestPayloadIsPresentWithThePermission is the positive half.
func TestPayloadIsPresentWithThePermission(t *testing.T) {
	set, app := signedIn(t) // the administrator holds "*"
	seedAuditEntry(t, app, `{"salary":50000}`, `{"salary":75000}`)

	result := set.Audit.Entries(bindings.AuditFilterDTO{})
	if !result.OK {
		t.Fatalf("Entries failed: %+v", result.Error)
	}
	entry := result.Data[0]
	if entry.BeforeJSON == nil || *entry.BeforeJSON != `{"salary":50000}` {
		t.Errorf("beforeJson = %v, want the stored payload", entry.BeforeJSON)
	}
	if entry.AfterJSON == nil || *entry.AfterJSON != `{"salary":75000}` {
		t.Errorf("afterJson = %v", entry.AfterJSON)
	}
	if entry.ChangedFields == nil {
		t.Error("changedFields was redacted for a caller who holds the permission")
	}
}

// TestTheListItselfIsGated: without audit.entry.view the 1.5 guard refuses the method before
// redaction is ever reached. Method-level and field-level gating are different questions.
func TestTheListItselfIsGated(t *testing.T) {
	set, app := signedIn(t)
	seedAuditEntry(t, app, "{}", "{}")

	ctx := app.Context()
	company, _ := app.Org.Company(ctx)
	roles, _ := app.Identity.Roles(ctx, company.ID)
	for _, r := range roles {
		grants, _ := app.Identity.RoleGrants(ctx, r.ID)
		for _, g := range grants {
			_ = app.Identity.RevokeFromRole(ctx, r.ID, g)
		}
	}

	result := set.Audit.Entries(bindings.AuditFilterDTO{})
	if result.OK {
		t.Fatal("the audit list was readable with no permissions at all")
	}
	if result.Error.Code != bindings.CodeForbidden {
		t.Errorf("code = %q, want %q", result.Error.Code, bindings.CodeForbidden)
	}
}

// TestForgettingRedactionHidesRatherThanLeaks is the D1 property, asserted directly.
//
// A DTO built WITHOUT calling redact.Visible — which is what a future code path that forgets
// would produce — serialises with the field absent. The failure mode of carelessness is a
// missing field someone reports as a bug, not a leaked one nobody notices.
//
// Mutation check: change the DTO field to a plain string and this fails, because a blank key is
// still a present key.
func TestForgettingRedactionHidesRatherThanLeaks(t *testing.T) {
	// Deliberately constructed by hand, with no redaction call at all.
	forgotten := bindings.AuditEntryDTO{
		ID:     "x",
		Action: "something.happened",
	}

	raw, err := json.Marshal(forgotten)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"beforeJson", "afterJson", "changedFields"} {
		if strings.Contains(string(raw), key) {
			t.Errorf("a DTO built without any redaction call still emits %q — "+
				"the safe state is not the default, so forgetting the call would LEAK: %s",
				key, raw)
		}
	}
}

// TestEntriesAreNewestFirst pins the ordering the list screen depends on.
func TestEntriesAreNewestFirst(t *testing.T) {
	set, app := signedIn(t)
	seedAuditEntry(t, app, `{"n":1}`, `{"n":1}`)
	seedAuditEntry(t, app, `{"n":2}`, `{"n":2}`)

	result := set.Audit.Entries(bindings.AuditFilterDTO{})
	if !result.OK || len(result.Data) != 2 {
		t.Fatalf("entries = %+v", result)
	}
	if result.Data[0].OccurredAt < result.Data[1].OccurredAt {
		t.Error("entries are not newest-first")
	}
}

// TestAuditReadPathHasNoWriteOrDelete documents the append-only guarantee (§15.1).
//
// Enforced by there being no method rather than by a comment — this asserts the surface stays
// that way as the module grows.
func TestAuditReadPathHasNoWriteOrDelete(t *testing.T) {
	_, app := signedIn(t)

	// A direct delete must be the ONLY way to remove a row, and nothing in the module offers
	// one. This is a documentation test: it fails to compile if someone adds Delete/Update to
	// the Service, which is the moment to have the conversation.
	var svc any = app.Audit
	if _, hasDelete := svc.(interface {
		Delete(context.Context, id.ID) error
	}); hasDelete {
		t.Error("the audit service exposes Delete; the trail is append-only (§15.1)")
	}
	if _, hasUpdate := svc.(interface {
		Update(context.Context, audit.Entry) error
	}); hasUpdate {
		t.Error("the audit service exposes Update; the trail is append-only (§15.1)")
	}
}
