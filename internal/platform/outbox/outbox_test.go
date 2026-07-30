package outbox_test

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

// ── fixtures ────────────────────────────────────────────────────────────────────

type invoicePosted struct {
	Invoice id.ID `json:"invoice"`
	Total   int64 `json:"total"`
}

func (invoicePosted) EventType() string     { return "sales.invoice_posted" }
func (invoicePosted) AggregateType() string { return "sales_document" }
func (e invoicePosted) AggregateID() id.ID  { return e.Invoice }

type invoiceVoided struct {
	Invoice id.ID `json:"invoice"`
}

func (invoiceVoided) EventType() string     { return "sales.invoice_voided" }
func (invoiceVoided) AggregateType() string { return "sales_document" }
func (e invoiceVoided) AggregateID() id.ID  { return e.Invoice }

const (
	postedType = "sales.invoice_posted"
	voidedType = "sales.invoice_voided"
)

type harness struct {
	db   *database.Store
	subs *outbox.Subscribers
	st   *outbox.Store
	clk  *clock.Fixed
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "outbox_test.db")
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

	clk := clock.NewFixed(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC))
	subs := outbox.NewSubscribers()
	return &harness{db: db, subs: subs, st: outbox.NewStore(db, subs, clk, nil), clk: clk}
}

func (h *harness) dispatcher(opts outbox.DispatcherOptions) *outbox.Dispatcher {
	if opts.Clock == nil {
		opts.Clock = h.clk
	}
	return outbox.NewDispatcher(h.db, h.subs, opts)
}

func (h *harness) countEvents(t *testing.T) int {
	t.Helper()
	var n int
	if err := h.db.WriterPool().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM outbox_events`).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

func (h *harness) countDeliveries(t *testing.T) int {
	t.Helper()
	var n int
	if err := h.db.WriterPool().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM outbox_deliveries`).Scan(&n); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	return n
}

func (h *harness) delivery(t *testing.T, eventID id.ID, handler string) (status string, attempts int64, nextAttempt *string) {
	t.Helper()
	err := h.db.WriterPool().QueryRowContext(context.Background(),
		`SELECT status, attempts, next_attempt_at FROM outbox_deliveries
		  WHERE event_id = ? AND handler = ?`, eventID.String(), handler).
		Scan(&status, &attempts, &nextAttempt)
	if err != nil {
		t.Fatalf("read delivery %s/%s: %v", eventID, handler, err)
	}
	return
}

func (h *harness) eventStatus(t *testing.T, eventID id.ID) string {
	t.Helper()
	var status string
	if err := h.db.WriterPool().QueryRowContext(context.Background(),
		`SELECT status FROM outbox_events WHERE id = ?`, eventID.String()).Scan(&status); err != nil {
		t.Fatalf("read event: %v", err)
	}
	return status
}

// recorder collects the events a handler received.
type recorder struct {
	mu   sync.Mutex
	seen []event.Envelope
}

func (r *recorder) handle(_ context.Context, env event.Envelope) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, env)
	return nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seen)
}

// ── THE ATOMICITY DRILL — the guarantee the whole step exists for ───────────────

func TestPublishCommitsWithTheBusinessTransaction(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	if err := h.subs.Register(postedType, "accounting", func(context.Context, event.Envelope) error { return nil }); err != nil {
		t.Fatal(err)
	}

	err := h.db.Do(ctx, func(ctx context.Context) error {
		_, pubErr := h.st.Publish(ctx, invoicePosted{Invoice: "inv-1", Total: 5000})
		return pubErr
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if got := h.countEvents(t); got != 1 {
		t.Errorf("events = %d, want 1", got)
	}
	if got := h.countDeliveries(t); got != 1 {
		t.Errorf("deliveries = %d, want 1", got)
	}
}

func TestPublishRollsBackWithTheBusinessTransaction(t *testing.T) {
	// THE drill. An event written outside the business transaction is the bug this whole
	// design exists to prevent: the invoice rolls back, the "post a journal entry" intent
	// survives, and the books are wrong with nothing recording why.
	ctx := context.Background()
	h := newHarness(t)
	if err := h.subs.Register(postedType, "accounting", func(context.Context, event.Envelope) error { return nil }); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("the sale failed after the event was published")
	err := h.db.Do(ctx, func(ctx context.Context) error {
		if _, pubErr := h.st.Publish(ctx, invoicePosted{Invoice: "inv-1", Total: 5000}); pubErr != nil {
			return pubErr
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Do returned %v, want the sentinel", err)
	}

	if got := h.countEvents(t); got != 0 {
		t.Errorf("events = %d after rollback, want 0 — the event escaped its transaction", got)
	}
	if got := h.countDeliveries(t); got != 0 {
		t.Errorf("deliveries = %d after rollback, want 0", got)
	}
}

func TestPublishMaterialisesOneDeliveryPerRegisteredHandler(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	for _, name := range []string{"accounting", "inventory", "notifications"} {
		if err := h.subs.Register(postedType, name, func(context.Context, event.Envelope) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	// A handler on a different event type must not receive a delivery.
	if err := h.subs.Register(voidedType, "accounting", func(context.Context, event.Envelope) error { return nil }); err != nil {
		t.Fatal(err)
	}

	var env event.Envelope
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		var pubErr error
		env, pubErr = h.st.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return pubErr
	}); err != nil {
		t.Fatal(err)
	}

	if got := h.countDeliveries(t); got != 3 {
		t.Fatalf("deliveries = %d, want 3 (one per handler of this event type)", got)
	}
	for _, name := range []string{"accounting", "inventory", "notifications"} {
		if status, _, _ := h.delivery(t, env.ID, name); status != outbox.StatusPending {
			t.Errorf("%s starts as %q, want pending", name, status)
		}
	}
}

func TestSubscriberRegisteredLaterGetsNoHistoricalDeliveries(t *testing.T) {
	// Deliveries are materialised at publish time precisely so this rule is exact:
	// a subscriber receives only events published after it was registered.
	ctx := context.Background()
	h := newHarness(t)
	if err := h.subs.Register(postedType, "early", func(context.Context, event.Envelope) error { return nil }); err != nil {
		t.Fatal(err)
	}

	var first event.Envelope
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		var pubErr error
		first, pubErr = h.st.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return pubErr
	}); err != nil {
		t.Fatal(err)
	}

	if err := h.subs.Register(postedType, "late", func(context.Context, event.Envelope) error { return nil }); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := h.db.WriterPool().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM outbox_deliveries WHERE event_id = ? AND handler = 'late'`,
		first.ID.String()).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("the late subscriber received %d historical deliveries, want 0", count)
	}
}

func TestPayloadRoundTripsThroughConcreteTypes(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	rec := &recorder{}
	if err := h.subs.Register(postedType, "accounting", rec.handle); err != nil {
		t.Fatal(err)
	}
	// 2^53+1 survives only if nothing decodes through float64.
	const exact = int64(9007199254740993)
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		_, pubErr := h.st.Publish(ctx, invoicePosted{Invoice: "inv-1", Total: exact})
		return pubErr
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.dispatcher(outbox.DispatcherOptions{}).DispatchOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 1 {
		t.Fatalf("handler saw %d events, want 1", rec.count())
	}

	var got invoicePosted
	if err := json.Unmarshal(rec.seen[0].Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got.Total != exact {
		t.Errorf("total = %d, want %d (precision lost through a float?)", got.Total, exact)
	}
	if got.Invoice != "inv-1" {
		t.Errorf("invoice = %q", got.Invoice)
	}
}

func TestCorrelationAndCausationAreStamped(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	if err := h.subs.Register(postedType, "accounting", func(context.Context, event.Envelope) error { return nil }); err != nil {
		t.Fatal(err)
	}

	corr := id.ID("01890000-0000-7000-8000-0000000c0771")
	traced := event.WithCorrelationID(ctx, corr)

	var env event.Envelope
	if err := h.db.Do(traced, func(ctx context.Context) error {
		var pubErr error
		env, pubErr = h.st.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return pubErr
	}); err != nil {
		t.Fatal(err)
	}
	if env.CorrelationID != corr {
		t.Errorf("correlation = %q, want %q", env.CorrelationID, corr)
	}

	// With no correlation in context, the event seeds its own so every envelope has one.
	var solo event.Envelope
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		var pubErr error
		solo, pubErr = h.st.Publish(ctx, invoicePosted{Invoice: "inv-2"})
		return pubErr
	}); err != nil {
		t.Fatal(err)
	}
	if solo.CorrelationID != solo.ID {
		t.Errorf("correlation = %q, want it seeded from the event id %q", solo.CorrelationID, solo.ID)
	}
}

// ── dispatch ────────────────────────────────────────────────────────────────────

func TestDispatchHappyPath(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	rec := &recorder{}
	if err := h.subs.Register(postedType, "accounting", rec.handle); err != nil {
		t.Fatal(err)
	}

	var env event.Envelope
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		var pubErr error
		env, pubErr = h.st.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return pubErr
	}); err != nil {
		t.Fatal(err)
	}

	d := h.dispatcher(outbox.DispatcherOptions{})
	rep, err := d.DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if rep.Delivered != 1 {
		t.Fatalf("report = %+v, want 1 delivered", rep)
	}
	if rec.count() != 1 {
		t.Errorf("handler invoked %d times, want 1", rec.count())
	}

	status, attempts, next := h.delivery(t, env.ID, "accounting")
	if status != outbox.StatusDone || attempts != 1 || next != nil {
		t.Errorf("delivery = %s attempts=%d next=%v, want done/1/nil", status, attempts, next)
	}
	if got := h.eventStatus(t, env.ID); got != outbox.StatusDone {
		t.Errorf("event status = %q, want done once every handler succeeded", got)
	}

	// A second pass has nothing to do, and must not redeliver.
	rep2, err := d.DispatchOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Delivered != 0 || rec.count() != 1 {
		t.Errorf("second pass redelivered: report=%+v handler calls=%d", rep2, rec.count())
	}
}

func TestOnlyTheFailedHandlerIsRetried(t *testing.T) {
	// The whole point of D3/Option A: a retry must not re-run a handler that already
	// succeeded. With a single status per event, accounting would post twice.
	ctx := context.Background()
	h := newHarness(t)

	good := &recorder{}
	badCalls := 0
	if err := h.subs.Register(postedType, "accounting", good.handle); err != nil {
		t.Fatal(err)
	}
	if err := h.subs.Register(postedType, "notifications", func(context.Context, event.Envelope) error {
		badCalls++
		return errors.New("smtp unavailable")
	}); err != nil {
		t.Fatal(err)
	}

	var env event.Envelope
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		var pubErr error
		env, pubErr = h.st.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return pubErr
	}); err != nil {
		t.Fatal(err)
	}

	d := h.dispatcher(outbox.DispatcherOptions{BaseBackoff: time.Millisecond})
	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if good.count() != 1 || badCalls != 1 {
		t.Fatalf("first pass: good=%d bad=%d, want 1 and 1", good.count(), badCalls)
	}

	// Move past the backoff and dispatch again.
	h.clk.Advance(time.Hour)
	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if good.count() != 1 {
		t.Errorf("the succeeded handler ran again (%d times) — this is the double-post bug", good.count())
	}
	if badCalls != 2 {
		t.Errorf("the failed handler ran %d times, want 2", badCalls)
	}
	if status, _, _ := h.delivery(t, env.ID, "accounting"); status != outbox.StatusDone {
		t.Errorf("accounting delivery = %q, want done", status)
	}
}

// ── HEAD-OF-LINE ORDERING ───────────────────────────────────────────────────────

func TestOrderingPerAggregateBlocksBehindAFailure(t *testing.T) {
	// "invoice voided" reaching accounting before "invoice posted" produces a ledger that
	// balances and is wrong — the hardest kind of error to find.
	ctx := context.Background()
	h := newHarness(t)

	var seen []string
	failPosted := true
	if err := h.subs.Register(postedType, "accounting", func(_ context.Context, env event.Envelope) error {
		if failPosted {
			return errors.New("chart of accounts not ready")
		}
		seen = append(seen, "posted")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.subs.Register(voidedType, "accounting", func(_ context.Context, env event.Envelope) error {
		seen = append(seen, "voided")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Two events for the SAME aggregate, posted first.
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		_, e := h.st.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(time.Second)
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		_, e := h.st.Publish(ctx, invoiceVoided{Invoice: "inv-1"})
		return e
	}); err != nil {
		t.Fatal(err)
	}

	d := h.dispatcher(outbox.DispatcherOptions{BaseBackoff: time.Millisecond})
	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 0 {
		t.Fatalf("handler saw %v; 'voided' must not be delivered while 'posted' is outstanding", seen)
	}

	// Once the first event succeeds, the second is released — in order.
	failPosted = false
	h.clk.Advance(time.Hour)
	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(time.Hour)
	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatal(err)
	}

	if len(seen) != 2 || seen[0] != "posted" || seen[1] != "voided" {
		t.Errorf("delivery order = %v, want [posted voided]", seen)
	}
}

func TestOrderingDoesNotBlockOtherAggregatesOrHandlers(t *testing.T) {
	// Head-of-line blocking must be scoped to (aggregate, handler): a stuck accounting
	// handler on invoice A must not stall invoice B, nor a different handler on A.
	ctx := context.Background()
	h := newHarness(t)

	otherAggregate := 0
	otherHandler := 0
	if err := h.subs.Register(postedType, "accounting", func(_ context.Context, env event.Envelope) error {
		var p invoicePosted
		_ = json.Unmarshal(env.Payload, &p)
		if p.Invoice == "inv-A" {
			return errors.New("stuck on A")
		}
		otherAggregate++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.subs.Register(postedType, "inventory", func(context.Context, event.Envelope) error {
		otherHandler++
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for _, invoice := range []id.ID{"inv-A", "inv-B"} {
		inv := invoice
		if err := h.db.Do(ctx, func(ctx context.Context) error {
			_, e := h.st.Publish(ctx, invoicePosted{Invoice: inv})
			return e
		}); err != nil {
			t.Fatal(err)
		}
		h.clk.Advance(time.Second)
	}
	// A second event for the stuck aggregate.
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		_, e := h.st.Publish(ctx, invoiceVoided{Invoice: "inv-A"})
		return e
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.dispatcher(outbox.DispatcherOptions{BaseBackoff: time.Millisecond}).DispatchOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if otherAggregate != 1 {
		t.Errorf("invoice B delivered %d times, want 1 — a stuck aggregate blocked an unrelated one", otherAggregate)
	}
	if otherHandler != 2 {
		t.Errorf("the inventory handler ran %d times, want 2 — a stuck handler blocked a healthy one", otherHandler)
	}
}

// ── retry, dead-lettering, crash recovery ───────────────────────────────────────

func TestFailureSchedulesABackoffAndIsNotReclaimedEarly(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	calls := 0
	if err := h.subs.Register(postedType, "flaky", func(context.Context, event.Envelope) error {
		calls++
		return errors.New("temporary")
	}); err != nil {
		t.Fatal(err)
	}
	var env event.Envelope
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		var e error
		env, e = h.st.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return e
	}); err != nil {
		t.Fatal(err)
	}

	d := h.dispatcher(outbox.DispatcherOptions{BaseBackoff: time.Minute, MaxBackoff: time.Hour})
	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatal(err)
	}

	status, attempts, next := h.delivery(t, env.ID, "flaky")
	if status != outbox.StatusFailed || attempts != 1 || next == nil {
		t.Fatalf("delivery = %s attempts=%d next=%v, want failed/1/scheduled", status, attempts, next)
	}
	var lastErr *string
	if err := h.db.WriterPool().QueryRowContext(ctx,
		`SELECT last_error FROM outbox_deliveries WHERE event_id = ? AND handler = 'flaky'`,
		env.ID.String()).Scan(&lastErr); err != nil {
		t.Fatal(err)
	}
	if lastErr == nil || *lastErr == "" {
		t.Error("last_error was not recorded; a stuck delivery would be undiagnosable")
	}
	if got := h.eventStatus(t, env.ID); got != outbox.StatusFailed {
		t.Errorf("event status = %q, want failed", got)
	}

	// Before the backoff elapses the delivery must not be re-claimed.
	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("handler ran %d times, want 1 — the backoff was ignored", calls)
	}

	h.clk.Advance(2 * time.Hour)
	if _, err := d.DispatchOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("handler ran %d times after the backoff elapsed, want 2", calls)
	}
}

func TestDeadLetterAfterMaxAttempts(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	if err := h.subs.Register(postedType, "doomed", func(context.Context, event.Envelope) error {
		return errors.New("permanently broken")
	}); err != nil {
		t.Fatal(err)
	}
	var env event.Envelope
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		var e error
		env, e = h.st.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return e
	}); err != nil {
		t.Fatal(err)
	}

	d := h.dispatcher(outbox.DispatcherOptions{MaxAttempts: 3, BaseBackoff: time.Millisecond})
	for i := 0; i < 5; i++ {
		if _, err := d.DispatchOnce(ctx); err != nil {
			t.Fatal(err)
		}
		h.clk.Advance(time.Hour)
	}

	status, attempts, next := h.delivery(t, env.ID, "doomed")
	if status != outbox.StatusDead {
		t.Fatalf("status = %q after exhausting retries, want dead", status)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want to stop at MaxAttempts (3)", attempts)
	}
	if next != nil {
		t.Errorf("a dead delivery still has next_attempt_at = %v; it must never be re-claimed", *next)
	}
	if got := h.eventStatus(t, env.ID); got != outbox.StatusDead {
		t.Errorf("event status = %q, want dead", got)
	}
	// Never deleted — it is the diagnostic record of an incident.
	if h.countDeliveries(t) != 1 {
		t.Error("the dead delivery was removed; it must be retained for a human to inspect")
	}
}

func TestDeadDeliveryBlocksItsAggregateStream(t *testing.T) {
	// If accounting dead-letters "posted", delivering "voided" would post a reversal for an
	// entry that was never made.
	ctx := context.Background()
	h := newHarness(t)
	voidedCalls := 0
	if err := h.subs.Register(postedType, "accounting", func(context.Context, event.Envelope) error {
		return errors.New("broken")
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.subs.Register(voidedType, "accounting", func(context.Context, event.Envelope) error {
		voidedCalls++
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := h.db.Do(ctx, func(ctx context.Context) error {
		_, e := h.st.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(time.Second)
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		_, e := h.st.Publish(ctx, invoiceVoided{Invoice: "inv-1"})
		return e
	}); err != nil {
		t.Fatal(err)
	}

	d := h.dispatcher(outbox.DispatcherOptions{MaxAttempts: 2, BaseBackoff: time.Millisecond})
	for i := 0; i < 6; i++ {
		if _, err := d.DispatchOnce(ctx); err != nil {
			t.Fatal(err)
		}
		h.clk.Advance(time.Hour)
	}
	if voidedCalls != 0 {
		t.Errorf("'voided' was delivered %d times while 'posted' is dead; the stream must halt", voidedCalls)
	}
}

func TestCrashRecoveryReclaimsAbandonedDeliveries(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	rec := &recorder{}
	if err := h.subs.Register(postedType, "accounting", rec.handle); err != nil {
		t.Fatal(err)
	}
	var env event.Envelope
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		var e error
		env, e = h.st.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return e
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate a dispatcher that claimed the row and then died.
	deadline := clock.Format(h.clk.Now().Add(5 * time.Minute))
	if _, err := h.db.WriterPool().ExecContext(ctx,
		`UPDATE outbox_deliveries SET status = 'processing', next_attempt_at = ?
		  WHERE event_id = ? AND handler = 'accounting'`, deadline, env.ID.String()); err != nil {
		t.Fatal(err)
	}

	d := h.dispatcher(outbox.DispatcherOptions{VisibilityTimeout: 5 * time.Minute})

	// Inside the visibility window: another dispatcher must leave it alone.
	rep, err := d.DispatchOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Reclaimed != 0 || rec.count() != 0 {
		t.Fatalf("row reclaimed inside its visibility window: %+v", rep)
	}

	// Past the deadline it is presumed abandoned.
	h.clk.Advance(10 * time.Minute)
	rep, err = d.DispatchOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Reclaimed != 1 {
		t.Errorf("Reclaimed = %d, want 1", rep.Reclaimed)
	}
	if rec.count() != 1 {
		t.Errorf("the reclaimed delivery was not re-delivered (%d calls)", rec.count())
	}
	if status, _, _ := h.delivery(t, env.ID, "accounting"); status != outbox.StatusDone {
		t.Errorf("status = %q after recovery, want done", status)
	}
}

func TestHandlerPanicIsIsolatedToItsOwnDelivery(t *testing.T) {
	// A bad subscriber must not take down an app that is ringing up a sale.
	ctx := context.Background()
	h := newHarness(t)
	survivor := &recorder{}
	if err := h.subs.Register(postedType, "panicky", func(context.Context, event.Envelope) error {
		panic("index out of range")
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.subs.Register(postedType, "healthy", survivor.handle); err != nil {
		t.Fatal(err)
	}

	var env event.Envelope
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		var e error
		env, e = h.st.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return e
	}); err != nil {
		t.Fatal(err)
	}

	rep, err := h.dispatcher(outbox.DispatcherOptions{BaseBackoff: time.Millisecond}).DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("a handler panic escaped the dispatcher: %v", err)
	}
	if rep.Delivered != 1 || rep.Failed != 1 {
		t.Errorf("report = %+v, want 1 delivered and 1 failed", rep)
	}
	if survivor.count() != 1 {
		t.Error("the healthy handler did not run; a panic was not isolated")
	}
	if status, _, _ := h.delivery(t, env.ID, "panicky"); status != outbox.StatusFailed {
		t.Errorf("panicking delivery = %q, want failed (retryable)", status)
	}
}

func TestDeliveryForAnUnregisteredHandlerIsSkipped(t *testing.T) {
	// A renamed or removed subscriber leaves orphan rows. Retrying them until dead would be
	// noise; blocking the aggregate forever would be worse.
	ctx := context.Background()
	h := newHarness(t)
	if err := h.subs.Register(postedType, "renamed_away", func(context.Context, event.Envelope) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := h.db.Do(ctx, func(ctx context.Context) error {
		_, e := h.st.Publish(ctx, invoicePosted{Invoice: "inv-1"})
		return e
	}); err != nil {
		t.Fatal(err)
	}

	// A fresh registry, as though the handler had been renamed in a new release.
	fresh := outbox.NewSubscribers()
	rep, err := outbox.NewDispatcher(h.db, fresh, outbox.DispatcherOptions{Clock: h.clk}).DispatchOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Skipped != 1 {
		t.Errorf("report = %+v, want 1 skipped", rep)
	}
}

func TestConcurrentDispatchersDeliverEachDeliveryOnce(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	rec := &recorder{}
	if err := h.subs.Register(postedType, "accounting", rec.handle); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := h.db.Do(ctx, func(ctx context.Context) error {
			_, e := h.st.Publish(ctx, invoicePosted{Invoice: id.ID("inv-" + string(rune('a'+i)))})
			return e
		}); err != nil {
			t.Fatal(err)
		}
		h.clk.Advance(time.Millisecond)
	}

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := h.dispatcher(outbox.DispatcherOptions{})
			_, _ = d.DispatchOnce(ctx)
		}()
	}
	wg.Wait()

	// Every event delivered, none twice.
	var done int
	if err := h.db.WriterPool().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM outbox_deliveries WHERE status = 'done'`).Scan(&done); err != nil {
		t.Fatal(err)
	}
	if done != 10 {
		t.Errorf("done deliveries = %d, want 10", done)
	}
	if rec.count() != 10 {
		t.Errorf("handler invoked %d times, want exactly 10", rec.count())
	}
}

func TestRegistrationRejections(t *testing.T) {
	subs := outbox.NewSubscribers()
	if err := subs.Register(postedType, "a", func(context.Context, event.Envelope) error { return nil }); err != nil {
		t.Fatal(err)
	}
	t.Run("duplicate", func(t *testing.T) {
		err := subs.Register(postedType, "a", func(context.Context, event.Envelope) error { return nil })
		if errs.CodeOf(err) != outbox.CodeDuplicateSubscription {
			t.Fatalf("code = %q", errs.CodeOf(err))
		}
	})
	t.Run("empty name", func(t *testing.T) {
		if errs.CodeOf(subs.Register(postedType, "", nil)) != outbox.CodeInvalidSubscription {
			t.Fatal("accepted an empty name")
		}
	})
	t.Run("nil handler", func(t *testing.T) {
		if errs.CodeOf(subs.Register(postedType, "b", nil)) != outbox.CodeInvalidSubscription {
			t.Fatal("accepted a nil handler")
		}
	})
}
