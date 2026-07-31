package jobs

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mizan-erp/mizan/internal/platform/outbox"
)

// The two jobs Phase 0 registers (PHASE_0_FOUNDATION §JOB): "exactly one real job — the
// outbox dispatcher — plus a heartbeat job that proves scheduling, catch-up, and the UI
// panel work."

// Job keys are part of the durable record: a jobs row is linked to code by this string, so
// renaming one orphans its run history.
const (
	KeyOutboxDispatch = "outbox.dispatch"
	KeyHeartbeat      = "platform.heartbeat"
)

// RegisterOutboxDispatch declares the job that drains the transactional outbox.
//
// This closes the item Step 0.6 carried forward: DispatchOnce was deliberately built as a
// callable unit with no loop of its own, so that the loop could be this scheduler rather than
// a second, parallel scheduling mechanism inside the outbox package.
//
// CatchUp is RunOnce, not Skip: after the app has been closed overnight there are pending
// deliveries waiting, and they should go out at 8am rather than one tick later. The
// occurrences themselves carry no distinct work — the pending rows do — so collapsing many
// missed ticks into one run is exactly right.
func RegisterOutboxDispatch(r *Registry, d *outbox.Dispatcher, every time.Duration) error {
	if every <= 0 {
		every = 5 * time.Second
	}
	return r.Register(Def{
		Key:         KeyOutboxDispatch,
		Schedule:    Every(every),
		Timeout:     2 * time.Minute,
		MaxAttempts: 2,
		CatchUp:     RunOnce,
		Description: "jobs.outbox_dispatch",
	}, func(ctx context.Context, run RunContext) error {
		rep, err := d.DispatchOnce(ctx)
		if err != nil {
			return err
		}
		// Recorded so the diagnostics panel shows what the run actually did, rather than a
		// green tick that is indistinguishable from a no-op.
		if encoded, marshalErr := json.Marshal(rep); marshalErr == nil {
			run.SetOutput(string(encoded))
		}
		return nil
	})
}

// HeartbeatOutput is what a heartbeat run records.
type HeartbeatOutput struct {
	At      string `json:"at"`
	Attempt int    `json:"attempt"`
}

// RegisterHeartbeat declares a job that does nothing but prove the machinery works.
//
// It exists for two reasons. It exercises the whole path — reconciliation, claiming,
// execution, run recording — on every install, so a scheduler that has silently stopped is
// visible. And it gives the Phase 9 diagnostics panel something to display on a healthy
// system, which is what makes "no runs recorded" read as a fault rather than as normal.
//
// CatchUp is RunOnce, not Skip. Skip was the first choice — "a missed heartbeat carries no
// information" — but it is wrong for a liveness signal: Skip drops the occurrence whenever a
// tick arrives a full interval late, which is exactly when the machine is loaded and a
// heartbeat is most worth having. RunOnce collapses any number of missed beats into a single
// "still alive, now", which is what the signal actually means.
func RegisterHeartbeat(r *Registry, every time.Duration) error {
	if every <= 0 {
		every = time.Minute
	}
	return r.Register(Def{
		Key:         KeyHeartbeat,
		Schedule:    Every(every),
		Timeout:     30 * time.Second,
		MaxAttempts: 1,
		CatchUp:     RunOnce,
		Description: "jobs.heartbeat",
	}, func(_ context.Context, run RunContext) error {
		encoded, err := json.Marshal(HeartbeatOutput{
			At:      run.Occurrence.UTC().Format(time.RFC3339),
			Attempt: run.Attempt,
		})
		if err != nil {
			return err
		}
		run.SetOutput(string(encoded))
		return nil
	})
}
