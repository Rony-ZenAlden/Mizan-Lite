package org_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/modules/org/domain"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/migrations"
)

// ── harness ─────────────────────────────────────────────────────────────────────

// newService builds a service over a real, fully migrated database.
//
// Against the REAL merged schema — 0001–0003 plus org's 0004 — so the foreign key to
// currencies(code), the CHECK constraints, and the unique indexes are genuinely exercised
// rather than mocked.
func newService(t *testing.T) (*org.Service, *database.Store) {
	t.Helper()

	// One path, used for both the pool and the runner. t.TempDir() returns a NEW directory on
	// every call, so naming it once is the difference between migrating the database under
	// test and migrating a different, empty one.
	path := filepath.Join(t.TempDir(), "test.db")

	store, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// The real merged set, assembled exactly as the composition root assembles it: platform
	// 0001–0002, currency 0003, org 0004. Anything less would not exercise the foreign key
	// from companies.functional_currency to currencies(code).
	merged := migrate.Merge(
		migrations.SQLite(),
		currency.NewModule(nil).Migrations(),
		org.NewModule(nil).Migrations(),
	)
	runner, err := migrate.New(store, migrate.Options{
		FS: merged, DBPath: path, SkipBackup: true,
	})
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	if _, err = runner.Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return org.NewService(store, clock.System()), store
}

func seedCurrency(t *testing.T, store *database.Store, code string) {
	t.Helper()
	ctx := context.Background()
	identifier, _ := id.New()
	now := clock.Format(clock.System().Now())
	_, err := store.Writer(ctx).ExecContext(ctx,
		`INSERT INTO currencies (id, code, name, symbol, decimal_places, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(identifier), code, code, code, 2, now, now)
	if err != nil {
		t.Fatalf("seeding currency %s: %v", code, err)
	}
}

func validInput() org.ProvisionInput {
	return org.ProvisionInput{
		Company: org.CompanyInput{
			Code: "MAIN", Name: "Mizan Demo", CountryCode: "SY",
			FunctionalCurrency: "SYP", PricingCurrency: "USD",
		},
		Branch:               org.LocationInput{Code: "HQ", Name: "Head Office"},
		Warehouse:            org.LocationInput{Code: "WH1", Name: "Main Warehouse"},
		FiscalYearStartMonth: time.January,
		FiscalYearStartYear:  2026,
	}
}

// ── provisioning ────────────────────────────────────────────────────────────────

func TestProvisionCreatesTheWholeSpine(t *testing.T) {
	svc, store := newService(t)
	ctx := context.Background()
	seedCurrency(t, store, "SYP")
	seedCurrency(t, store, "USD")

	provisioned, err := svc.IsProvisioned(ctx)
	if err != nil || provisioned {
		t.Fatalf("a fresh database reported provisioned=%v (err=%v)", provisioned, err)
	}

	result, err := svc.Provision(ctx, validInput())
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if result.CompanyID.IsZero() || result.BranchID.IsZero() ||
		result.WarehouseID.IsZero() || result.FiscalYearID.IsZero() {
		t.Fatalf("Provision returned incomplete ids: %+v", result)
	}

	if provisioned, err = svc.IsProvisioned(ctx); err != nil || !provisioned {
		t.Errorf("after Provision, provisioned=%v (err=%v)", provisioned, err)
	}

	company, err := svc.Company(ctx)
	if err != nil {
		t.Fatalf("Company: %v", err)
	}
	if company.Name != "Mizan Demo" || company.CountryCode != "SY" {
		t.Errorf("company = %+v", company)
	}

	branch, err := svc.DefaultBranch(ctx)
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if !branch.IsDefault || branch.Code != "HQ" {
		t.Errorf("default branch = %+v", branch)
	}

	warehouses, err := svc.Warehouses(ctx, branch.ID)
	if err != nil || len(warehouses) != 1 || !warehouses[0].IsDefault {
		t.Errorf("warehouses = %+v (err=%v)", warehouses, err)
	}

	years, err := svc.FiscalYears(ctx)
	if err != nil || len(years) != 1 {
		t.Fatalf("fiscal years = %d (err=%v)", len(years), err)
	}
	if len(years[0].Periods) != domain.PeriodsPerYear {
		t.Errorf("periods = %d, want %d", len(years[0].Periods), domain.PeriodsPerYear)
	}
	if !years[0].Tiles() {
		t.Error("the persisted fiscal year does not tile — a posting could land in no period")
	}
}

// TestProvisionIsRejectedTwice is the one-company invariant.
//
// It lives in the SERVICE, not only on the Setup binding (§5.3): the binding guard is about
// reachability, this one is about truth, and a second company would make every scope in the
// system ambiguous.
//
// Mutation check: remove the IsProvisioned guard from Provision and this must fail.
func TestProvisionIsRejectedTwice(t *testing.T) {
	svc, store := newService(t)
	ctx := context.Background()
	seedCurrency(t, store, "SYP")
	seedCurrency(t, store, "USD")

	if _, err := svc.Provision(ctx, validInput()); err != nil {
		t.Fatalf("first Provision: %v", err)
	}

	second := validInput()
	second.Company.Code = "OTHER"
	_, err := svc.Provision(ctx, second)
	if err == nil {
		t.Fatal("a second Provision succeeded; every scope is now ambiguous")
	}
	if code := errs.CodeOf(err); code != domain.CodeAlreadyProvisioned {
		t.Errorf("code = %q, want %q", code, domain.CodeAlreadyProvisioned)
	}
}

// TestFailedProvisionLeavesNoCompany is the atomicity guarantee: a company with no branch is
// a state no screen can recover from, so a partial apply must leave nothing at all.
func TestFailedProvisionLeavesNoCompany(t *testing.T) {
	svc, store := newService(t)
	ctx := context.Background()
	seedCurrency(t, store, "SYP")

	// The warehouse has no name, so provisioning fails AFTER the company and branch inserts.
	bad := validInput()
	bad.Company.PricingCurrency = "" // USD is not seeded here
	bad.Warehouse.Name = ""

	if _, err := svc.Provision(ctx, bad); err == nil {
		t.Fatal("an invalid Provision succeeded")
	}

	var companies int
	if err := store.Reader(ctx).QueryRowContext(ctx,
		`SELECT COUNT(*) FROM companies`).Scan(&companies); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if companies != 0 {
		t.Errorf("companies = %d after a failed provision, want 0 — the transaction leaked",
			companies)
	}
	if provisioned, _ := svc.IsProvisioned(ctx); provisioned {
		t.Error("IsProvisioned is true after a failed provision")
	}
}

func TestProvisionRejectsAnUnknownCurrency(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	// No currency seeded at all: the foreign key must refuse the company.

	if _, err := svc.Provision(ctx, validInput()); err == nil {
		t.Fatal("a company naming a currency that does not exist was accepted")
	}
}

// ── reads before setup ──────────────────────────────────────────────────────────

func TestCompanyBeforeSetupIsATypedNotFound(t *testing.T) {
	svc, _ := newService(t)

	_, err := svc.Company(context.Background())
	if err == nil {
		t.Fatal("Company succeeded on an unprovisioned database")
	}
	if !errs.IsCategory(err, errs.CategoryNotFound) {
		t.Errorf("category = %v, want NotFound", errs.CategoryOf(err))
	}
	if code := errs.CodeOf(err); code != domain.CodeNotProvisioned {
		t.Errorf("code = %q, want %q", code, domain.CodeNotProvisioned)
	}
}

// ── the last-active guards ──────────────────────────────────────────────────────

func TestTheLastActiveBranchCannotBeDeactivated(t *testing.T) {
	svc, store := newService(t)
	ctx := context.Background()
	seedCurrency(t, store, "SYP")
	seedCurrency(t, store, "USD")

	result, err := svc.Provision(ctx, validInput())
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	err = svc.SetBranchActive(ctx, result.BranchID, false)
	if err == nil {
		t.Fatal("the only branch was deactivated; the business has nowhere to trade")
	}
	if code := errs.CodeOf(err); code != domain.CodeLastActiveBranch {
		t.Errorf("code = %q, want %q", code, domain.CodeLastActiveBranch)
	}
}

func TestTheLastActiveWarehouseCannotBeDeactivated(t *testing.T) {
	svc, store := newService(t)
	ctx := context.Background()
	seedCurrency(t, store, "SYP")
	seedCurrency(t, store, "USD")

	result, err := svc.Provision(ctx, validInput())
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	err = svc.SetWarehouseActive(ctx, result.WarehouseID, result.BranchID, false)
	if err == nil {
		t.Fatal("the branch's only warehouse was deactivated")
	}
	if code := errs.CodeOf(err); code != domain.CodeLastActiveWarehouse {
		t.Errorf("code = %q, want %q", code, domain.CodeLastActiveWarehouse)
	}
}

func TestCanDeactivateWhenASiblingRemains(t *testing.T) {
	if domain.CanDeactivate(1) {
		t.Error("deactivating the last active location was permitted")
	}
	if !domain.CanDeactivate(2) {
		t.Error("deactivating one of two was refused")
	}
}

// ── module wiring ───────────────────────────────────────────────────────────────

// TestModuleDeclaresItsDependency: org's FK on currencies(code) is a real ordering constraint,
// and this is the first module to have one — the topological sort from 0.10 was previously
// correct against a single node and entirely unexercised.
func TestModuleDeclaresItsDependency(t *testing.T) {
	m := org.NewModule(nil)
	if m.Name() != "org" {
		t.Errorf("name = %q", m.Name())
	}
	deps := m.DependsOn()
	if len(deps) != 1 || deps[0] != "currency" {
		t.Errorf("DependsOn = %v, want [currency]", deps)
	}
}

// TestModuleSeedsNothing pins Step 1.1's D1.
//
// PHASE_1_CORE_DATA §SEQ said this step would seed a default company. It must not: the wizard
// creates it, and a seeded placeholder would make the wizard's "no company exists" invariant
// false before it ran. If someone later adds a seed here, this fails and points at the reason.
func TestModuleSeedsNothing(t *testing.T) {
	m := org.NewModule(nil)
	if specs := m.Metadata(); len(specs) != 0 {
		t.Errorf("org seeds %d metadata specs; provisioning belongs to the setup wizard (D1)",
			len(specs))
	}
}
