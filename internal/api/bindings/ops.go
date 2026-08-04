package bindings

import (
	"time"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
)

// JobStateDTO is one scheduled job and its most recent outcome.
type JobStateDTO struct {
	Key      string `json:"key"`
	Enabled  bool   `json:"enabled"`
	Schedule string `json:"schedule"`
	// Timestamps are the portable CHAR(24) UTC form (kernel/clock), empty when never run.
	// The frontend formats them with Intl for the active locale; the backend never formats
	// a date for display, because a formatted date is prose (§22.2).
	LastRunAt  string `json:"lastRunAt"`
	NextRunAt  string `json:"nextRunAt"`
	LastStatus string `json:"lastStatus"`
}

// RunRecordDTO is one execution of a job.
type RunRecordDTO struct {
	RunID      string `json:"runId"`
	JobKey     string `json:"jobKey"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
	Status     string `json:"status"`
	Attempt    int    `json:"attempt"`
	// Error is the recorded failure text. It is developer-facing and shown only in the
	// diagnostics panel, which is an operator surface — not a customer-facing screen.
	Error       string `json:"error"`
	Output      string `json:"output"`
	TriggeredBy string `json:"triggeredBy"`
}

// Ops exposes background-job state for the diagnostics panel.
//
// §24.2 is the reason this exists in Phase 0 rather than Phase 9: "invisible background
// failures are how customers lose backups without knowing." The query API was built and tested
// in 0.7; this is the first thing to read it.
type Ops struct{ graph }

// Jobs returns every declared job with its schedule and last outcome.
func (o *Ops) Jobs() envelope.Result[[]JobStateDTO] {
	app, ok := o.resolve()
	if !ok {
		return envelope.Fail[[]JobStateDTO](notReady())
	}

	states, err := app.Scheduler.JobStates(app.Context())
	if err != nil {
		return envelope.Fail[[]JobStateDTO](err)
	}

	out := make([]JobStateDTO, 0, len(states))
	for _, s := range states {
		out = append(out, JobStateDTO{
			Key:        s.Key,
			Enabled:    s.Enabled,
			Schedule:   s.Schedule,
			LastRunAt:  formatTime(s.LastRunAt),
			NextRunAt:  formatTime(s.NextRunAt),
			LastStatus: s.LastStatus,
		})
	}
	return envelope.Ok(out)
}

// Runs returns recent executions, newest first. An empty jobKey returns runs for every job.
//
// There is deliberately NO RunNow binding. Triggering a job by hand is an operator action that
// wants a permission and a confirmation, and RBAC does not exist until Phase 1. Shipping it
// unguarded now would mean taking a control away later, which is worse than not having it.
func (o *Ops) Runs(jobKey string, limit int) envelope.Result[[]RunRecordDTO] {
	app, ok := o.resolve()
	if !ok {
		return envelope.Fail[[]RunRecordDTO](notReady())
	}

	records, err := app.Scheduler.RecentRuns(app.Context(), jobKey, limit)
	if err != nil {
		return envelope.Fail[[]RunRecordDTO](err)
	}

	out := make([]RunRecordDTO, 0, len(records))
	for _, r := range records {
		out = append(out, RunRecordDTO{
			RunID:       string(r.RunID),
			JobKey:      r.JobKey,
			StartedAt:   formatTime(r.StartedAt),
			FinishedAt:  formatTime(r.FinishedAt),
			Status:      r.Status,
			Attempt:     r.Attempt,
			Error:       r.Error,
			Output:      r.Output,
			TriggeredBy: r.TriggeredBy,
		})
	}
	return envelope.Ok(out)
}

// formatTime renders a timestamp, or "" for the zero time.
//
// The zero value must not become "0001-01-01T00:00:00.000Z": a job that has never run would
// then display as having run two thousand years ago. Empty is the honest answer and the
// frontend renders it as a dash.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return clock.Format(t)
}
