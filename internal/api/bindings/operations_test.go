package bindings_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/modules/audit"
)

// TestEveryDestructiveOperationLeavesAnAuditEntry
//
// # DoD criterion 11, and nothing tested it
//
// A restore replaces the shop's data with an older copy. It is the single most destructive act
// the application offers, and "who did this, and when" is a question somebody will need answered.
//
// An import creates records in bulk. The ROWS are audited by the services that create them — that
// is what 9.4's design is for — but "four thousand products arrived at once, from a file, at
// somebody's instruction" is a fact those four thousand entries do not carry.
func TestEveryDestructiveOperationLeavesAnAuditEntry(t *testing.T) {
	set, app := signedInAdmin(t)
	ctx := app.Context()

	// A backup to restore from.
	taken := set.Operations.TakeBackup()
	if !taken.OK {
		t.Fatalf("TakeBackup: %+v", taken.Error)
	}

	prepared := set.Operations.PrepareRestore(taken.Data.Name)
	if !prepared.OK {
		t.Fatalf("PrepareRestore: %+v", prepared.Error)
	}
	cancelled := set.Operations.CancelRestore()
	if !cancelled.OK {
		t.Fatalf("CancelRestore: %+v", cancelled.Error)
	}

	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	imported := set.Operations.ImportProducts(
		base64.StdEncoding.EncodeToString([]byte("code,name,unit\nOIL-1,Olive oil,PCS\n")),
		false)
	if !imported.OK {
		t.Fatalf("ImportProducts: %+v", imported.Error)
	}

	entries, err := app.Audit.Entries(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry.Action] = true
	}

	for _, action := range []string{
		"ops.restore.prepared", "ops.restore.cancelled", "ops.import.completed",
	} {
		if !seen[action] {
			t.Errorf("%q happened and left no audit entry", action)
		}
	}

	// The prepared entry NAMES the safety snapshot, which is what makes it useful rather than
	// merely present: an auditor reading "a restore was prepared" wants to know what the shop
	// could go back to.
	var payload string
	for _, entry := range entries {
		if entry.Action == "ops.restore.prepared" {
			payload = entry.AfterJSON
		}
	}
	if !strings.Contains(payload, "safety_backup") {
		t.Errorf("the prepared-restore entry does not name the safety snapshot: %q", payload)
	}
}

// TestADryRunIsNotAudited
//
// It writes nothing and reads a file the user chose. Auditing it would fill the trail with
// entries about decisions nobody made.
func TestADryRunIsNotAudited(t *testing.T) {
	set, app := signedInAdmin(t)
	ctx := app.Context()

	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	checked := set.Operations.ImportProducts(
		base64.StdEncoding.EncodeToString([]byte("code,name,unit\nOIL-1,Olive oil,PCS\n")),
		true)
	if !checked.OK {
		t.Fatalf("ImportProducts: %+v", checked.Error)
	}

	entries, err := app.Audit.Entries(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	for _, entry := range entries {
		if entry.Action == "ops.import.completed" {
			t.Error("a dry run was audited as a completed import")
		}
	}
}
