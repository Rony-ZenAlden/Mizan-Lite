package bootstrap_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/currency/domain"
	"github.com/mizan-erp/mizan/internal/platform/jobs"
	"github.com/mizan-erp/mizan/internal/platform/paths"
)

// ── harness ─────────────────────────────────────────────────────────────────────

// bootIn starts a full graph in a private data directory.
//
// StartScheduler is off: tests drive Tick directly so scheduling is deterministic rather than
// a race against wall time. Everything else is exactly what the shipped binary does.
func bootIn(t *testing.T, dir string) *bootstrap.App {
	t.Helper()
	t.Setenv(paths.EnvOverride, dir)

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

func boot(t *testing.T) *bootstrap.App {
	t.Helper()
	return bootIn(t, t.TempDir())
}

// ── the happy path ──────────────────────────────────────────────────────────────

func TestStartBuildsAWorkingGraph(t *testing.T) {
	app := boot(t)

	if app.DB == nil || app.Settings == nil || app.Catalog == nil ||
		app.Bus == nil || app.Outbox == nil || app.Scheduler == nil ||
		app.Currency == nil || app.Org == nil {
		t.Fatalf("graph has nil components: %+v", app)
	}

	// Asserted by PRESENCE, not by exact count.
	//
	// This previously read `len(app.Modules) != 1 ... want [currency]` and broke the moment
	// org arrived — the same brittleness Step 0.6 §11.2 fixed for the migration count, and for
	// the same reason: a test that must be edited every time the system legitimately grows is
	// a maintenance tax, not a safety net. Phase 1 adds three more modules.
	present := map[string]bool{}
	for _, m := range app.Modules {
		present[m.Name()] = true
	}
	for _, want := range []string{"currency", "org"} {
		if !present[want] {
			t.Errorf("module %q is missing from the graph; got %v", want, present)
		}
	}
}

func TestMigrationsAndSeedsAreApplied(t *testing.T) {
	app := boot(t)
	ctx := app.Context()

	// The currency module's own migration ran, merged with the platform's.
	syp, err := app.Currency.Get(ctx, "SYP")
	if err != nil {
		t.Fatalf("currency SYP: %v", err)
	}
	if syp.Decimals() != 0 {
		t.Errorf("SYP decimals = %d, want 0", syp.Decimals())
	}

	types, err := app.Currency.RateTypes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 4 {
		t.Errorf("rate types = %d, want 4", len(types))
	}
}

func TestSecondBootIsClean(t *testing.T) {
	// The ordinary case: a customer closes the app and opens it again. No migrations pending,
	// no seed churn, no leftover state.
	dir := t.TempDir()

	first := bootIn(t, dir)
	afterOne := countRows(t, first, "currencies")
	if err := first.Shutdown(context.Background()); err != nil {
		t.Fatalf("first shutdown: %v", err)
	}

	second := bootIn(t, dir)
	ctx := second.Context()
	if _, err := second.Currency.Get(ctx, "USD"); err != nil {
		t.Fatalf("second boot cannot read seeded data: %v", err)
	}

	// Seeding every boot (D4) must not duplicate rows.
	//
	// Compared against the FIRST boot rather than a literal. The literal was 4, and adding
	// three currencies in Step 1.9 broke this test while the property it guards — idempotency —
	// was never in question. That is the fifth count-based assertion in this project to break
	// for a reason unrelated to what it tested.
	afterTwo := countRows(t, second, "currencies")
	if afterTwo != afterOne {
		t.Errorf("currencies = %d after two boots, %d after one — seeding is not idempotent",
			afterTwo, afterOne)
	}
	if afterOne == 0 {
		t.Error("no currencies were seeded at all")
	}
}

func countRows(t *testing.T, app *bootstrap.App, table string) int {
	t.Helper()
	var count int
	if err := app.DB.WriterPool().QueryRowContext(app.Context(),
		`SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestSeedingSelfHealsADeletedSystemRow(t *testing.T) {
	// The reason D4 has no first-run flag: always seeding repairs a row someone removed.
	dir := t.TempDir()
	first := bootIn(t, dir)
	ctx := first.Context()
	if _, err := first.DB.WriterPool().ExecContext(ctx,
		`DELETE FROM currencies WHERE code = 'EUR'`); err != nil {
		t.Fatal(err)
	}
	if err := first.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	second := bootIn(t, dir)
	if _, err := second.Currency.Get(second.Context(), "EUR"); err != nil {
		t.Errorf("a deleted system currency was not restored on the next boot: %v", err)
	}
}

// ── THE ORDERING DRILL ──────────────────────────────────────────────────────────

type invoicePosted struct {
	Invoice id.ID `json:"invoice"`
}

func (invoicePosted) EventType() string     { return "sales.invoice_posted" }
func (invoicePosted) AggregateType() string { return "sales_document" }
func (e invoicePosted) AggregateID() id.ID  { return e.Invoice }

func TestOutboxIsDeliveredThroughTheScheduledJob(t *testing.T) {
	// The end-to-end wiring proof: the outbox store, the subscriber registry, the dispatcher,
	// and the scheduler all share one graph, and an event published inside a Unit of Work is
	// delivered by the SCHEDULED job rather than by calling the dispatcher directly.
	//
	// Honest scope: this does NOT prove bootstrap's internal subscribe-before-start ordering,
	// because no Phase-0 module registers a subscription — the observer below is registered by
	// the test, after Start has returned. That ordering is enforced by the sequence in
	// bootstrap.go and becomes mutation-testable as soon as a module subscribes for real
	// (accounting, Phase 2). Recorded rather than overclaimed.
	app := boot(t)
	ctx := app.Context()

	var mu sync.Mutex
	delivered := 0
	if err := app.Subs.Register("sales.invoice_posted", "test-observer",
		func(context.Context, event.Envelope) error {
			mu.Lock()
			defer mu.Unlock()
			delivered++
			return nil
		}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := app.DB.Do(ctx, func(ctx context.Context) error {
		_, pubErr := app.Outbox.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return pubErr
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Run the scheduler the way the app does — through the job, not by calling the dispatcher.
	if err := app.Scheduler.RunNow(ctx, jobs.KeyOutboxDispatch); err != nil {
		t.Fatalf("RunNow: %v", err)
	}

	mu.Lock()
	got := delivered
	mu.Unlock()
	if got != 1 {
		t.Fatalf("subscriber invoked %d times, want 1 — the dispatcher ran before subscriptions "+
			"were registered, so the delivery was skipped", got)
	}

	var done int
	if err := app.DB.WriterPool().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM outbox_deliveries WHERE status = 'done'`).Scan(&done); err != nil {
		t.Fatal(err)
	}
	if done != 1 {
		t.Errorf("done deliveries = %d, want 1", done)
	}
}

func TestJobsAreRegisteredAndReconciled(t *testing.T) {
	app := boot(t)
	states, err := app.Scheduler.JobStates(app.Context())
	if err != nil {
		t.Fatalf("JobStates: %v", err)
	}

	found := map[string]bool{}
	for _, s := range states {
		found[s.Key] = true
		if !s.Enabled {
			t.Errorf("job %q is disabled after boot", s.Key)
		}
	}
	for _, key := range []string{jobs.KeyOutboxDispatch, jobs.KeyHeartbeat} {
		if !found[key] {
			t.Errorf("job %q was not reconciled into the jobs table", key)
		}
	}
}

// ── migration failure ───────────────────────────────────────────────────────────

func TestStartFailsFatallyOnACorruptDatabase(t *testing.T) {
	// A database that cannot be migrated must stop startup, not produce a half-built graph.
	// Step 0.4 closes the Store on its restore path, so continuing would mean every later
	// component operating on a closed handle.
	dir := t.TempDir()
	t.Setenv(paths.EnvOverride, dir)

	resolved, err := paths.Resolve("Mizan")
	if err != nil {
		t.Fatal(err)
	}
	// A file that is not a SQLite database at all.
	if writeErr := os.WriteFile(resolved.DBFile, []byte("this is not a database"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	app, err := bootstrap.Start(context.Background(), bootstrap.Options{
		Paths: resolved, SkipBackup: true,
	})
	if err == nil {
		_ = app.Shutdown(context.Background())
		t.Fatal("Start succeeded on a corrupt database")
	}
	if app != nil {
		t.Error("Start returned a partially-built graph alongside its error")
	}
	if errs.CodeOf(err) == "" {
		t.Errorf("startup failure is not a typed error: %v", err)
	}
}

// ── shutdown ────────────────────────────────────────────────────────────────────

func TestShutdownLeavesNothingRunningAndIsRepeatable(t *testing.T) {
	dir := t.TempDir()
	app := bootIn(t, dir)
	ctx := context.Background()

	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	// A second call must be a no-op, not a double-close.
	if err := app.Shutdown(ctx); err != nil {
		t.Errorf("second Shutdown: %v", err)
	}

	// Reopen and confirm no job run was left `running` — the next start would otherwise find
	// a phantom lease blocking its singleton jobs.
	reopened := bootIn(t, dir)
	runs, err := reopened.Scheduler.RecentRuns(reopened.Context(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if r.Status == jobs.StatusRunning {
			t.Errorf("job %q was left running across shutdown", r.JobKey)
		}
	}
}

func TestShutdownFlushesPendingOutboxWork(t *testing.T) {
	// The final dispatch pass: work already committed goes out before the process ends.
	app := boot(t)
	ctx := app.Context()

	var mu sync.Mutex
	delivered := 0
	if err := app.Subs.Register("sales.invoice_posted", "flush-observer",
		func(context.Context, event.Envelope) error {
			mu.Lock()
			defer mu.Unlock()
			delivered++
			return nil
		}); err != nil {
		t.Fatal(err)
	}
	if err := app.DB.Do(ctx, func(ctx context.Context) error {
		_, pubErr := app.Outbox.Publish(ctx, invoicePosted{Invoice: "inv-flush"})
		return pubErr
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if delivered != 1 {
		t.Errorf("pending delivery was not flushed at shutdown (delivered %d)", delivered)
	}
}

// ── the wired graph, end to end ─────────────────────────────────────────────────

func TestCurrencyConversionWorksThroughTheBuiltGraph(t *testing.T) {
	// Proof that the module really received its dependencies: settings resolve the pivot and
	// the rate-type binding, the repositories read the migrated schema, and the converter
	// returns a rate the caller can store.
	app := boot(t)
	ctx := app.Context()

	if err := app.Currency.SetRate(ctx, "USD", "SYP", domain.RateTypeMarket,
		money.RateFromNano(13_000*1_000_000_000), time.Now().UTC(), "manual"); err != nil {
		t.Fatalf("SetRate: %v", err)
	}

	usd, err := app.Currency.Get(ctx, "USD")
	if err != nil {
		t.Fatal(err)
	}
	syp, err := app.Currency.Get(ctx, "SYP")
	if err != nil {
		t.Fatal(err)
	}

	res, err := app.Currency.ConvertForContext(ctx, money.FromMinor(usd, 100), syp,
		time.Now().UTC(), currency.ContextSales)
	if err != nil {
		t.Fatalf("ConvertForContext: %v", err)
	}
	if res.Amount.Minor() != 13_000 {
		t.Errorf("converted to %d, want 13000", res.Amount.Minor())
	}
	if res.Rate.IsZero() {
		t.Error("Result carries no rate; a document could not record what it used")
	}
}

func TestContextCarriesLocaleAndCorrelation(t *testing.T) {
	// appctx closes the item Step 0.8 carried forward: the locale was resolvable from the
	// setting, but nothing stamped it onto the context.
	app := boot(t)
	ctx := app.Context()

	if got := locale.FromContext(ctx); got.IsZero() {
		t.Error("no locale on the per-call context")
	}
	if _, ok := event.CorrelationID(ctx); !ok {
		t.Error("no correlation id on the per-call context; an event chain would be untraceable")
	}

	// Each call gets its own correlation, so two user actions are distinguishable.
	first, _ := event.CorrelationID(app.Context())
	second, _ := event.CorrelationID(app.Context())
	if first == second {
		t.Error("two calls shared a correlation id")
	}
}

func TestArabicNamesResolveThroughTheGraph(t *testing.T) {
	app := boot(t)
	arabic := locale.WithLocale(app.Context(), "ar")

	infos, err := app.Currency.List(arabic, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Code == "SYP" && info.Name != "ليرة سورية" {
			t.Errorf("SYP name in Arabic = %q, want the seeded translation", info.Name)
		}
	}
}

// ── data directory ──────────────────────────────────────────────────────────────

func TestStartRequiresPaths(t *testing.T) {
	_, err := bootstrap.Start(context.Background(), bootstrap.Options{})
	if err == nil {
		t.Fatal("Start succeeded without paths")
	}
	if !errors.Is(err, err) || errs.CodeOf(err) != bootstrap.CodeStartupFailed {
		t.Errorf("code = %q, want %q", errs.CodeOf(err), bootstrap.CodeStartupFailed)
	}
}

func TestDataDirectoryLayout(t *testing.T) {
	dir := t.TempDir()
	app := bootIn(t, dir)

	for _, want := range []string{
		app.Paths.DBFile,
		app.Paths.Backups,
		app.Paths.Logs,
	} {
		if _, err := os.Stat(want); err != nil && !filepath.IsAbs(want) {
			t.Errorf("path %q is not absolute", want)
		}
	}
	if _, err := os.Stat(app.Paths.Backups); err != nil {
		t.Errorf("backups directory was not created: %v", err)
	}
}

// ── Step 1.1: org ───────────────────────────────────────────────────────────────

// TestStartSucceedsOnAnUnprovisionedDatabase pins Step 1.1's D1.
//
// A fresh install has NO company until the setup wizard runs, and Start must succeed against
// exactly that state. If org ever starts seeding a placeholder company at boot, the wizard's
// "no company exists yet" invariant (§WIZ.1) is false before it ever runs — and this fails.
func TestStartSucceedsOnAnUnprovisionedDatabase(t *testing.T) {
	app := boot(t)

	if app.Org == nil {
		t.Fatal("the org service is not wired into the graph")
	}
	provisioned, err := app.Org.IsProvisioned(app.Context())
	if err != nil {
		t.Fatalf("IsProvisioned: %v", err)
	}
	if provisioned {
		t.Error("a freshly booted database reports a company; nothing may be provisioned at boot (D1)")
	}
}

// TestModulesAreOrderedByDependency is the first real exercise of the topological sort built
// in Step 0.10: org depends on currency (its FK on currencies(code)), and the composition root
// deliberately hands the modules in the WRONG order to prove the sort reorders them.
func TestModulesAreOrderedByDependency(t *testing.T) {
	app := boot(t)

	var currencyAt, orgAt = -1, -1
	for i, m := range app.Modules {
		switch m.Name() {
		case "currency":
			currencyAt = i
		case "org":
			orgAt = i
		}
	}
	if currencyAt == -1 || orgAt == -1 {
		t.Fatalf("expected both modules, got %d", len(app.Modules))
	}
	if currencyAt > orgAt {
		t.Errorf("currency is ordered after org (%d > %d); org's schema references currencies(code)",
			currencyAt, orgAt)
	}
}
