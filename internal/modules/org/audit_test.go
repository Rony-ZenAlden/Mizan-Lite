package org_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/modules/audit"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/modules/org"
)

// Setting up an installation is the FIRST entry in the trail, and it has no actor: the wizard
// runs before any user exists. The entry must still be written, and must say so honestly.
func TestProvisioningIsAudited(t *testing.T) {
	svc, store := newService(t)
	seedCurrency(t, store, "SYP")
	ctx := context.Background()

	result, err := svc.Provision(ctx, org.ProvisionInput{
		Company: org.CompanyInput{
			Code: "MAIN", Name: "Demo", CountryCode: "SY", FunctionalCurrency: "SYP",
		},
		Branch:               org.LocationInput{Code: "HQ", Name: "Head Office"},
		Warehouse:            org.LocationInput{Code: "WH1", Name: "Main"},
		FiscalYearStartMonth: time.January,
		FiscalYearStartYear:  2026,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	entries, err := auditSvc.Entries(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d entries, want exactly 1", len(entries))
	}
	entry := entries[0]

	if entry.Action != org.ActionCompanyProvisioned {
		t.Errorf("action = %q, want %q", entry.Action, org.ActionCompanyProvisioned)
	}
	if entry.EntityID != result.CompanyID {
		t.Errorf("entity id = %q, want the company %q", entry.EntityID, result.CompanyID)
	}
	if !entry.ActorUserID.IsZero() {
		t.Errorf("actor = %q, want none: the wizard runs before any user exists", entry.ActorUserID)
	}
	if entry.Source != auditc.SourceSystem {
		t.Errorf("source = %q, want %q", entry.Source, auditc.SourceSystem)
	}
	if !strings.Contains(entry.AfterJSON, `"companyCode":"MAIN"`) {
		t.Errorf("payload = %q, want the organisation that was created", entry.AfterJSON)
	}
}

// A failed provision leaves neither a company nor a claim that one was created.
//
// The composite guarantee: everything in Provision is one transaction, and the audit entry is
// in it too. An entry for a company that does not exist would be worse than no entry.
func TestFailedProvisionLeavesNoAuditEntry(t *testing.T) {
	svc, store := newService(t)
	seedCurrency(t, store, "SYP")
	ctx := context.Background()

	// No such currency: the foreign key rejects the company, part-way through the transaction.
	if _, err := svc.Provision(ctx, org.ProvisionInput{
		Company: org.CompanyInput{
			Code: "MAIN", Name: "Demo", CountryCode: "SY", FunctionalCurrency: "XXX",
		},
		Branch:               org.LocationInput{Code: "HQ", Name: "Head Office"},
		Warehouse:            org.LocationInput{Code: "WH1", Name: "Main"},
		FiscalYearStartMonth: time.January,
		FiscalYearStartYear:  2026,
	}); err == nil {
		t.Fatal("Provision succeeded with an unknown currency")
	}

	entries, err := auditSvc.Entries(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("%d audit entries survived a failed provision", len(entries))
	}
}
