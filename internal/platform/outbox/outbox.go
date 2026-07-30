// Package outbox is the transactional outbox for INTEGRATION events — the cross-module
// effects that must survive a crash (ARCHITECTURE_v1 §23.3).
//
// The guarantee, and the reason this exists at all: an event is written inside the SAME
// transaction as the business change that raised it. Either the invoice and the
// intent-to-post-a-journal-entry both commit, or neither does. The tempting alternative —
// commit, then publish — is wrong in a way that only appears in production, because the
// process can die in the gap and nothing anywhere records what should have happened.
//
// Delivery is tracked per handler (Step 0.6 decision D3, Option A), so a retry re-runs only
// what actually failed.
package outbox

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodePublishFailed         = "outbox.publish_failed"
	CodeDuplicateSubscription = "outbox.duplicate_subscription"
	CodeInvalidSubscription   = "outbox.invalid_subscription"
	CodeClaimFailed           = "outbox.claim_failed"
	CodeHandlerPanicked       = "outbox.handler_panicked"
)

// Delivery statuses, matching the CHECK constraint in 0002_outbox_deliveries.sql.
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusDone       = "done"
	StatusFailed     = "failed"
	StatusDead       = "dead"
)

// Handler processes one integration event.
//
// deliveryID is the event's own identifier and is stable across retries, so a handler that
// wants belt-and-braces deduplication can do it with one indexed lookup rather than
// inventing a scheme. With per-handler delivery tracking a retry re-runs only handlers that
// failed, so correctness does not depend on this.
type Handler func(ctx context.Context, env event.Envelope) error

// Subscribers maps event types to the handlers registered for them.
//
// Registration is explicit at bootstrap — no reflection-based discovery, so every
// cross-module reaction is greppable (ARCHITECTURE_v1 §23.2).
type Subscribers struct {
	mu    sync.RWMutex
	byKey map[string][]namedHandler // event type → handlers
}

type namedHandler struct {
	name string
	fn   Handler
}

// NewSubscribers returns an empty registry.
func NewSubscribers() *Subscribers {
	return &Subscribers{byKey: map[string][]namedHandler{}}
}

// Register adds a handler for an event type.
//
// The handler name is written into every delivery row, so it is part of the durable record:
// renaming a handler orphans its in-flight deliveries. Duplicate (event type, name) is an
// error rather than a silent overwrite.
func (s *Subscribers) Register(eventType, name string, fn Handler) error {
	if eventType == "" || name == "" {
		return errs.Validation(CodeInvalidSubscription,
			"an integration subscription needs both an event type and a name")
	}
	if fn == nil {
		return errs.Validation(CodeInvalidSubscription, "an integration subscription needs a handler").
			WithParam("name", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.byKey[eventType] {
		if existing.name == name {
			return errs.Conflict(CodeDuplicateSubscription,
				"this event already has an integration subscription with that name").
				WithParam("event_type", eventType).WithParam("name", name)
		}
	}
	s.byKey[eventType] = append(s.byKey[eventType], namedHandler{name: name, fn: fn})
	return nil
}

// namesFor returns the handler names registered for an event type, sorted so delivery rows
// are written in a deterministic order.
func (s *Subscribers) namesFor(eventType string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.byKey[eventType]))
	for _, h := range s.byKey[eventType] {
		out = append(out, h.name)
	}
	sort.Strings(out)
	return out
}

func (s *Subscribers) lookup(eventType, name string) (Handler, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, h := range s.byKey[eventType] {
		if h.name == name {
			return h.fn, true
		}
	}
	return nil, false
}

// ── the store ───────────────────────────────────────────────────────────────────

// Store writes events and their per-handler deliveries.
type Store struct {
	db   database.DB
	subs *Subscribers
	clk  clock.Clock
	log  *slog.Logger
}

// NewStore builds a Store. A nil clock uses the system clock; a nil logger uses the default.
func NewStore(db database.DB, subs *Subscribers, clk clock.Clock, log *slog.Logger) *Store {
	if clk == nil {
		clk = clock.System()
	}
	if log == nil {
		log = slog.Default()
	}
	return &Store{db: db, subs: subs, clk: clk, log: log}
}

// Publish records e and one delivery row per registered handler.
//
// Everything here uses db.Writer(ctx), which resolves to the caller's transaction when
// inside a Unit of Work. That single fact IS the atomicity guarantee: a rolled-back business
// transaction leaves no event and no deliveries.
func (s *Store) Publish(ctx context.Context, e event.Event) (event.Envelope, error) {
	payload, err := json.Marshal(e)
	if err != nil {
		return event.Envelope{}, errs.Wrap(err, errs.CategoryValidation, CodePublishFailed,
			"encoding event payload for "+e.EventType())
	}
	env, err := event.Stamp(ctx, e, s.clk.Now(), payload)
	if err != nil {
		return event.Envelope{}, err
	}

	now := clock.Format(s.clk.Now())
	if _, err := s.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO outbox_events
		  (id, occurred_at, event_type, aggregate_type, aggregate_id, payload_json,
		   correlation_id, causation_id, branch_id, status, attempts, next_attempt_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', 0, ?)`,
		env.ID.String(), clock.Format(env.OccurredAt), env.Type, env.AggregateType,
		env.AggregateID.String(), string(env.Payload),
		nullable(env.CorrelationID), nullable(env.CausationID), nullable(env.BranchID),
		now,
	); err != nil {
		return event.Envelope{}, errs.Wrap(s.db.Dialect().TranslateError(err),
			errs.CategoryInternal, CodePublishFailed, "writing outbox event "+env.Type)
	}

	handlers := s.subs.namesFor(env.Type)
	if len(handlers) == 0 {
		// Correct — nobody was listening — but this is also exactly what a bootstrap
		// ordering bug looks like, so it must not be silent.
		s.log.WarnContext(ctx, "integration event published with no registered handlers",
			slog.String("event_type", env.Type), slog.String("event_id", env.ID.String()))
		return env, nil
	}

	for _, name := range handlers {
		if _, err := s.db.Writer(ctx).ExecContext(ctx, `
			INSERT INTO outbox_deliveries
			  (event_id, handler, status, attempts, next_attempt_at, created_at, updated_at)
			VALUES (?, ?, 'pending', 0, ?, ?, ?)`,
			env.ID.String(), name, now, now, now,
		); err != nil {
			return event.Envelope{}, errs.Wrap(s.db.Dialect().TranslateError(err),
				errs.CategoryInternal, CodePublishFailed,
				"writing outbox delivery for "+env.Type+"/"+name)
		}
	}
	return env, nil
}

// nullable renders an optional identifier for storage: NULL rather than an empty string, so
// "absent" is distinguishable from "present but blank".
func nullable(v id.ID) any {
	if v.IsZero() {
		return nil
	}
	return v.String()
}

// parseTime reads a stored CHAR(24) timestamp, falling back to the zero time.
func parseTime(s string) time.Time {
	t, _ := clock.ParseTimestamp(s)
	return t
}
