package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// DispatcherOptions configures a Dispatcher. Every duration has a defensible default; a
// zero value works.
type DispatcherOptions struct {
	// BatchSize caps how many deliveries one pass claims. Default 50.
	BatchSize int
	// MaxAttempts before a delivery is dead-lettered. Default 8.
	MaxAttempts int
	// BaseBackoff is the first retry delay; it doubles per attempt. Default 5s.
	BaseBackoff time.Duration
	// MaxBackoff caps the delay. Default 15m.
	MaxBackoff time.Duration
	// VisibilityTimeout is how long a claimed delivery may stay 'processing' before it is
	// presumed abandoned by a crashed dispatcher and reclaimed. Default 5m.
	VisibilityTimeout time.Duration
	Clock             clock.Clock
	Logger            *slog.Logger
}

func (o DispatcherOptions) withDefaults() DispatcherOptions {
	if o.BatchSize <= 0 {
		o.BatchSize = 50
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 8
	}
	if o.BaseBackoff <= 0 {
		o.BaseBackoff = 5 * time.Second
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = 15 * time.Minute
	}
	if o.VisibilityTimeout <= 0 {
		o.VisibilityTimeout = 5 * time.Minute
	}
	if o.Clock == nil {
		o.Clock = clock.System()
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o
}

// Dispatcher delivers outbox events to their registered handlers.
//
// It is a callable unit, not a loop: Step 0.7 wraps DispatchOnce in a durable job with a
// schedule and a heartbeat. Keeping the two apart means this step's correctness is provable
// on its own, and the scheduler is then added to something already known to work.
type Dispatcher struct {
	db   Database
	subs *Subscribers
	opts DispatcherOptions
}

// Database is what the dispatcher needs: executor resolution plus the Unit of Work.
type Database interface {
	database.DB
	database.UnitOfWork
}

// NewDispatcher builds a Dispatcher.
func NewDispatcher(db Database, subs *Subscribers, opts DispatcherOptions) *Dispatcher {
	return &Dispatcher{db: db, subs: subs, opts: opts.withDefaults()}
}

// Report summarises one dispatch pass.
type Report struct {
	Delivered  int // handler succeeded
	Failed     int // handler errored or panicked; will be retried
	DeadLetter int // exhausted MaxAttempts; needs a human
	Reclaimed  int // abandoned 'processing' rows returned to 'pending'
	Skipped    int // no handler registered under that name any more
	Contended  int // another dispatcher claimed it first
}

// DispatchOnce performs a single pass: reclaim abandoned work, claim what is due, deliver it.
func (d *Dispatcher) DispatchOnce(ctx context.Context) (Report, error) {
	var rep Report

	reclaimed, err := d.reclaimAbandoned(ctx)
	if err != nil {
		return rep, err
	}
	rep.Reclaimed = reclaimed

	due, err := d.claim(ctx)
	if err != nil {
		return rep, err
	}
	for _, item := range due {
		outcome, err := d.deliver(ctx, item)
		if err != nil {
			return rep, err
		}
		switch outcome {
		case outcomeDelivered:
			rep.Delivered++
		case outcomeFailed:
			rep.Failed++
		case outcomeDead:
			rep.DeadLetter++
		case outcomeSkipped:
			rep.Skipped++
		case outcomeContended:
			rep.Contended++
		}
	}
	return rep, nil
}

// reclaimAbandoned returns 'processing' rows whose visibility deadline has passed.
//
// A dispatcher that dies mid-batch leaves rows claimed forever; without this they would never
// be delivered and nothing would report it. next_attempt_at carries the deadline, which is
// why no extra column was needed (design D5).
func (d *Dispatcher) reclaimAbandoned(ctx context.Context) (int, error) {
	now := clock.Format(d.opts.Clock.Now())
	res, err := d.db.Writer(ctx).ExecContext(ctx, `
		UPDATE outbox_deliveries
		   SET status = 'pending', updated_at = ?
		 WHERE status = 'processing'
		   AND next_attempt_at IS NOT NULL
		   AND next_attempt_at < ?`, now, now)
	if err != nil {
		return 0, errs.Wrap(d.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeClaimFailed, "reclaiming abandoned outbox deliveries")
	}
	n, err := res.RowsAffected()
	if err != nil {
		// The UPDATE succeeded; only the affected-row count is unavailable, and that count
		// feeds nothing but the report. Failing the pass here would block dispatch over a
		// statistic.
		return 0, nil //nolint:nilerr // the reclaim happened; only the count is missing
	}
	if n > 0 {
		d.opts.Logger.WarnContext(ctx, "reclaimed outbox deliveries abandoned by a previous run",
			slog.Int64("count", n))
	}
	return int(n), nil
}

type workItem struct {
	env           event.Envelope
	handler       string
	attempts      int64
	aggregateType string
	aggregateID   string
	occurredAt    string
}

// claim selects deliveries that are due, respecting per-(aggregate, handler) ordering.
//
// The correlated NOT EXISTS is the head-of-line rule: a delivery is only eligible when this
// handler has no EARLIER event for the same aggregate still outstanding. Without it, "invoice
// voided" could reach accounting before "invoice posted" — producing a ledger that balances
// and is wrong, which is the hardest kind of error to find.
//
// 'dead' counts as outstanding: if a handler dead-lettered an event, delivering that
// aggregate's later events to it would compound the damage. The stream halts until a human
// resolves it, which is what 'dead' is for.
func (d *Dispatcher) claim(ctx context.Context) ([]workItem, error) {
	now := clock.Format(d.opts.Clock.Now())

	rows, err := d.db.Reader(ctx).QueryContext(ctx, `
		SELECT e.id, e.occurred_at, e.event_type, e.aggregate_type, e.aggregate_id,
		       e.payload_json, e.correlation_id, e.causation_id, e.branch_id,
		       d.handler, d.attempts
		  FROM outbox_deliveries d
		  JOIN outbox_events e ON e.id = d.event_id
		 WHERE d.status IN ('pending', 'failed')
		   AND (d.next_attempt_at IS NULL OR d.next_attempt_at <= ?)
		   AND NOT EXISTS (
		         SELECT 1
		           FROM outbox_deliveries d2
		           JOIN outbox_events e2 ON e2.id = d2.event_id
		          WHERE d2.handler = d.handler
		            AND e2.aggregate_type = e.aggregate_type
		            AND e2.aggregate_id = e.aggregate_id
		            AND d2.status <> 'done'
		            AND (e2.occurred_at < e.occurred_at
		                 OR (e2.occurred_at = e.occurred_at AND e2.id < e.id))
		       )
		 ORDER BY e.occurred_at, e.id
		 LIMIT ?`, now, d.opts.BatchSize)
	if err != nil {
		return nil, errs.Wrap(d.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeClaimFailed, "claiming outbox deliveries")
	}
	defer rows.Close()

	var out []workItem
	for rows.Next() {
		var (
			item                           workItem
			eventID, occurredAt, payload   string
			correlation, causation, branch *string
		)
		if scanErr := rows.Scan(&eventID, &occurredAt, &item.env.Type, &item.aggregateType,
			&item.aggregateID, &payload, &correlation, &causation, &branch,
			&item.handler, &item.attempts); scanErr != nil {
			return nil, errs.Wrap(scanErr, errs.CategoryInternal, CodeClaimFailed,
				"scanning outbox deliveries")
		}
		item.env.ID = id.ID(eventID)
		item.env.OccurredAt = parseTime(occurredAt)
		item.env.AggregateType = item.aggregateType
		item.env.AggregateID = id.ID(item.aggregateID)
		item.env.Payload = []byte(payload)
		item.env.CorrelationID = deref(correlation)
		item.env.CausationID = deref(causation)
		item.env.BranchID = deref(branch)
		item.occurredAt = occurredAt
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Wrap(err, errs.CategoryInternal, CodeClaimFailed, "reading outbox deliveries")
	}
	return out, nil
}

func deref(s *string) id.ID {
	if s == nil {
		return ""
	}
	return id.ID(*s)
}

type outcome int

const (
	outcomeDelivered outcome = iota
	outcomeFailed
	outcomeDead
	outcomeSkipped
	outcomeContended
)

// deliver runs one handler for one event and records the result.
func (d *Dispatcher) deliver(ctx context.Context, item workItem) (outcome, error) {
	handler, ok := d.subs.lookup(item.env.Type, item.handler)
	if !ok {
		// The handler that owned this delivery is no longer registered — a renamed or
		// removed subscriber. Leaving it pending would block the aggregate forever, so it
		// is skipped and reported rather than silently retried until dead.
		d.opts.Logger.WarnContext(ctx, "outbox delivery has no registered handler; skipping",
			slog.String("event_type", item.env.Type), slog.String("handler", item.handler))
		return outcomeSkipped, nil
	}

	// Claiming is a compare-and-swap, not a read-then-write. The candidate list came from a
	// SELECT, and between that SELECT and here another dispatcher may have taken the same
	// row — which would deliver the event twice, the exact duplicate this design exists to
	// prevent. Only the dispatcher whose UPDATE actually changes the row proceeds.
	claimed, err := d.tryClaim(ctx, item)
	if err != nil {
		return outcomeContended, err
	}
	if !claimed {
		return outcomeContended, nil
	}

	// The handler runs OUTSIDE a transaction of ours. It manages its own, so a handler that
	// writes rows commits them independently of this bookkeeping — which is what makes a
	// failure retryable rather than all-or-nothing.
	//
	// CausationID is stamped so anything the handler publishes links back to this event
	// automatically, without the handler author having to remember.
	handlerCtx := event.WithCausationID(
		event.WithCorrelationID(ctx, item.env.CorrelationID), item.env.ID)

	err = invokeSafely(handlerCtx, item.handler, item.env, handler)
	if err == nil {
		if markErr := d.markDone(ctx, item); markErr != nil {
			return outcomeDelivered, markErr
		}
		return outcomeDelivered, nil
	}

	attempts := item.attempts + 1
	if attempts >= int64(d.opts.MaxAttempts) {
		d.opts.Logger.ErrorContext(ctx, "outbox delivery dead-lettered after exhausting retries",
			slog.String("event_type", item.env.Type),
			slog.String("event_id", item.env.ID.String()),
			slog.String("handler", item.handler),
			slog.Int64("attempts", attempts),
			slog.Any("error", err))
		if markErr := d.markDead(ctx, item, attempts, err); markErr != nil {
			return outcomeDead, markErr
		}
		return outcomeDead, nil
	}

	d.opts.Logger.WarnContext(ctx, "outbox delivery failed; will retry",
		slog.String("event_type", item.env.Type),
		slog.String("handler", item.handler),
		slog.Int64("attempt", attempts),
		slog.Any("error", err))
	if markErr := d.markFailed(ctx, item, attempts, err); markErr != nil {
		return outcomeFailed, markErr
	}
	return outcomeFailed, nil
}

// invokeSafely runs a handler, converting a panic into a typed error.
//
// A bad subscriber must not take down a desktop app that is currently ringing up a sale. The
// panic becomes an ordinary delivery failure, retried and eventually dead-lettered like any
// other.
func invokeSafely(ctx context.Context, name string, env event.Envelope, fn Handler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errs.Internal(CodeHandlerPanicked,
				fmt.Sprintf("handler %q panicked processing %q: %v", name, env.Type, r))
		}
	}()
	return fn(ctx, env)
}

// ── state transitions ───────────────────────────────────────────────────────────

// tryClaim atomically moves a delivery from pending/failed to processing.
//
// The `status IN ('pending','failed')` predicate is the compare half of the
// compare-and-swap: if another dispatcher already claimed the row, the UPDATE matches
// nothing and this caller backs off. SQLite's single-writer pool serialises the statements,
// so exactly one dispatcher can win.
//
// next_attempt_at doubles as the visibility deadline while processing (design D5).
func (d *Dispatcher) tryClaim(ctx context.Context, item workItem) (bool, error) {
	now := d.opts.Clock.Now()
	deadline := clock.Format(now.Add(d.opts.VisibilityTimeout))

	res, err := d.db.Writer(ctx).ExecContext(ctx, `
		UPDATE outbox_deliveries
		   SET status = 'processing', next_attempt_at = ?, updated_at = ?
		 WHERE event_id = ? AND handler = ?
		   AND status IN ('pending', 'failed')`,
		deadline, clock.Format(now), item.env.ID.String(), item.handler)
	if err != nil {
		return false, errs.Wrap(d.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeClaimFailed, "claiming outbox delivery")
	}
	n, err := res.RowsAffected()
	if err != nil {
		// Without the count we cannot know whether this dispatcher won the claim. Reporting
		// "not claimed" costs one skipped pass; guessing "claimed" could deliver the event
		// twice, which is the failure this whole design exists to prevent.
		return false, nil //nolint:nilerr // unknown claim outcome is treated as contended
	}
	return n == 1, nil
}

func (d *Dispatcher) markDone(ctx context.Context, item workItem) error {
	now := clock.Format(d.opts.Clock.Now())
	if err := d.exec(ctx, `
		UPDATE outbox_deliveries
		   SET status = 'done', attempts = attempts + 1, next_attempt_at = NULL,
		       last_error = NULL, processed_at = ?, updated_at = ?
		 WHERE event_id = ? AND handler = ?`,
		now, now, item.env.ID.String(), item.handler); err != nil {
		return err
	}
	return d.rollUpEvent(ctx, item.env.ID)
}

func (d *Dispatcher) markFailed(ctx context.Context, item workItem, attempts int64, cause error) error {
	now := d.opts.Clock.Now()
	next := clock.Format(now.Add(d.backoff(attempts)))
	if err := d.exec(ctx, `
		UPDATE outbox_deliveries
		   SET status = 'failed', attempts = ?, next_attempt_at = ?, last_error = ?, updated_at = ?
		 WHERE event_id = ? AND handler = ?`,
		attempts, next, cause.Error(), clock.Format(now),
		item.env.ID.String(), item.handler); err != nil {
		return err
	}
	return d.rollUpEvent(ctx, item.env.ID)
}

func (d *Dispatcher) markDead(ctx context.Context, item workItem, attempts int64, cause error) error {
	now := clock.Format(d.opts.Clock.Now())
	if err := d.exec(ctx, `
		UPDATE outbox_deliveries
		   SET status = 'dead', attempts = ?, next_attempt_at = NULL, last_error = ?, updated_at = ?
		 WHERE event_id = ? AND handler = ?`,
		attempts, cause.Error(), now, item.env.ID.String(), item.handler); err != nil {
		return err
	}
	return d.rollUpEvent(ctx, item.env.ID)
}

// rollUpEvent recomputes outbox_events.status from its deliveries, so the event row stays a
// meaningful summary for the diagnostics screen: dead if any handler gave up, done once every
// handler succeeded, otherwise still in flight.
func (d *Dispatcher) rollUpEvent(ctx context.Context, eventID id.ID) error {
	now := clock.Format(d.opts.Clock.Now())
	return d.exec(ctx, `
		UPDATE outbox_events
		   SET status = CASE
		         WHEN EXISTS (SELECT 1 FROM outbox_deliveries
		                       WHERE event_id = ? AND status = 'dead') THEN 'dead'
		         WHEN NOT EXISTS (SELECT 1 FROM outbox_deliveries
		                           WHERE event_id = ? AND status <> 'done') THEN 'done'
		         WHEN EXISTS (SELECT 1 FROM outbox_deliveries
		                       WHERE event_id = ? AND status = 'failed') THEN 'failed'
		         ELSE 'pending'
		       END,
		       processed_at = CASE
		         WHEN NOT EXISTS (SELECT 1 FROM outbox_deliveries
		                           WHERE event_id = ? AND status <> 'done') THEN ?
		         ELSE processed_at
		       END
		 WHERE id = ?`,
		eventID.String(), eventID.String(), eventID.String(),
		eventID.String(), now, eventID.String())
}

func (d *Dispatcher) exec(ctx context.Context, query string, args ...any) error {
	if _, err := d.db.Writer(ctx).ExecContext(ctx, query, args...); err != nil {
		return errs.Wrap(d.db.Dialect().TranslateError(err), errs.CategoryInternal,
			CodeClaimFailed, "updating outbox delivery state")
	}
	return nil
}

// backoff returns the retry delay for an attempt: exponential, capped, with jitter.
//
// Jitter is not ceremony. Without it a batch of deliveries that all failed for the same
// reason — the printer is off, the network is down — retries in perfect lockstep forever,
// hammering the same failing resource at the same instants.
func (d *Dispatcher) backoff(attempts int64) time.Duration {
	delay := d.opts.BaseBackoff
	for i := int64(1); i < attempts && delay < d.opts.MaxBackoff; i++ {
		delay *= 2
	}
	if delay > d.opts.MaxBackoff {
		delay = d.opts.MaxBackoff
	}
	// ±25% jitter.
	spread := delay / 4
	if spread > 0 {
		delay += time.Duration(rand.Int64N(int64(2*spread))) - spread //nolint:gosec // jitter, not cryptography
	}
	return delay
}
