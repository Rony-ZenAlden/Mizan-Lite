// Package eventbus is the in-process bus for DOMAIN events: things that happen within one
// module and must succeed or fail together with the operation that raised them
// (ARCHITECTURE_v1 §23.1).
//
// Domain events are delivered synchronously, in the publisher's goroutine and transaction. A
// handler error aborts the publish and rolls the whole operation back — that is the point:
// "the invoice recalculates its totals" must not be able to half-happen.
//
// Cross-module effects do NOT belong here. They go through platform/outbox, so a failing
// subscriber cannot roll back an unrelated sale. Conflating the two is the classic source of
// either lost side effects or transactions that abort because a dashboard counter failed.
package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/event"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeDuplicateSubscription = "eventbus.duplicate_subscription"
	CodeInvalidSubscription   = "eventbus.invalid_subscription"
	CodeHandlerFailed         = "eventbus.handler_failed"
	CodeHandlerPanicked       = "eventbus.handler_panicked"
)

// Options configures a Bus.
type Options struct {
	Logger *slog.Logger
}

// Bus dispatches domain events to synchronously registered handlers.
type Bus struct {
	log *slog.Logger

	mu   sync.RWMutex
	subs map[string][]subscription // event type → handlers, in registration order
}

type subscription struct {
	name   string
	invoke func(context.Context, event.Event) error
}

// New returns an empty bus.
func New(opts Options) *Bus {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Bus{log: opts.Logger, subs: map[string][]subscription{}}
}

// Subscribe registers a typed handler for events of type T.
//
// Generic so the handler receives a concrete type rather than an `any` it must assert.
// Registration is explicit and happens at bootstrap — there is deliberately NO
// reflection-based discovery, so "who reacts to invoice posted?" is answerable with grep,
// which is the only thing that still works once the author has moved on.
//
// name identifies the subscription in logs and errors; a duplicate (event type, name) is an
// error rather than a silent overwrite, because the winner would otherwise depend on package
// initialisation order and could differ between builds.
func Subscribe[T event.Event](b *Bus, name string, handler func(context.Context, T) error) error {
	if name == "" {
		return errs.Validation(CodeInvalidSubscription, "a subscription needs a name")
	}
	if handler == nil {
		return errs.Validation(CodeInvalidSubscription, "a subscription needs a handler").
			WithParam("name", name)
	}

	// The event type is read from T's zero value, so subscribing needs no instance. That
	// requires EventType() to work on the zero value — true for the value-struct events this
	// codebase uses, but a pointer type would nil-dereference here. Caught and reported as a
	// registration error rather than crashing the bootstrap.
	eventType, err := zeroEventType[T]()
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for _, existing := range b.subs[eventType] {
		if existing.name == name {
			return errs.Conflict(CodeDuplicateSubscription,
				"this event already has a subscription with that name").
				WithParam("event_type", eventType).WithParam("name", name)
		}
	}
	b.subs[eventType] = append(b.subs[eventType], subscription{
		name: name,
		invoke: func(ctx context.Context, e event.Event) error {
			typed, ok := e.(T)
			if !ok {
				return errs.Internal(CodeHandlerFailed,
					fmt.Sprintf("event %q is not the type this handler subscribed to", e.EventType())).
					WithParam("name", name)
			}
			return handler(ctx, typed)
		},
	})
	return nil
}

func zeroEventType[T event.Event]() (name string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errs.Validation(CodeInvalidSubscription,
				fmt.Sprintf("cannot read the event type from the zero value of %T: "+
					"subscribe with a value type, not a pointer", *new(T)))
		}
	}()
	var zero T
	name = zero.EventType()
	if name == "" {
		return "", errs.Validation(CodeInvalidSubscription, "event type is empty")
	}
	return name, nil
}

// Publish delivers e to every subscriber, in registration order, synchronously.
//
// The first handler error stops delivery and is returned, so the caller's Unit of Work rolls
// back. Publishing an event nobody subscribes to is not an error — modules must be able to
// raise events before anything listens.
func (b *Bus) Publish(ctx context.Context, e event.Event) error {
	b.mu.RLock()
	handlers := make([]subscription, len(b.subs[e.EventType()]))
	copy(handlers, b.subs[e.EventType()])
	b.mu.RUnlock()

	for _, sub := range handlers {
		if err := invokeSafely(ctx, sub, e); err != nil {
			b.log.ErrorContext(ctx, "domain event handler failed",
				slog.String("event_type", e.EventType()),
				slog.String("subscription", sub.name),
				slog.Any("error", err))
			return err
		}
	}
	return nil
}

// invokeSafely runs one handler, converting a panic into a typed error.
//
// Recovering is NOT swallowing: the error is returned, so the transaction still rolls back.
// A recovered panic that allowed the commit to proceed would be strictly worse than the
// crash, because the operation would appear to have succeeded.
func invokeSafely(ctx context.Context, sub subscription, e event.Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errs.Internal(CodeHandlerPanicked,
				fmt.Sprintf("subscription %q panicked handling %q: %v", sub.name, e.EventType(), r))
		}
	}()
	return sub.invoke(ctx, e)
}

// Subscriptions returns the registered subscription names for an event type, ordered as they
// will run. Used by diagnostics and by tests asserting bootstrap wiring.
func (b *Bus) Subscriptions(eventType string) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.subs[eventType]))
	for _, s := range b.subs[eventType] {
		out = append(out, s.name)
	}
	return out
}

// EventTypes returns every event type with at least one subscriber, sorted.
func (b *Bus) EventTypes() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.subs))
	for t := range b.subs {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
