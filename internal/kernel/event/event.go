package event

import (
	"context"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Event is anything a module publishes. Implementations are plain structs owned by the
// module that raises them.
//
// EventType is namespaced and stable ("sales.invoice_posted"): it is written into every
// customer's outbox and is therefore part of the public contract. An event whose payload
// changes meaning gets a NEW type, never a redefinition — historical rows in a three-year-old
// outbox must stay interpretable.
type Event interface {
	EventType() string
	AggregateType() string
	AggregateID() id.ID
}

// Publisher raises an event.
//
// Declared HERE, in the kernel, rather than by each module that publishes: a narrow port
// repeated four times is four chances for the shapes to drift, and the shape is not negotiable
// — it is exactly what platform/eventbus.Bus already does.
//
// Modules depend on THIS, never on *eventbus.Bus. The point is not testability (a fake bus is
// no easier than a real one); it is that a module must not be able to reach the bus's
// Subscribe. A module raises events; deciding who reacts is the composition root's job.
type Publisher interface {
	Publish(ctx context.Context, e Event) error
}

// Envelope is the transport form shared by the in-process bus and the outbox, so promoting a
// domain event to an integration event later does not change its shape.
type Envelope struct {
	// ID is the event's own identity. UUIDv7, so it is also a tiebreaker that orders
	// consistently with OccurredAt.
	ID            id.ID
	Type          string
	OccurredAt    time.Time
	AggregateType string
	AggregateID   id.ID
	// CorrelationID identifies the user action that began this chain; CausationID the event
	// that directly caused this one. Together they make a causal graph reconstructable from
	// the outbox alone — which is what a support engineer needs, and what a future cloud
	// sync consumes.
	CorrelationID id.ID
	CausationID   id.ID
	BranchID      id.ID
	Payload       []byte // JSON
}

// ── correlation & causation, carried in the context ─────────────────────────────
//
// These are stamped automatically when an event is published. If a handler had to remember
// to pass them along, causal chains would be only as reliable as the discipline of whoever
// wrote the handler — which is to say, absent by the third module.

type correlationKey struct{}
type causationKey struct{}
type branchKey struct{}

// WithCorrelationID marks ctx as belonging to one user action. The API layer sets this once
// per request; everything downstream inherits it.
func WithCorrelationID(ctx context.Context, cid id.ID) context.Context {
	return context.WithValue(ctx, correlationKey{}, cid)
}

// CorrelationID returns the active correlation, if any.
func CorrelationID(ctx context.Context) (id.ID, bool) {
	v, ok := ctx.Value(correlationKey{}).(id.ID)
	return v, ok && !v.IsZero()
}

// WithCausationID marks ctx as being handled because of a specific event. The dispatcher
// sets this before invoking a handler, so anything that handler publishes is linked back.
func WithCausationID(ctx context.Context, cid id.ID) context.Context {
	return context.WithValue(ctx, causationKey{}, cid)
}

// CausationID returns the event currently being handled, if any.
func CausationID(ctx context.Context) (id.ID, bool) {
	v, ok := ctx.Value(causationKey{}).(id.ID)
	return v, ok && !v.IsZero()
}

// WithBranchID records the branch an action belongs to, so events remain attributable in a
// multi-branch install (ARCHITECTURE_v1 §26).
func WithBranchID(ctx context.Context, bid id.ID) context.Context {
	return context.WithValue(ctx, branchKey{}, bid)
}

// BranchID returns the active branch, if any.
func BranchID(ctx context.Context) (id.ID, bool) {
	v, ok := ctx.Value(branchKey{}).(id.ID)
	return v, ok && !v.IsZero()
}

// Stamp builds an Envelope for e, filling identity and causal fields from ctx.
//
// A published event's CausationID is whatever event is currently being handled; its
// CorrelationID is inherited, or — when this is the first event of a chain — seeded from the
// event's own id so every envelope has one.
func Stamp(ctx context.Context, e Event, now time.Time, payload []byte) (Envelope, error) {
	eventID, err := id.New()
	if err != nil {
		return Envelope{}, err
	}
	env := Envelope{
		ID:            eventID,
		Type:          e.EventType(),
		OccurredAt:    now.UTC(),
		AggregateType: e.AggregateType(),
		AggregateID:   e.AggregateID(),
		Payload:       payload,
	}
	if cid, ok := CorrelationID(ctx); ok {
		env.CorrelationID = cid
	} else {
		env.CorrelationID = eventID
	}
	if cause, ok := CausationID(ctx); ok {
		env.CausationID = cause
	}
	if bid, ok := BranchID(ctx); ok {
		env.BranchID = bid
	}
	return env, nil
}
