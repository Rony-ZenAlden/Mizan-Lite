package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// jobRow is the durable state of one job.
type jobRow struct {
	id        id.ID
	key       string
	nextRunAt time.Time
	timeout   time.Duration
	enabled   bool
}

func (s *Scheduler) loadJobRows(ctx context.Context) (map[string]jobRow, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx,
		`SELECT id, job_key, next_run_at, timeout_seconds, is_enabled FROM jobs`)
	if err != nil {
		return nil, errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeReconcileFailed, "loading jobs")
	}
	defer rows.Close()

	out := map[string]jobRow{}
	for rows.Next() {
		row, scanErr := scanJobRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out[row.key] = row
	}
	return out, rows.Err()
}

func (s *Scheduler) loadDueJobs(ctx context.Context, now time.Time) ([]jobRow, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, job_key, next_run_at, timeout_seconds, is_enabled
		  FROM jobs
		 WHERE is_enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?
		 ORDER BY next_run_at
		 LIMIT ?`, clock.Format(now), s.opts.BatchSize)
	if err != nil {
		return nil, errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeClaimFailed, "loading due jobs")
	}
	defer rows.Close()

	var out []jobRow
	for rows.Next() {
		row, scanErr := scanJobRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Scheduler) loadJobRow(ctx context.Context, key string) (jobRow, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, job_key, next_run_at, timeout_seconds, is_enabled
		  FROM jobs WHERE job_key = ?`, key)
	if err != nil {
		return jobRow{}, errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeUnknownJob, "loading job "+key)
	}
	defer rows.Close()
	if !rows.Next() {
		return jobRow{}, errs.NotFound(CodeUnknownJob, "no such job row").WithParam("key", key)
	}
	return scanJobRow(rows)
}

type scanner interface{ Scan(dest ...any) error }

func scanJobRow(rows scanner) (jobRow, error) {
	var (
		row       jobRow
		rawID     string
		nextRunAt *string
		timeoutS  int64
		enabled   int64
	)
	if err := rows.Scan(&rawID, &row.key, &nextRunAt, &timeoutS, &enabled); err != nil {
		return jobRow{}, errs.Wrap(err, errs.CategoryInternal, CodeReconcileFailed, "scanning job")
	}
	row.id = id.ID(rawID)
	row.timeout = time.Duration(timeoutS) * time.Second
	row.enabled = enabled != 0
	if nextRunAt != nil {
		row.nextRunAt, _ = clock.ParseTimestamp(*nextRunAt)
	}
	return row, nil
}

// hasLiveRun reports whether a job has a run holding an unexpired lease.
//
// The lease is started_at + the job's timeout — no extra column needed (design D2). A run
// past it belongs to a process that died; ReclaimAbandonedRuns will close it out, and in the
// meantime it must not block the job forever.
func (s *Scheduler) hasLiveRun(ctx context.Context, jobID id.ID, timeout time.Duration) (bool, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx,
		`SELECT started_at FROM job_runs WHERE job_id = ? AND status = ?`,
		jobID.String(), StatusRunning)
	if err != nil {
		return false, errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeClaimFailed, "checking for a live run")
	}
	defer rows.Close()

	now := s.clk.Now()
	for rows.Next() {
		var startedAt string
		if scanErr := rows.Scan(&startedAt); scanErr != nil {
			return false, errs.Wrap(scanErr, errs.CategoryInternal, CodeClaimFailed, "scanning run")
		}
		started, ok := clock.ParseTimestamp(startedAt)
		if !ok {
			continue
		}
		if now.Before(started.Add(timeout)) {
			return true, nil
		}
	}
	return false, rows.Err()
}

// ── execution ───────────────────────────────────────────────────────────────────

// runWithRetries executes one occurrence, retrying within the occurrence up to MaxAttempts.
//
// Retries are in-process rather than persisted. The trade-off is deliberate: a retry lost to
// an app closing is not a lost job, because these are PERIODIC — the next occurrence runs the
// same work. Persisting retry state would add a second scheduling mechanism alongside
// next_run_at for very little gain.
func (s *Scheduler) runWithRetries(ctx context.Context, row jobRow, e entry, occurrence time.Time, trigger string) {
	for attempt := 1; attempt <= e.def.MaxAttempts; attempt++ {
		status, err := s.runOnce(ctx, row, e, occurrence, trigger, attempt)
		if status == StatusSucceeded {
			return
		}
		// A cancelled run means the app is shutting down; retrying would fight the drain.
		if status == StatusCancelled || ctx.Err() != nil {
			return
		}
		if attempt == e.def.MaxAttempts {
			s.log.ErrorContext(ctx, "job failed after exhausting its attempts",
				slog.String("job_key", row.key),
				slog.Int("attempts", attempt), slog.Any("error", err))
			return
		}
		select {
		case <-time.After(s.retryBackoff(attempt)):
		case <-ctx.Done():
			return
		}
	}
}

// runOnce performs a single attempt and records it.
func (s *Scheduler) runOnce(
	ctx context.Context, row jobRow, e entry, occurrence time.Time, trigger string, attempt int,
) (string, error) {
	runID, err := s.startRun(ctx, row.id, attempt, trigger)
	if err != nil {
		return StatusFailed, err
	}

	var (
		outputMu sync.Mutex
		output   string
	)
	runCtx := RunContext{
		JobKey: row.key, RunID: runID, Attempt: attempt,
		TriggeredBy: trigger, Occurrence: occurrence,
		progress: func(percent int, note string) {
			s.log.DebugContext(ctx, "job progress",
				slog.String("job_key", row.key),
				slog.Int("percent", percent), slog.String("note", note))
		},
		output: func(v string) {
			outputMu.Lock()
			defer outputMu.Unlock()
			output = v
		},
	}

	// The timeout is what stops a hung handler consuming a worker forever. A handler that
	// ignores cancellation still holds its goroutine — nothing can prevent that — but the run
	// is recorded as `timeout` so the failure is visible rather than silent.
	timeoutCtx, cancel := context.WithTimeout(ctx, e.def.Timeout)
	defer cancel()

	runErr := invokeSafely(timeoutCtx, row.key, runCtx, e.handler)

	status := StatusSucceeded
	switch {
	case runErr == nil:
	case errors.Is(timeoutCtx.Err(), context.DeadlineExceeded):
		status = StatusTimeout
	case ctx.Err() != nil:
		status = StatusCancelled
	default:
		status = StatusFailed
	}

	outputMu.Lock()
	finalOutput := output
	outputMu.Unlock()

	// Recorded with a background context: a cancelled or timed-out run must still be written
	// down, and using the dead context would silently lose exactly the runs worth seeing.
	if err := s.finishRun(context.WithoutCancel(ctx), runID, status, runErr, finalOutput); err != nil {
		s.log.ErrorContext(ctx, "could not record the outcome of a job run",
			slog.String("job_key", row.key), slog.Any("error", err))
	}
	return status, runErr
}

// invokeSafely converts a handler panic into an ordinary failed run.
//
// A bad job must not take down an app that is ringing up a sale; it becomes a recorded
// failure, retried and eventually given up on like any other.
func invokeSafely(ctx context.Context, key string, run RunContext, h Handler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errs.Internal(CodeRunFailed, fmt.Sprintf("job %q panicked: %v", key, r))
		}
	}()
	return h(ctx, run)
}

func (s *Scheduler) retryBackoff(attempt int) time.Duration {
	delay := s.opts.RetryBackoff
	for i := 1; i < attempt && delay < s.opts.MaxRetryBackoff; i++ {
		delay *= 2
	}
	if delay > s.opts.MaxRetryBackoff {
		delay = s.opts.MaxRetryBackoff
	}
	// Jitter, for the same reason as the outbox: jobs failing for one environmental cause
	// must not retry in perfect lockstep against the resource that is already struggling.
	if spread := delay / 4; spread > 0 {
		delay += time.Duration(rand.Int64N(int64(2*spread))) - spread //nolint:gosec // jitter, not cryptography
	}
	return delay
}

// ── run records ─────────────────────────────────────────────────────────────────

func (s *Scheduler) startRun(ctx context.Context, jobID id.ID, attempt int, trigger string) (id.ID, error) {
	runID, err := id.New()
	if err != nil {
		return "", err
	}
	if _, err := s.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO job_runs (id, job_id, started_at, status, attempt, triggered_by)
		VALUES (?, ?, ?, ?, ?, ?)`,
		runID.String(), jobID.String(), clock.Format(s.clk.Now()),
		StatusRunning, int64(attempt), trigger,
	); err != nil {
		return "", errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeRunFailed, "recording the start of a job run")
	}
	return runID, nil
}

func (s *Scheduler) finishRun(ctx context.Context, runID id.ID, status string, runErr error, output string) error {
	var errText, outputText any
	if runErr != nil {
		errText = fmtRunError(runErr)
	}
	if output != "" {
		outputText = output
	}
	if _, err := s.db.Writer(ctx).ExecContext(ctx, `
		UPDATE job_runs SET finished_at = ?, status = ?, error = ?, output_json = ?
		 WHERE id = ?`,
		clock.Format(s.clk.Now()), status, errText, outputText, runID.String(),
	); err != nil {
		return errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeRunFailed, "recording the outcome of a job run")
	}
	return nil
}

// ReclaimAbandonedRuns closes out runs left `running` by a process that died.
//
// Without it a singleton job would never run again: its lease is held by a run that will
// never finish. This is the third use of the expired-lease pattern — migration lock (0.4),
// outbox visibility timeout (0.6), and here — which is deliberate: a desktop app is killed
// mid-operation routinely, and every durable claim needs an answer for it.
func (s *Scheduler) ReclaimAbandonedRuns(ctx context.Context) (int, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `
		SELECT r.id, r.started_at, j.timeout_seconds, j.job_key
		  FROM job_runs r JOIN jobs j ON j.id = r.job_id
		 WHERE r.status = ?`, StatusRunning)
	if err != nil {
		return 0, errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeRunFailed, "loading in-flight runs")
	}

	type stale struct {
		runID  string
		jobKey string
	}
	var expired []stale
	now := s.clk.Now()
	for rows.Next() {
		var runID, startedAt, jobKey string
		var timeoutS int64
		if scanErr := rows.Scan(&runID, &startedAt, &timeoutS, &jobKey); scanErr != nil {
			rows.Close()
			return 0, errs.Wrap(scanErr, errs.CategoryInternal, CodeRunFailed, "scanning in-flight run")
		}
		started, ok := clock.ParseTimestamp(startedAt)
		// The lease arithmetic is done in Go rather than SQL: timestamps are portable
		// CHAR(24) text, and date arithmetic on them differs per engine — exactly what the
		// dialect contract exists to avoid.
		if !ok || now.After(started.Add(time.Duration(timeoutS)*time.Second)) {
			expired = append(expired, stale{runID: runID, jobKey: jobKey})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, errs.Wrap(err, errs.CategoryInternal, CodeRunFailed, "reading in-flight runs")
	}
	rows.Close()

	for _, e := range expired {
		if _, err := s.db.Writer(ctx).ExecContext(ctx, `
			UPDATE job_runs SET status = ?, finished_at = ?, error = ? WHERE id = ?`,
			StatusTimeout, clock.Format(now),
			"the process ended before this run finished", e.runID,
		); err != nil {
			return 0, errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
				CodeRunFailed, "reclaiming an abandoned run")
		}
		s.log.WarnContext(ctx, "reclaimed a job run abandoned by a previous process",
			slog.String("job_key", e.jobKey))
	}
	return len(expired), nil
}

// cancelRunningRuns marks anything still in flight as cancelled at shutdown, so the next
// startup finds no phantom leases.
func (s *Scheduler) cancelRunningRuns(ctx context.Context) error {
	if _, err := s.db.Writer(ctx).ExecContext(ctx, `
		UPDATE job_runs SET status = ?, finished_at = ?, error = ? WHERE status = ?`,
		StatusCancelled, clock.Format(s.clk.Now()),
		"cancelled by application shutdown", StatusRunning,
	); err != nil {
		return errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeRunFailed, "cancelling in-flight runs at shutdown")
	}
	return nil
}

// ── query API (for the Phase 9 diagnostics panel) ───────────────────────────────

// RunRecord is one recorded attempt.
type RunRecord struct {
	RunID       id.ID
	JobKey      string
	StartedAt   time.Time
	FinishedAt  time.Time
	Status      string
	Attempt     int
	Error       string
	Output      string
	TriggeredBy string
}

// RecentRuns returns the most recent runs, newest first. An empty jobKey returns runs for
// every job.
//
// Built now because §24.2 is explicit that unrecorded background failure is how customers
// lose backups without noticing — the panel that reads this is Phase 9, but the data has to
// be there from the first run.
func (s *Scheduler) RecentRuns(ctx context.Context, jobKey string, limit int) ([]RunRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT r.id, j.job_key, r.started_at, r.finished_at, r.status, r.attempt,
		       r.error, r.output_json, r.triggered_by
		  FROM job_runs r JOIN jobs j ON j.id = r.job_id`
	args := []any{}
	if jobKey != "" {
		query += ` WHERE j.job_key = ?`
		args = append(args, jobKey)
	}
	query += ` ORDER BY r.started_at DESC, r.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeRunFailed, "loading job runs")
	}
	defer rows.Close()

	var out []RunRecord
	for rows.Next() {
		var (
			rec                          RunRecord
			rawID, startedAt             string
			finishedAt, errText, outText *string
			attempt                      int64
		)
		if scanErr := rows.Scan(&rawID, &rec.JobKey, &startedAt, &finishedAt, &rec.Status,
			&attempt, &errText, &outText, &rec.TriggeredBy); scanErr != nil {
			return nil, errs.Wrap(scanErr, errs.CategoryInternal, CodeRunFailed, "scanning job run")
		}
		rec.RunID = id.ID(rawID)
		rec.Attempt = int(attempt)
		rec.StartedAt, _ = clock.ParseTimestamp(startedAt)
		if finishedAt != nil {
			rec.FinishedAt, _ = clock.ParseTimestamp(*finishedAt)
		}
		if errText != nil {
			rec.Error = *errText
		}
		if outText != nil {
			rec.Output = *outText
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// JobState is the current state of one declared job.
type JobState struct {
	Key        string
	Enabled    bool
	Schedule   string
	LastRunAt  time.Time
	NextRunAt  time.Time
	LastStatus string
}

// JobStates returns every job row with its most recent outcome, ordered by key.
func (s *Scheduler) JobStates(ctx context.Context) ([]JobState, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `
		SELECT j.job_key, j.is_enabled, j.schedule_cron, j.last_run_at, j.next_run_at,
		       (SELECT r.status FROM job_runs r
		         WHERE r.job_id = j.id ORDER BY r.started_at DESC, r.id DESC LIMIT 1)
		  FROM jobs j
		 ORDER BY j.job_key`)
	if err != nil {
		return nil, errs.Wrap(s.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeRunFailed, "loading job states")
	}
	defer rows.Close()

	var out []JobState
	for rows.Next() {
		var (
			st                             JobState
			enabled                        int64
			schedule, last, next, lastStat *string
		)
		if scanErr := rows.Scan(&st.Key, &enabled, &schedule, &last, &next, &lastStat); scanErr != nil {
			return nil, errs.Wrap(scanErr, errs.CategoryInternal, CodeRunFailed, "scanning job state")
		}
		st.Enabled = enabled != 0
		if schedule != nil {
			st.Schedule = *schedule
		}
		if last != nil {
			st.LastRunAt, _ = clock.ParseTimestamp(*last)
		}
		if next != nil {
			st.NextRunAt, _ = clock.ParseTimestamp(*next)
		}
		if lastStat != nil {
			st.LastStatus = *lastStat
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
