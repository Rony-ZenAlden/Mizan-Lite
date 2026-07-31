// Tests live in package jobs so catch-up planning and the claim can be exercised directly,
// and so a fixed clock can drive scheduling without waiting on wall time.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/internal/platform/outbox"
	"github.com/mizan-erp/mizan/migrations"
)

// ── harness ─────────────────────────────────────────────────────────────────────

var baseTime = time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)

func newDB(t *testing.T) *database.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "jobs_test.db")
	db, err := database.Open(database.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	runner, err := migrate.New(db, migrate.Options{
		FS: migrations.SQLite(), DBPath: dbPath, SkipBackup: true,
	})
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	if _, err := runner.Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// newScheduler builds a scheduler on a shared database and clock.
func newScheduler(t *testing.T, db *database.Store, clk *clock.Fixed, reg *Registry) *Scheduler {
	t.Helper()
	s, err := New(db, Options{
		Registry: reg, Clock: clk,
		// Fast retries so failure paths do not stretch the suite out.
		RetryBackoff: time.Millisecond, MaxRetryBackoff: 2 * time.Millisecond,
		ShutdownGrace: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

type harness struct {
	db  *database.Store
	clk *clock.Fixed
	reg *Registry
	s   *Scheduler
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db := newDB(t)
	clk := clock.NewFixed(baseTime)
	reg := NewRegistry()
	return &harness{db: db, clk: clk, reg: reg, s: newScheduler(t, db, clk, reg)}
}

// counter is a handler that records invocations.
type counter struct {
	mu          sync.Mutex
	calls       int
	occurrences []time.Time
	fail        error
	block       chan struct{}
}

func (c *counter) handle(ctx context.Context, run RunContext) error {
	c.mu.Lock()
	c.calls++
	c.occurrences = append(c.occurrences, run.Occurrence)
	block, fail := c.block, c.fail
	c.mu.Unlock()

	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fail
}

func (c *counter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (h *harness) runRows(t *testing.T, jobKey string) []RunRecord {
	t.Helper()
	recs, err := h.s.RecentRuns(context.Background(), jobKey, 200)
	if err != nil {
		t.Fatalf("RecentRuns: %v", err)
	}
	return recs
}

func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", want)
	}
	if got := errs.CodeOf(err); got != want {
		t.Fatalf("code = %q, want %q (err: %v)", got, want, err)
	}
}

// ── schedules ───────────────────────────────────────────────────────────────────

func TestEveryComputesFromNowNotByAccumulation(t *testing.T) {
	// On a desktop the laptop sleeps for hours. Accumulating `previous + interval` would
	// produce a backlog; recomputing from now self-heals.
	s := Every(30 * time.Second)
	if got := s.Next(baseTime); !got.Equal(baseTime.Add(30 * time.Second)) {
		t.Fatalf("Next = %v", got)
	}
	late := baseTime.Add(14 * time.Hour)
	if got := s.Next(late); !got.Equal(late.Add(30 * time.Second)) {
		t.Errorf("after a long sleep Next = %v, want it computed from the given instant", got)
	}
}

func TestScheduleRoundTripsThroughStorage(t *testing.T) {
	original := Every(90 * time.Second)
	if got := original.String(); got != "@every 1m30s" {
		t.Fatalf("String = %q", got)
	}
	parsed, err := ParseSchedule(original.String())
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	if parsed.Next(baseTime) != original.Next(baseTime) {
		t.Error("round trip changed the schedule")
	}
}

func TestParseScheduleRejectsUnsupportedForms(t *testing.T) {
	for _, bad := range []string{"", "0 2 * * *", "@every", "@every -5s", "@every zzz", "hourly"} {
		t.Run(bad, func(t *testing.T) {
			if _, err := ParseSchedule(bad); err == nil {
				t.Fatalf("accepted %q", bad)
			}
		})
	}
}

func TestBackwardsClockJumpDoesNotFreezeAJob(t *testing.T) {
	// If a user corrects a wrong system clock backwards, an accumulated schedule would push
	// next_run_at months out and the job would silently never run again.
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{}
	if err := h.reg.Register(Def{Key: "j", Schedule: Every(time.Minute)}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	h.clk.Advance(10 * time.Minute)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	h.s.wg.Wait()

	// Clock corrected backwards by an hour: the job must still become due again.
	h.clk.Current = baseTime.Add(-time.Hour)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	h.s.wg.Wait()
	if c.count() == 0 {
		t.Error("the job never ran after a backwards clock correction")
	}
}

// ── reconciliation ──────────────────────────────────────────────────────────────

func TestReconcileInsertsUpdatesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{}
	if err := h.reg.Register(Def{Key: "a", Schedule: Every(time.Minute)}, c.handle); err != nil {
		t.Fatal(err)
	}

	rep, err := h.s.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if rep.Inserted != 1 || rep.Disabled != 0 {
		t.Fatalf("first reconcile = %+v, want 1 inserted", rep)
	}

	rep2, err := h.s.Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Inserted != 0 || rep2.Updated != 1 {
		t.Errorf("second reconcile = %+v, want 0 inserted / 1 updated", rep2)
	}

	states, err := h.s.JobStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Key != "a" || states[0].Schedule != "@every 1m0s" {
		t.Errorf("states = %+v", states)
	}
}

func TestReconcilePreservesAdminDisable(t *testing.T) {
	// An admin who disabled the nightly backup must not find it re-enabled by an upgrade.
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{}
	if err := h.reg.Register(Def{Key: "a", Schedule: Every(time.Minute)}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.WriterPool().ExecContext(ctx,
		`UPDATE jobs SET is_enabled = 0 WHERE job_key = 'a'`); err != nil {
		t.Fatal(err)
	}

	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	states, _ := h.s.JobStates(ctx)
	if len(states) != 1 || states[0].Enabled {
		t.Error("reconciliation re-enabled a job an admin had disabled")
	}
	// And a disabled job is not claimed.
	h.clk.Advance(time.Hour)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	h.s.wg.Wait()
	if c.count() != 0 {
		t.Errorf("a disabled job ran %d times", c.count())
	}
}

func TestReconcileDisablesOrphanRowsWithoutFailing(t *testing.T) {
	// A job removed in a new release, or a downgrade. Refusing to start would keep a shop
	// closed over an inert row (same reasoning as unknown settings keys, 0.5 D3).
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{}
	if err := h.reg.Register(Def{Key: "gone", Schedule: Every(time.Minute)}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	// A build that no longer declares it.
	fresh := NewRegistry()
	s2 := newScheduler(t, h.db, h.clk, fresh)
	rep, err := s2.Reconcile(ctx)
	if err != nil {
		t.Fatalf("reconcile refused to start over an orphan row: %v", err)
	}
	if rep.Disabled != 1 {
		t.Errorf("report = %+v, want 1 disabled", rep)
	}

	states, _ := s2.JobStates(ctx)
	if len(states) != 1 || states[0].Enabled {
		t.Error("the orphan row was not disabled")
	}
}

func TestRegistrationRejections(t *testing.T) {
	r := NewRegistry()
	c := &counter{}
	if err := r.Register(Def{Key: "a", Schedule: Every(time.Minute)}, c.handle); err != nil {
		t.Fatal(err)
	}
	t.Run("duplicate", func(t *testing.T) {
		assertCode(t, r.Register(Def{Key: "a", Schedule: Every(time.Minute)}, c.handle), CodeDuplicateJob)
	})
	t.Run("no key", func(t *testing.T) {
		assertCode(t, r.Register(Def{Schedule: Every(time.Minute)}, c.handle), CodeInvalidDef)
	})
	t.Run("no schedule", func(t *testing.T) {
		assertCode(t, r.Register(Def{Key: "b"}, c.handle), CodeInvalidDef)
	})
	t.Run("no handler", func(t *testing.T) {
		assertCode(t, r.Register(Def{Key: "c", Schedule: Every(time.Minute)}, nil), CodeInvalidDef)
	})
	t.Run("bad policy", func(t *testing.T) {
		assertCode(t, r.Register(Def{Key: "d", Schedule: Every(time.Minute), CatchUp: "sometimes"},
			c.handle), CodeInvalidDef)
	})
}

func TestSingletonIsTheZeroValueDefault(t *testing.T) {
	// AllowConcurrent is inverted from the design's `Singleton bool` precisely so a job
	// declared without thinking about concurrency is a singleton.
	def := Def{Key: "a", Schedule: Every(time.Minute)}.withDefaults()
	if def.AllowConcurrent {
		t.Error("a job declared without specifying concurrency is not a singleton")
	}
	if def.CatchUp != RunOnce || def.MaxCatchUp != DefaultMaxCatchUp ||
		def.MaxAttempts != DefaultMaxAttempts || def.Timeout != DefaultTimeout {
		t.Errorf("defaults not applied: %+v", def)
	}
}

// ── THE CATCH-UP TABLE ──────────────────────────────────────────────────────────

func TestCatchUpPolicies(t *testing.T) {
	// The central question of a desktop scheduler: what happens to occurrences missed while
	// the app was closed? "A missed backup should run; three missed rate fetches should
	// collapse to one" (§24.1).
	cases := []struct {
		name        string
		policy      CatchUpPolicy
		maxCatchUp  int
		occurrences int
		wantRuns    int
		wantDropped int
		wantSkipped int
	}{
		{"run_once, nothing missed", RunOnce, 50, 1, 1, 0, 0},
		{"run_once collapses many missed into one", RunOnce, 50, 240, 1, 0, 0},
		{"skip runs when on time", Skip, 50, 1, 1, 0, 0},
		{"skip abandons when behind", Skip, 50, 12, 0, 0, 12},
		{"run_all runs each missed occurrence", RunAll, 50, 7, 7, 0, 0},
		{"run_all is capped", RunAll, 50, 86400, 50, 86350, 0},
		{"run_all cap is per job", RunAll, 3, 10, 3, 7, 0},
		{"run_all exactly at the cap", RunAll, 5, 5, 5, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := Def{
				Key: "j", Schedule: Every(time.Second),
				CatchUp: tc.policy, MaxCatchUp: tc.maxCatchUp,
			}.withDefaults()
			runs, dropped, skipped := planRuns(def, tc.occurrences)
			if runs != tc.wantRuns || dropped != tc.wantDropped || skipped != tc.wantSkipped {
				t.Errorf("runs=%d dropped=%d skipped=%d, want %d/%d/%d",
					runs, dropped, skipped, tc.wantRuns, tc.wantDropped, tc.wantSkipped)
			}
		})
	}
}

func TestRunAllCatchUpIsBoundedEndToEnd(t *testing.T) {
	// A shop closed for a month with a fast job. Running every occurrence would make the app
	// unusable at 8am — the opposite of what a scheduler is for.
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{}
	if err := h.reg.Register(Def{
		Key: "busy", Schedule: Every(time.Second), CatchUp: RunAll, MaxCatchUp: 5,
	}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	h.clk.Advance(time.Hour) // 3,600 missed occurrences
	rep, err := h.s.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	h.s.wg.Wait()

	if rep.Started != 5 {
		t.Errorf("started %d runs, want the cap of 5", rep.Started)
	}
	if rep.Dropped < 3000 {
		t.Errorf("dropped = %d, want the excess reported, not hidden", rep.Dropped)
	}
	if c.count() != 5 {
		t.Errorf("handler ran %d times, want 5", c.count())
	}
}

func TestCatchUpRunsCarryTheirOccurrenceTime(t *testing.T) {
	// A catch-up run represents a past instant. A job that stamps records with it stays
	// historically accurate rather than backdating everything to "now".
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{}
	if err := h.reg.Register(Def{
		Key: "j", Schedule: Every(time.Minute), CatchUp: RunAll, MaxCatchUp: 10,
	}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	h.clk.Advance(3 * time.Minute)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	h.s.wg.Wait()

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.occurrences) < 2 {
		t.Fatalf("got %d runs, want several catch-up runs", len(c.occurrences))
	}
	for i := 1; i < len(c.occurrences); i++ {
		if !c.occurrences[i].After(c.occurrences[i-1]) {
			t.Errorf("occurrence %d (%v) is not after %v", i, c.occurrences[i], c.occurrences[i-1])
		}
	}
}

// ── claiming ────────────────────────────────────────────────────────────────────

func TestTwoSchedulersRunADueJobExactlyOnce(t *testing.T) {
	// The drill that caught the 0.6 bug, applied here: select-then-mark would let both
	// schedulers run the same occurrence.
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{}
	if err := h.reg.Register(Def{Key: "j", Schedule: Every(time.Minute)}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(2 * time.Minute)

	second := newScheduler(t, h.db, h.clk, h.reg)

	var wg sync.WaitGroup
	for _, s := range []*Scheduler{h.s, second} {
		sched := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := sched.Tick(ctx); err != nil {
				t.Errorf("Tick: %v", err)
			}
		}()
	}
	wg.Wait()
	h.s.wg.Wait()
	second.wg.Wait()

	if c.count() != 1 {
		t.Errorf("the job ran %d times across two schedulers, want exactly 1", c.count())
	}
}

func TestOnlyOneSchedulerWinsASimultaneousClaim(t *testing.T) {
	// The deterministic version of the race. TestTwoSchedulersRunADueJobExactlyOnce above is
	// timing-dependent — the single-writer pool usually serialises the two ticks, so the
	// second never even sees the job as due. Here BOTH schedulers read the due row first and
	// only then claim, which is exactly the interleaving that must be safe.
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{}
	if err := h.reg.Register(Def{Key: "j", Schedule: Every(time.Minute)}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(2 * time.Minute)

	second := newScheduler(t, h.db, h.clk, h.reg)
	now := h.clk.Now()

	dueA, err := h.s.loadDueJobs(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	dueB, err := second.loadDueJobs(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(dueA) != 1 || len(dueB) != 1 {
		t.Fatalf("both schedulers should see the job as due: %d and %d", len(dueA), len(dueB))
	}

	next := now.Add(time.Minute)
	wonA, err := h.s.claim(ctx, dueA[0], next, now)
	if err != nil {
		t.Fatal(err)
	}
	wonB, err := second.claim(ctx, dueB[0], next, now)
	if err != nil {
		t.Fatal(err)
	}
	if wonA == wonB {
		t.Errorf("both schedulers claimed the same occurrence (A=%v B=%v); "+
			"the compare-and-swap is not guarding", wonA, wonB)
	}
}

func TestClaimOnStaleNextRunAtAffectsNothing(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{}
	if err := h.reg.Register(Def{Key: "j", Schedule: Every(time.Minute)}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := h.s.loadJobRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	row := rows["j"]

	// Someone else advanced the schedule.
	if _, updErr := h.db.WriterPool().ExecContext(ctx,
		`UPDATE jobs SET next_run_at = ? WHERE job_key = 'j'`,
		clock.Format(baseTime.Add(time.Hour))); updErr != nil {
		t.Fatal(updErr)
	}

	claimed, err := h.s.claim(ctx, row, baseTime.Add(2*time.Hour), baseTime)
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Error("a claim on a stale next_run_at succeeded; the compare-and-swap is not guarding")
	}
}

// ── singleton ───────────────────────────────────────────────────────────────────

func TestSingletonSkipsWhileALiveRunHoldsTheLease(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{block: make(chan struct{})}
	if err := h.reg.Register(Def{
		Key: "slow", Schedule: Every(time.Minute), Timeout: time.Hour,
	}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	h.clk.Advance(time.Minute)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	// Wait for the run to be recorded as running.
	waitFor(t, func() bool { return c.count() == 1 })

	h.clk.Advance(time.Minute)
	rep, err := h.s.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.SkippedBusy != 1 {
		t.Errorf("report = %+v, want the due occurrence skipped while a run holds the lease", rep)
	}
	if c.count() != 1 {
		t.Errorf("a singleton job ran %d times concurrently", c.count())
	}

	close(c.block)
	h.s.wg.Wait()
}

func TestExpiredLeaseNoLongerBlocksASingleton(t *testing.T) {
	// A run whose lease expired belongs to a dead process; it must not block the job forever.
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{}
	if err := h.reg.Register(Def{
		Key: "j", Schedule: Every(time.Minute), Timeout: 30 * time.Second,
	}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := h.s.loadJobRows(ctx)
	runID, _ := id.New()
	if _, err := h.db.WriterPool().ExecContext(ctx, `
		INSERT INTO job_runs (id, job_id, started_at, status, attempt, triggered_by)
		VALUES (?, ?, ?, 'running', 1, 'schedule')`,
		runID.String(), rows["j"].id.String(), clock.Format(baseTime)); err != nil {
		t.Fatal(err)
	}

	// Inside the lease: blocked.
	h.clk.Advance(10 * time.Second)
	rep, err := h.s.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.SkippedBusy != 1 {
		t.Fatalf("report = %+v, want the job blocked inside its lease", rep)
	}

	// Past it: free.
	h.clk.Advance(time.Hour)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	h.s.wg.Wait()
	if c.count() != 1 {
		t.Errorf("handler ran %d times after the lease expired, want 1", c.count())
	}
}

func TestAllowConcurrentPermitsOverlap(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{block: make(chan struct{})}
	if err := h.reg.Register(Def{
		Key: "parallel", Schedule: Every(time.Minute), Timeout: time.Hour, AllowConcurrent: true,
	}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	h.clk.Advance(time.Minute)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return c.count() == 1 })

	h.clk.Advance(time.Minute)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return c.count() == 2 })

	close(c.block)
	h.s.wg.Wait()
}

// ── execution & run records ─────────────────────────────────────────────────────

func TestSuccessfulRunIsRecordedWithOutput(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	if err := h.reg.Register(Def{Key: "j", Schedule: Every(time.Minute)},
		func(_ context.Context, run RunContext) error {
			run.Progress(50, "half way")
			run.SetOutput(`{"processed":3}`)
			return nil
		}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(time.Minute)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	h.s.wg.Wait()

	recs := h.runRows(t, "j")
	if len(recs) != 1 {
		t.Fatalf("got %d runs, want 1", len(recs))
	}
	r := recs[0]
	if r.Status != StatusSucceeded || r.Attempt != 1 || r.TriggeredBy != TriggerSchedule {
		t.Errorf("run = %+v", r)
	}
	if r.Output != `{"processed":3}` {
		t.Errorf("output = %q — the panel needs this to be more than a green tick", r.Output)
	}
	if r.FinishedAt.IsZero() {
		t.Error("finished_at was not recorded")
	}
}

func TestFailureRetriesThenGivesUp(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{fail: errors.New("nope")}
	if err := h.reg.Register(Def{
		Key: "flaky", Schedule: Every(time.Minute), MaxAttempts: 3,
	}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(time.Minute)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	h.s.wg.Wait()

	if c.count() != 3 {
		t.Errorf("handler ran %d times, want MaxAttempts (3)", c.count())
	}
	recs := h.runRows(t, "flaky")
	if len(recs) != 3 {
		t.Fatalf("got %d run records, want one per attempt", len(recs))
	}
	for _, r := range recs {
		if r.Status != StatusFailed {
			t.Errorf("status = %q, want failed", r.Status)
		}
		if r.Error == "" {
			t.Error("the failure reason was not recorded; a stuck job would be undiagnosable")
		}
	}
}

func TestHandlerPanicBecomesARecordedFailure(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	if err := h.reg.Register(Def{Key: "boom", Schedule: Every(time.Minute), MaxAttempts: 1},
		func(context.Context, RunContext) error { panic("nil map") }); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(time.Minute)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatalf("a panic escaped the scheduler: %v", err)
	}
	h.s.wg.Wait()

	recs := h.runRows(t, "boom")
	if len(recs) != 1 || recs[0].Status != StatusFailed {
		t.Fatalf("runs = %+v, want one failed run", recs)
	}
	if recs[0].Error == "" {
		t.Error("the panic was not recorded")
	}
}

func TestTimeoutIsRecorded(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	if err := h.reg.Register(Def{
		Key: "hangs", Schedule: Every(time.Minute), Timeout: 20 * time.Millisecond, MaxAttempts: 1,
	}, func(ctx context.Context, _ RunContext) error {
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(time.Minute)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	h.s.wg.Wait()

	recs := h.runRows(t, "hangs")
	if len(recs) != 1 || recs[0].Status != StatusTimeout {
		t.Fatalf("runs = %+v, want one timeout", recs)
	}
}

// ── crash recovery — the drill ──────────────────────────────────────────────────

func TestReclaimAbandonedRuns(t *testing.T) {
	// A desktop app killed mid-run leaves job_runs rows at `running` forever, and a
	// singleton job would then never run again.
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{}
	if err := h.reg.Register(Def{
		Key: "j", Schedule: Every(time.Minute), Timeout: time.Minute,
	}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := h.s.loadJobRows(ctx)

	// Two runs left behind: one recent, one long expired.
	for _, started := range []time.Time{baseTime, baseTime.Add(-time.Hour)} {
		runID, _ := id.New()
		if _, err := h.db.WriterPool().ExecContext(ctx, `
			INSERT INTO job_runs (id, job_id, started_at, status, attempt, triggered_by)
			VALUES (?, ?, ?, 'running', 1, 'schedule')`,
			runID.String(), rows["j"].id.String(), clock.Format(started)); err != nil {
			t.Fatal(err)
		}
	}

	reclaimed, err := h.s.ReclaimAbandonedRuns(ctx)
	if err != nil {
		t.Fatalf("ReclaimAbandonedRuns: %v", err)
	}
	if reclaimed != 1 {
		t.Errorf("reclaimed %d, want only the expired one", reclaimed)
	}

	recs := h.runRows(t, "j")
	var timeouts, running int
	for _, r := range recs {
		switch r.Status {
		case StatusTimeout:
			timeouts++
		case StatusRunning:
			running++
		}
	}
	if timeouts != 1 || running != 1 {
		t.Errorf("statuses after reclaim: %d timeout, %d running; want 1 and 1", timeouts, running)
	}
}

// ── lifecycle ───────────────────────────────────────────────────────────────────

func TestStartStopDrainsAndLeavesNothingRunning(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	done := make(chan struct{})
	c := &counter{block: done}
	if err := h.reg.Register(Def{
		Key: "j", Schedule: Every(time.Millisecond), Timeout: time.Hour,
	}, c.handle); err != nil {
		t.Fatal(err)
	}

	// A real clock here: Start drives a wall-clock ticker.
	live, err := New(h.db, Options{
		Registry: h.reg, TickInterval: 2 * time.Millisecond, ShutdownGrace: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if startErr := live.Start(ctx); startErr != nil {
		t.Fatalf("Start: %v", startErr)
	}
	waitFor(t, func() bool { return c.count() >= 1 })

	// The handler is still blocked, so Stop must hit its grace period and cancel.
	if stopErr := live.Stop(ctx); stopErr != nil {
		t.Fatalf("Stop: %v", stopErr)
	}
	close(done)

	recs, err := live.RecentRuns(ctx, "j", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.Status == StatusRunning {
			t.Errorf("a run was left `running` after shutdown; the next startup sees a phantom lease")
		}
	}
}

func TestStartIsNotReentrant(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	s, err := New(h.db, Options{Registry: h.reg, TickInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Stop(ctx) }()
	if err := s.Start(ctx); err == nil {
		t.Error("Start succeeded twice; two tick loops would double every claim attempt")
	}
}

func TestRunNowTriggersOutsideTheSchedule(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	c := &counter{}
	if err := h.reg.Register(Def{Key: "j", Schedule: Every(time.Hour)}, c.handle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.s.RunNow(ctx, "j"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if c.count() != 1 {
		t.Errorf("handler ran %d times, want 1", c.count())
	}
	recs := h.runRows(t, "j")
	if len(recs) != 1 || recs[0].TriggeredBy != TriggerManual {
		t.Errorf("runs = %+v, want one manual run", recs)
	}
	t.Run("unknown key", func(t *testing.T) {
		assertCode(t, h.s.RunNow(ctx, "nope"), CodeUnknownJob)
	})
}

// ── 0.6 + 0.7 together ──────────────────────────────────────────────────────────

type invoicePosted struct {
	Invoice id.ID `json:"invoice"`
}

func (invoicePosted) EventType() string     { return "sales.invoice_posted" }
func (invoicePosted) AggregateType() string { return "sales_document" }
func (e invoicePosted) AggregateID() id.ID  { return e.Invoice }

func TestOutboxDispatchJobDrainsAPublishedEvent(t *testing.T) {
	// The end-to-end proof: an event published inside a Unit of Work is delivered by the
	// scheduled job, and the delivery ends `done`. This is 0.6 and 0.7 working together, and
	// it closes the item 0.6 carried forward.
	ctx := context.Background()
	h := newHarness(t)

	subs := outbox.NewSubscribers()
	var delivered int
	var mu sync.Mutex
	if err := subs.Register("sales.invoice_posted", "accounting",
		func(context.Context, event.Envelope) error {
			mu.Lock()
			defer mu.Unlock()
			delivered++
			return nil
		}); err != nil {
		t.Fatal(err)
	}

	store := outbox.NewStore(h.db, subs, h.clk, nil)
	dispatcher := outbox.NewDispatcher(h.db, subs, outbox.DispatcherOptions{Clock: h.clk})

	if err := RegisterOutboxDispatch(h.reg, dispatcher, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	if err := h.db.Do(ctx, func(ctx context.Context) error {
		_, err := store.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return err
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	h.clk.Advance(10 * time.Second)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	h.s.wg.Wait()

	mu.Lock()
	got := delivered
	mu.Unlock()
	if got != 1 {
		t.Fatalf("the subscriber was invoked %d times, want 1", got)
	}

	var done int
	if err := h.db.WriterPool().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM outbox_deliveries WHERE status = 'done'`).Scan(&done); err != nil {
		t.Fatal(err)
	}
	if done != 1 {
		t.Errorf("done deliveries = %d, want 1", done)
	}

	// The run records what it did, not just that it ran.
	recs := h.runRows(t, KeyOutboxDispatch)
	if len(recs) != 1 || recs[0].Status != StatusSucceeded {
		t.Fatalf("runs = %+v", recs)
	}
	var rep outbox.Report
	if err := json.Unmarshal([]byte(recs[0].Output), &rep); err != nil {
		t.Fatalf("decoding the run output: %v", err)
	}
	if rep.Delivered != 1 {
		t.Errorf("recorded report = %+v, want 1 delivered", rep)
	}
}

func TestHeartbeatJobRecordsItsOccurrence(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	if err := RegisterHeartbeat(h.reg, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(time.Minute)
	if _, err := h.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	h.s.wg.Wait()

	recs := h.runRows(t, KeyHeartbeat)
	if len(recs) != 1 || recs[0].Status != StatusSucceeded {
		t.Fatalf("runs = %+v, want one successful heartbeat", recs)
	}
	var out HeartbeatOutput
	if err := json.Unmarshal([]byte(recs[0].Output), &out); err != nil {
		t.Fatalf("decoding heartbeat output: %v", err)
	}
	if out.At == "" {
		t.Error("the heartbeat recorded no timestamp")
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────────

// waitFor polls until cond holds, so tests do not depend on goroutine scheduling.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for a condition")
}
