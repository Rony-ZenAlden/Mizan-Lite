package eventbus_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
)

type invoicePosted struct {
	Invoice id.ID
	Total   int64
}

func (invoicePosted) EventType() string     { return "sales.invoice_posted" }
func (invoicePosted) AggregateType() string { return "sales_document" }
func (e invoicePosted) AggregateID() id.ID  { return e.Invoice }

type invoiceVoided struct{ Invoice id.ID }

func (invoiceVoided) EventType() string     { return "sales.invoice_voided" }
func (invoiceVoided) AggregateType() string { return "sales_document" }
func (e invoiceVoided) AggregateID() id.ID  { return e.Invoice }

func newBus() *eventbus.Bus { return eventbus.New(eventbus.Options{}) }

func TestPublishDeliversTypedEvent(t *testing.T) {
	b := newBus()
	var got invoicePosted
	if err := eventbus.Subscribe(b, "totals", func(_ context.Context, e invoicePosted) error {
		got = e
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	want := invoicePosted{Invoice: "inv-1", Total: 5000}
	if err := b.Publish(context.Background(), want); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if got != want {
		t.Errorf("handler received %+v, want %+v", got, want)
	}
}

func TestPublishOnlyReachesMatchingSubscribers(t *testing.T) {
	b := newBus()
	postedCalls, voidedCalls := 0, 0
	_ = eventbus.Subscribe(b, "a", func(context.Context, invoicePosted) error { postedCalls++; return nil })
	_ = eventbus.Subscribe(b, "b", func(context.Context, invoiceVoided) error { voidedCalls++; return nil })

	if err := b.Publish(context.Background(), invoicePosted{Invoice: "x"}); err != nil {
		t.Fatal(err)
	}
	if postedCalls != 1 || voidedCalls != 0 {
		t.Errorf("posted=%d voided=%d, want 1 and 0", postedCalls, voidedCalls)
	}
}

func TestHandlersRunInRegistrationOrder(t *testing.T) {
	// Deterministic and visible in the bootstrap. There is no priority mechanism, because a
	// priority is an implicit dependency that should have been made explicit.
	b := newBus()
	var order []string
	for _, name := range []string{"first", "second", "third"} {
		n := name
		if err := eventbus.Subscribe(b, n, func(context.Context, invoicePosted) error {
			order = append(order, n)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Publish(context.Background(), invoicePosted{Invoice: "x"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"first", "second", "third"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	subs := b.Subscriptions("sales.invoice_posted")
	if len(subs) != 3 || subs[0] != "first" {
		t.Errorf("Subscriptions = %v, want registration order", subs)
	}
}

func TestDuplicateSubscriptionRejected(t *testing.T) {
	// Silent last-one-wins would depend on package init order and could differ per build.
	b := newBus()
	if err := eventbus.Subscribe(b, "totals", func(context.Context, invoicePosted) error { return nil }); err != nil {
		t.Fatal(err)
	}
	err := eventbus.Subscribe(b, "totals", func(context.Context, invoicePosted) error { return nil })
	if errs.CodeOf(err) != eventbus.CodeDuplicateSubscription {
		t.Fatalf("code = %q, want %q", errs.CodeOf(err), eventbus.CodeDuplicateSubscription)
	}
}

func TestInvalidSubscriptionsRejected(t *testing.T) {
	b := newBus()
	t.Run("no name", func(t *testing.T) {
		err := eventbus.Subscribe(b, "", func(context.Context, invoicePosted) error { return nil })
		if errs.CodeOf(err) != eventbus.CodeInvalidSubscription {
			t.Fatalf("code = %q", errs.CodeOf(err))
		}
	})
	t.Run("nil handler", func(t *testing.T) {
		if errs.CodeOf(eventbus.Subscribe[invoicePosted](b, "x", nil)) != eventbus.CodeInvalidSubscription {
			t.Fatal("a nil handler was accepted")
		}
	})
}

func TestHandlerErrorPropagates(t *testing.T) {
	// Domain events are same-transaction: a handler error must reach the caller so the Unit
	// of Work rolls back. "The invoice recalculates its totals" cannot half-happen.
	b := newBus()
	sentinel := errs.Validation("test.boom", "totals do not balance")
	_ = eventbus.Subscribe(b, "failing", func(context.Context, invoicePosted) error { return sentinel })

	ran := false
	_ = eventbus.Subscribe(b, "later", func(context.Context, invoicePosted) error { ran = true; return nil })

	err := b.Publish(context.Background(), invoicePosted{Invoice: "x"})
	if errs.CodeOf(err) != "test.boom" {
		t.Fatalf("error = %v, want the handler's error", err)
	}
	if ran {
		t.Error("delivery continued after a handler failed; the operation is already doomed")
	}
}

func TestHandlerPanicBecomesErrorAndStillFails(t *testing.T) {
	// Recovering must not mean swallowing: a recovered panic that let the transaction commit
	// would be strictly worse than the crash, because the operation would look successful.
	b := newBus()
	_ = eventbus.Subscribe(b, "panicky", func(context.Context, invoicePosted) error {
		panic("nil map write")
	})

	err := b.Publish(context.Background(), invoicePosted{Invoice: "x"})
	if err == nil {
		t.Fatal("a panicking handler produced no error; the transaction would have committed")
	}
	if errs.CodeOf(err) != eventbus.CodeHandlerPanicked {
		t.Errorf("code = %q, want %q", errs.CodeOf(err), eventbus.CodeHandlerPanicked)
	}
	if !errs.IsCategory(err, errs.CategoryInternal) {
		t.Errorf("category = %v, want Internal", errs.CategoryOf(err))
	}
}

func TestPublishWithNoSubscribersIsNotAnError(t *testing.T) {
	// Modules must be able to raise events before anything listens.
	if err := newBus().Publish(context.Background(), invoicePosted{Invoice: "x"}); err != nil {
		t.Fatalf("Publish with no subscribers: %v", err)
	}
}

func TestEventTypesSorted(t *testing.T) {
	b := newBus()
	_ = eventbus.Subscribe(b, "v", func(context.Context, invoiceVoided) error { return nil })
	_ = eventbus.Subscribe(b, "p", func(context.Context, invoicePosted) error { return nil })

	got := b.EventTypes()
	want := []string{"sales.invoice_posted", "sales.invoice_voided"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("EventTypes = %v, want %v", got, want)
	}
}
