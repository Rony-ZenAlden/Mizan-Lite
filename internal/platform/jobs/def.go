// Package jobs is the durable job scheduler.
//
// It is persisted rather than in-memory for one reason, stated in ARCHITECTURE_v1 §24.1:
// "a desktop app is closed every evening, so in-memory scheduling loses work." A server
// process runs for months; Mizan is switched off at 8pm and reopened at 8am, and anything a
// ticker would have fired in between simply did not happen.
//
// That makes "what should happen to occurrences missed while the app was closed?" the central
// question of this package, not an edge case — answered per job by a CatchUpPolicy.
//
// Every attempt is recorded in job_runs, because §24.2 is explicit that "invisible background
// failures are how customers lose backups without knowing".
package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable error codes. They double as i18n keys and are part of the public contract —
// never rename once shipped.
const (
	CodeInvalidDef      = "jobs.invalid_definition"
	CodeDuplicateJob    = "jobs.duplicate_key"
	CodeUnknownJob      = "jobs.unknown_key"
	CodeInvalidSchedule = "jobs.invalid_schedule"
	CodeReconcileFailed = "jobs.reconcile_failed"
	CodeClaimFailed     = "jobs.claim_failed"
	CodeRunFailed       = "jobs.run_failed"
)

// Run statuses, matching the CHECK constraint on job_runs.status in 0001_platform.sql.
const (
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusTimeout   = "timeout"
	StatusCancelled = "cancelled"
)

// Trigger values, matching the CHECK constraint on job_runs.triggered_by.
const (
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
	TriggerStartup  = "startup"
)

// CatchUpPolicy decides what happens to occurrences missed while the app was closed
// (ARCHITECTURE_v1 §24.1): "a missed backup should run; three missed rate fetches should
// collapse to one."
type CatchUpPolicy string

const (
	// RunOnce collapses any number of missed occurrences into a single run. The right
	// default: it neither loses the work nor repeats it pointlessly.
	RunOnce CatchUpPolicy = "run_once"
	// Skip abandons missed occurrences entirely and resumes on the next scheduled time.
	// For work whose value is purely current — cache warming, log cleanup.
	Skip CatchUpPolicy = "skip"
	// RunAll runs once per missed occurrence, in order, bounded by MaxCatchUp. For jobs
	// where each occurrence has distinct work.
	RunAll CatchUpPolicy = "run_all"
)

// Valid reports whether p is a defined policy.
func (p CatchUpPolicy) Valid() bool {
	switch p {
	case RunOnce, Skip, RunAll:
		return true
	default:
		return false
	}
}

// ── schedules ───────────────────────────────────────────────────────────────────

// Schedule computes when a job should next run.
//
// An interface with one implementation today (Every). Standard 5-field cron is deferred to
// the first job that actually needs it — Phase 0's jobs are all intervals, and adding a cron
// dependency would break the fully-offline build constraint for no present benefit
// (design D1).
type Schedule interface {
	// Next returns the first scheduled instant strictly after `after`.
	Next(after time.Time) time.Time
	// String is the durable form written to jobs.schedule_cron.
	String() string
}

type everySchedule struct{ d time.Duration }

// Every returns a fixed-interval schedule.
//
// Stored as "@every 30s", which is robfig/cron's own syntax for interval schedules. That is
// not a workaround: it means every stored value stays valid if a full cron parser is adopted
// later, so the migration path costs nothing.
func Every(d time.Duration) Schedule { return everySchedule{d: d} }

func (e everySchedule) Next(after time.Time) time.Time { return after.Add(e.d) }

func (e everySchedule) String() string { return "@every " + e.d.String() }

// Interval exposes the underlying duration, which catch-up needs in order to count missed
// occurrences.
func (e everySchedule) Interval() time.Duration { return e.d }

// intervalSchedule is implemented by schedules with a fixed period. Counting missed
// occurrences is exact for these; other schedule kinds fall back to enumeration.
type intervalSchedule interface{ Interval() time.Duration }

// ParseSchedule reads the durable form back. It exists so a value round-trips through
// jobs.schedule_cron, and so a future cron parser has one obvious place to be added.
func ParseSchedule(s string) (Schedule, error) {
	trimmed := strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(trimmed, "@every "); ok {
		d, err := time.ParseDuration(strings.TrimSpace(rest))
		if err != nil {
			return nil, errs.Wrap(err, errs.CategoryValidation, CodeInvalidSchedule,
				"schedule is not a valid duration").WithParam("schedule", s)
		}
		if d <= 0 {
			return nil, errs.Validation(CodeInvalidSchedule,
				"schedule interval must be positive").WithParam("schedule", s)
		}
		return Every(d), nil
	}
	return nil, errs.Validation(CodeInvalidSchedule,
		"only @every schedules are supported in this version").WithParam("schedule", s)
}

// ── definitions ─────────────────────────────────────────────────────────────────

// Default values applied to a Def when it leaves a field zero.
const (
	DefaultTimeout     = 5 * time.Minute
	DefaultMaxAttempts = 3
	DefaultMaxCatchUp  = 50
)

// Def declares a job. The owning module states it once; the jobs table holds its state.
type Def struct {
	// Key is the stable identifier, e.g. "outbox.dispatch". It is the durable link between
	// code and the jobs row, so renaming one orphans its history.
	Key string
	// Schedule decides when the job is due.
	Schedule Schedule
	// Timeout bounds a single run. Zero uses DefaultTimeout.
	Timeout time.Duration
	// MaxAttempts caps retries within one occurrence. Zero uses DefaultMaxAttempts.
	MaxAttempts int
	// CatchUp decides what happens to missed occurrences. Zero uses RunOnce.
	CatchUp CatchUpPolicy
	// MaxCatchUp bounds RunAll. Zero uses the scheduler's default (50).
	//
	// Per-job rather than global because the right bound depends on what an occurrence
	// costs: fifty missed nightly backups is meaningless, fifty missed five-second ticks is
	// four minutes of work.
	MaxCatchUp int
	// AllowConcurrent permits overlapping runs.
	//
	// Inverted from the design's `Singleton bool` so that the ZERO VALUE IS THE SAFE ONE: a
	// job declared without thinking about concurrency is a singleton, and "no two backups at
	// once" (§24.2) holds by default rather than by remembering.
	AllowConcurrent bool
	// Description is an i18n key, never prose (ARCHITECTURE_v1 §22.2).
	Description string
}

func (d Def) withDefaults() Def {
	if d.Timeout <= 0 {
		d.Timeout = DefaultTimeout
	}
	if d.MaxAttempts <= 0 {
		d.MaxAttempts = DefaultMaxAttempts
	}
	if d.CatchUp == "" {
		d.CatchUp = RunOnce
	}
	if d.MaxCatchUp <= 0 {
		d.MaxCatchUp = DefaultMaxCatchUp
	}
	return d
}

func (d Def) validate() error {
	if strings.TrimSpace(d.Key) == "" {
		return errs.Validation(CodeInvalidDef, "a job needs a key")
	}
	if d.Schedule == nil {
		return errs.Validation(CodeInvalidDef, "a job needs a schedule").WithParam("key", d.Key)
	}
	if !d.CatchUp.Valid() {
		return errs.Validation(CodeInvalidDef, "unknown catch-up policy").
			WithParam("key", d.Key).WithParam("policy", string(d.CatchUp))
	}
	return nil
}

// Handler performs one run of a job.
//
// It must respect ctx: cancellation is how a timeout and a graceful shutdown reach a running
// job. A handler that ignores it is recorded as `timeout` and holds a worker until it
// returns.
type Handler func(ctx context.Context, run RunContext) error

// RunContext describes the run in progress.
type RunContext struct {
	JobKey      string
	RunID       id.ID
	Attempt     int
	TriggeredBy string
	// Occurrence is the scheduled instant this run represents. During catch-up it is the
	// missed time, not now — a job that stamps records with it stays historically accurate.
	Occurrence time.Time

	progress func(percent int, note string)
	output   func(json string)
}

// Progress reports how far along the run is.
//
// Step 0.10 forwards these to Wails events so a long job shows movement instead of appearing
// hung: "a POS that freezes during a report is not acceptable" (§24.4).
func (r RunContext) Progress(percent int, note string) {
	if r.progress != nil {
		r.progress(percent, note)
	}
}

// SetOutput records a structured result, stored in job_runs.output_json.
//
// This is what makes the diagnostics panel useful rather than a list of green ticks: the
// dispatcher records how many events it delivered, a backup records the file it wrote.
func (r RunContext) SetOutput(jsonValue string) {
	if r.output != nil {
		r.output(jsonValue)
	}
}

// enumerationGuard bounds the loop for schedules that must be enumerated rather than
// computed. It is deliberately far larger than any MaxCatchUp: it exists only so a
// pathological schedule cannot spin forever, NOT to apply the catch-up cap.
//
// Conflating the two was a real bug: capping the count at MaxCatchUp+1 made `Dropped` report
// 1 instead of 3595, hiding exactly the "how far behind were we?" signal the cap exists to
// surface.
const enumerationGuard = 1_000_000

// missedOccurrences counts scheduled instants in (from, now], i.e. how many times the job
// should have run, including the one that is due right now.
//
// Exact for fixed-interval schedules; other kinds enumerate, bounded by cap.
func missedOccurrences(s Schedule, from, now time.Time, guard int) int {
	if !from.Before(now) && !from.Equal(now) {
		return 0
	}
	if iv, ok := s.(intervalSchedule); ok && iv.Interval() > 0 {
		elapsed := now.Sub(from)
		n := int(elapsed/iv.Interval()) + 1
		if n > guard {
			return guard
		}
		return n
	}
	n := 0
	for t := from; !t.After(now) && n < guard; t = s.Next(t) {
		n++
	}
	return n
}

func fmtRunError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v", err)
}
