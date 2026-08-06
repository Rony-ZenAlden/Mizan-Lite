package bindings_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/appctx"
	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
)

// seededEntity is the entity type the redaction tests file their fixtures under.
//
// A type of their own, so a query can pick them out from the entries the fixture's own
// provisioning and sign-in now write (1.7). Filtering rather than counting: the trail is a
// shared, growing surface, and a test that assumes it holds only its own rows breaks the next
// time anything upstream starts auditing — which is exactly what happened here.
const seededEntity = "test.payload"

// seedAuditEntry writes one row through the REAL write path (1.7).
//
// Step 1.6 wrote it with raw SQL because no write path existed. Now one does, and going through
// it means these tests exercise the subscriber, the actor snapshot, and the transaction — a
// fixture built by hand would keep passing after the real path broke.
//
// The payload is invented (a salary change) because the property under test is redaction, and
// a value nobody should see needs to be a value worth not seeing.
func seedAuditEntry(t *testing.T, app *bootstrap.App, before, after string) {
	t.Helper()

	// Stamped exactly as the 1.5 guard stamps it, which also exercises the composition root's
	// ActorResolver adapter: the snapshot below is only correct if that adapter reads the
	// context the guard writes.
	actorID, err := id.New()
	if err != nil {
		t.Fatalf("id: %v", err)
	}
	ctx := appctx.WithActor(app.Context(), appctx.Actor{
		UserID: actorID, DisplayName: "Alice Example",
	})

	err = app.DB.Do(ctx, func(ctx context.Context) error {
		return app.Bus.Publish(ctx, auditc.Auditable{
			Action:      "identity.user.password_changed",
			EntityType:  seededEntity,
			EntityLabel: "alice",
			Before:      json.RawMessage(before),
			After:       json.RawMessage(after),
			Changed:     []string{"salary"},
			Source:      auditc.SourceUI,
		})
	})
	if err != nil {
		t.Fatalf("seeding an audit entry: %v", err)
	}
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

	result := set.Audit.Entries(bindings.AuditFilterDTO{EntityType: seededEntity})
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

	result := set.Audit.Entries(bindings.AuditFilterDTO{EntityType: seededEntity})
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

	result := set.Audit.Entries(bindings.AuditFilterDTO{EntityType: seededEntity})
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
