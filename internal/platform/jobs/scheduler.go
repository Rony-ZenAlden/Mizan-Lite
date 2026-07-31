package jobs

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Database is what the scheduler needs: executor resolution plus the Unit of Work.
type Database interface {
	database.DB
	database.UnitOfWork
}

// Options configures a Scheduler. Every field has a defensible default.
type Options struct {
	Registry *Registry
	// Workers bounds concurrent runs. Default min(4, NumCPU).
	Workers int
	// TickInterval is how often the scheduler looks for due jobs. Default 1s.
	TickInterval time.Duration
	// BatchSize caps how many due jobs one tick considers. Default 20.
	BatchSize int
	// ShutdownGrace is how long Stop waits for in-flight runs to drain. Default 10s.
	ShutdownGrace time.Duration
	// RetryBackoff is the first in-run retry delay; it doubles per attempt. Default 1s.
	RetryBackoff time.Duration
	// MaxRetryBackoff caps it. Default 30s.
	MaxRetryBackoff time.Duration
	Clock           clock.Clock
	Logger          *slog.Logger
}

func (o Options) withDefaults() Options {
	if o.Workers <= 0 {
		o.Workers = min(4, runtime.NumCPU())
	}
	if o.TickInterval <= 0 {
		o.TickInterval = time.Second
	}
	if o.BatchSize <= 0 {
		o.BatchSize = 20
	}
	if o.ShutdownGrace <= 0 {
		o.ShutdownGrace = 10 * time.Second
	}
	if o.RetryBackoff <= 0 {
		o.RetryBackoff = time.Second
	}
	if o.MaxRetryBackoff <= 0 {
		o.MaxRetryBackoff = 30 * time.Second
	}
	if o.Clock == nil {
		o.Clock = clock.System()
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o
}

// Scheduler claims due jobs and runs them on a bounded pool.
type Scheduler struct {
	db   Database
	reg  *Registry
	opts Options
	clk  clock.Clock
	log  *slog.Logger

	sem chan struct{} // bounded worker pool
	wg  sync.WaitGroup

	mu      sync.Mutex
	stop    chan struct{}
	running bool
}

// New builds a Scheduler.
func New(db Database, opts Options) (*Scheduler, error) {
	if db == nil {
		return nil, errs.Internal(CodeInvalidDef, "jobs: a database is required")
	}
	opts = opts.withDefaults()
	if opts.Registry == nil {
		opts.Registry = NewRegistry()
	}
	return &Scheduler{
		db: db, reg: opts.Registry, opts: opts,
		clk: opts.Clock, log: opts.Logger,
		sem: make(chan struct{}, opts.Workers),
	}, nil
}

// Registry exposes the job registry, so the bootstrap can register jobs after construction.
func (s *Scheduler) Registry() *Registry { return s.reg }

// TickReport summarises one scheduling pass.
type TickReport struct {
	Claimed       int // jobs whose occurrence this scheduler won
	Started       int // runs actually launched (catch-up may launch several per job)
	SkippedBusy   int // singleton jobs skipped because a live run holds the lease
	SkippedPolicy int // occurrences dropped by the Skip catch-up policy
	Contended     int // another scheduler claimed it first
	Dropped       int // catch-up occurrences beyond MaxCatchUp
}

// Tick performs one scheduling pass: find due jobs, claim them, launch their runs.
//
// Exposed and callable, exactly as outbox.DispatchOnce is, so the scheduling logic can be
// tested deterministically without a running loop and a real wall clock.
func (s *Scheduler) Tick(ctx context.Context) (TickReport, error) {
	var rep TickReport
	now := s.clk.Now()

	due, err := s.loadDueJobs(ctx, now)
	if err != nil {
		return rep, err
	}

	for _, row := range due {
		e, declared := s.reg.lookup(row.key)
		if !declared {
			// Reconcile disables these; a row can only appear here if it was added between
			// reconciliation and now.
			continue
		}
		def := e.def

		// Singleton: a long run may still be in flight when the next occurrence falls due.
		if !def.AllowConcurrent {
			live, liveErr := s.hasLiveRun(ctx, row.id, row.timeout)
			if liveErr != nil {
				return rep, liveErr
			}
			if live {
				rep.SkippedBusy++
				continue
			}
		}

		// Counted without applying the catch-up cap: planRuns needs the true figure to report
		// how many occurrences were dropped.
		occurrences := missedOccurrences(def.Schedule, row.nextRunAt, now, enumerationGuard)
		if occurrences == 0 {
			continue
		}
		runs, dropped, policySkipped := planRuns(def, occurrences)
		rep.Dropped += dropped
		rep.SkippedPolicy += policySkipped

		// Claim before launching anything. One conditional UPDATE guarded on the
		// next_run_at we just read — the same compare-and-swap the outbox uses, and for the
		// same reason: select-then-mark let three dispatchers deliver ten events thirty
		// times in Step 0.6.
		claimed, err := s.claim(ctx, row, def.Schedule.Next(now), now)
		if err != nil {
			return rep, err
		}
		if !claimed {
			rep.Contended++
			continue
		}
		rep.Claimed++

		if runs == 0 {
			continue // policy said skip; the schedule has still advanced
		}
		if dropped > 0 {
			s.log.WarnContext(ctx, "catch-up bounded; dropping the excess missed occurrences",
				slog.String("job_key", row.key),
				slog.Int("ran", runs), slog.Int("dropped", dropped),
				slog.Int("max_catch_up", def.MaxCatchUp))
		}

		occurrenceTimes := make([]time.Time, runs)
		for i := range occurrenceTimes {
			occurrenceTimes[i] = row.nextRunAt.Add(time.Duration(i) * occurrenceStep(def.Schedule))
		}
		s.launchSeries(ctx, row, e, occurrenceTimes, TriggerSchedule)
		rep.Started += runs
	}
	return rep, nil
}

// planRuns applies the catch-up policy, returning how many runs to launch, how many
// occurrences were dropped by the cap, and how many were dropped by policy.
//
// `occurrences` includes the one that is due right now, so `occurrences - 1` is the number
// genuinely missed while the app was closed.
func planRuns(def Def, occurrences int) (runs, dropped, policySkipped int) {
	missed := occurrences - 1
	switch def.CatchUp {
	case Skip:
		if missed > 0 {
			// Fell behind: this work's value was purely current, so abandon it.
			return 0, 0, occurrences
		}
		return 1, 0, 0
	case RunAll:
		if occurrences > def.MaxCatchUp {
			// A shop closed a month with a 30-second job accumulates ~86,000 occurrences.
			// Running them all would make the app unusable at 8am — the opposite of what a
			// scheduler is for. Dropping them silently would hide a real problem, so the
			// excess is counted and logged by the caller.
			return def.MaxCatchUp, occurrences - def.MaxCatchUp, 0
		}
		return occurrences, 0, 0
	default: // RunOnce
		return 1, 0, 0
	}
}

// occurrenceStep is the spacing between consecutive occurrences, used to label catch-up runs
// with the time they represent. Zero for non-interval schedules, which collapses the labels
// onto the first occurrence rather than inventing times.
func occurrenceStep(s Schedule) time.Duration {
	if iv, ok := s.(intervalSchedule); ok {
		return iv.Interval()
	}
	return 0
}

// claim atomically takes ownership of a job's due occurrence.
//
// The guard `next_run_at = ?` is the compare half of a compare-and-swap: if another
// scheduler advanced it first, this UPDATE matches nothing and we back off. Advancing
// next_run_at as part of the claim also means one occurrence cannot be claimed twice.
func (s *Scheduler) claim(ctx context.Context, row jobRow, next, now time.Time) (bool, error) {
	res, err := s.db.Writer(ctx).ExecContext(ctx, `
		UPDATE jobs
		   SET last_run_at = ?, next_run_at = ?, row_version = row_version + 1, updated_at = ?
		 WHERE id = ? AND is_enabled = 1 AND next_run_at = ?`,
		clock.Format(now), clock.Format(next), clock.Format(now),
		row.id.String(), clock.Format(row.nextRunAt))
	if err != nil {
		return false, errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeClaimFailed, "claiming job "+row.key)
	}
	n, err := res.RowsAffected()
	if err != nil {
		// Without the count we cannot know whether we won. Reporting "not claimed" costs one
		// tick; guessing "claimed" could run a singleton job twice.
		return false, nil //nolint:nilerr // unknown claim outcome is treated as contended
	}
	return n == 1, nil
}

// launchSeries runs one job's occurrences on the bounded pool, SEQUENTIALLY.
//
// Sequential is not an implementation detail. Launching catch-up occurrences concurrently
// broke two guarantees at once: RunAll promises them "in order", and a singleton job would
// have had N copies of itself running simultaneously — the precise thing `is_singleton`
// exists to prevent ("no two backups at once", §24.2).
//
// One pool slot is held for the whole series, so a job catching up on fifty occurrences
// occupies one worker rather than starving the others. The pool is bounded at all because
// this shares a machine with the UI and a single-writer database: an unbounded burst would
// starve the POS.
func (s *Scheduler) launchSeries(ctx context.Context, row jobRow, e entry, occurrences []time.Time, trigger string) {
	if len(occurrences) == 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case s.sem <- struct{}{}:
			defer func() { <-s.sem }()
		case <-ctx.Done():
			return
		}
		for _, occurrence := range occurrences {
			if ctx.Err() != nil {
				return
			}
			s.runWithRetries(ctx, row, e, occurrence, trigger)
		}
	}()
}

// ── lifecycle ───────────────────────────────────────────────────────────────────

// Start reconciles, reclaims runs abandoned by a previous process, and begins ticking.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errs.Conflict(CodeInvalidDef, "the scheduler is already running")
	}
	s.stop = make(chan struct{})
	s.running = true
	stop := s.stop
	s.mu.Unlock()

	if _, err := s.Reconcile(ctx); err != nil {
		return err
	}
	if _, err := s.ReclaimAbandonedRuns(ctx); err != nil {
		return err
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.opts.TickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := s.Tick(ctx); err != nil {
					s.log.ErrorContext(ctx, "scheduler tick failed", slog.Any("error", err))
				}
			}
		}
	}()
	return nil
}

// Stop halts claiming and drains in-flight runs.
//
// Anything still running when the grace period expires is marked `cancelled` rather than
// abandoned, so the next startup finds no phantom leases to reclaim.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	close(s.stop)
	s.running = false
	s.mu.Unlock()

	drained := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(drained)
	}()

	select {
	case <-drained:
		return nil
	case <-time.After(s.opts.ShutdownGrace):
		s.log.WarnContext(ctx, "shutdown grace expired; marking in-flight runs cancelled",
			slog.Duration("grace", s.opts.ShutdownGrace))
		return s.cancelRunningRuns(ctx)
	}
}

// RunNow triggers a job immediately, outside its schedule, recording the run as manual.
// The diagnostics panel's "run now" button, and a useful test seam.
func (s *Scheduler) RunNow(ctx context.Context, jobKey string) error {
	e, ok := s.reg.lookup(jobKey)
	if !ok {
		return errs.NotFound(CodeUnknownJob, "no job is declared under that key").
			WithParam("key", jobKey)
	}
	row, err := s.loadJobRow(ctx, jobKey)
	if err != nil {
		return err
	}
	s.runWithRetries(ctx, row, e, s.clk.Now(), TriggerManual)
	return nil
}
