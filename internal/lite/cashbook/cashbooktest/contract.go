package cashbooktest

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/cashbook"
	"github.com/mizan-erp/mizan/internal/lite/cashbook/domain"
)

// Subject is a fresh, empty store, and what a money entry must reference: a rate.
type Subject struct {
	Store   cashbook.Store
	NewRate func(t *testing.T) id.ID
}

func newID(t *testing.T) id.ID {
	t.Helper()
	v, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// StoreContract is the behaviour every cashbook.Store must have.
func StoreContract(t *testing.T, newSubject func(t *testing.T) Subject) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 9, 14, 17, 0, 0, 123_000_000, time.UTC)

	put := func(t *testing.T, s cashbook.Store, e domain.Entry) domain.Entry {
		t.Helper()
		var err error
		e.ID = newID(t)
		if e.Seq, err = s.NextSeq(ctx); err != nil {
			t.Fatal(err)
		}
		e.OccurredAt = at
		if err = s.Insert(ctx, e); err != nil {
			t.Fatalf("Insert(%s): %v", e.Kind, err)
		}
		return e
	}

	t.Run("every kind reads back exactly, by place, and a range holds its dates", func(t *testing.T) {
		sub := newSubject(t)
		rate := sub.NewRate(t)
		expense := put(t, sub.Store, domain.Entry{BusinessDate: "2026-09-13", Kind: domain.KindExpense, Currency: "SYP", AmountMinor: 250_000,
			Category: "electricity", FromDrawer: true, RateID: rate, RateNano: 15_000_000_000_000, Note: "فاتورة"})
		deposit := put(t, sub.Store, domain.Entry{BusinessDate: "2026-09-14", Kind: domain.KindDeposit, Currency: "USD", AmountMinor: 5_000,
			RateID: rate, RateNano: 15_000_000_000_000})
		count := put(t, sub.Store, domain.Entry{BusinessDate: "2026-09-14", Kind: domain.KindCount, Currency: "SYP", AmountMinor: 0, ExpectedMinor: 1_000})
		reversal := put(t, sub.Store, domain.Entry{BusinessDate: "2026-09-15", Kind: domain.KindReversal, Currency: "SYP", AmountMinor: 0,
			ReversesID: count.ID, Note: "عدّ خطأ"})
		for _, want := range []domain.Entry{expense, deposit, count, reversal} {
			got, err := sub.Store.Entry(ctx, want.ID)
			if err != nil || !got.OccurredAt.Equal(want.OccurredAt) {
				t.Fatalf("Entry = %+v, %v", got, err)
			}
			got.OccurredAt = want.OccurredAt
			if got != want {
				t.Fatalf("read back\n%+v\nwant\n%+v", got, want)
			}
		}
		if n, _ := sub.Store.NextSeq(ctx); n != 5 {
			t.Fatalf("NextSeq = %d", n)
		}
		if r, _ := sub.Store.Between(ctx, "2026-09-14", "2026-09-15"); len(r) != 3 || r[0].ID != deposit.ID || r[2].ID != reversal.ID {
			t.Fatalf("Between = %+v", r)
		}
		if got, found, _ := sub.Store.ReversalOf(ctx, count.ID); !found || got.ID != reversal.ID {
			t.Fatal("ReversalOf")
		}
		if _, found, _ := sub.Store.ReversalOf(ctx, expense.ID); found {
			t.Fatal("a reversal of an entry nobody reversed")
		}
		if _, err := sub.Store.Entry(ctx, newID(t)); errs.CodeOf(err) != domain.CodeEntryNotFound {
			t.Fatalf("a missing entry: %v", err)
		}
	})

	t.Run("counts before a date, of one currency; a place and a reversal are used once", func(t *testing.T) {
		sub := newSubject(t)
		c1 := put(t, sub.Store, domain.Entry{BusinessDate: "2026-09-12", Kind: domain.KindCount, Currency: "SYP", AmountMinor: 100, ExpectedMinor: 100})
		put(t, sub.Store, domain.Entry{BusinessDate: "2026-09-13", Kind: domain.KindCount, Currency: "USD", AmountMinor: 100, ExpectedMinor: 100})
		c3 := put(t, sub.Store, domain.Entry{BusinessDate: "2026-09-13", Kind: domain.KindCount, Currency: "SYP", AmountMinor: 200, ExpectedMinor: 200})
		put(t, sub.Store, domain.Entry{BusinessDate: "2026-09-14", Kind: domain.KindCount, Currency: "SYP", AmountMinor: 300, ExpectedMinor: 300})
		counts, err := sub.Store.CountsBefore(ctx, "2026-09-14", "SYP")
		if err != nil || len(counts) != 2 || counts[0].ID != c1.ID || counts[1].ID != c3.ID {
			t.Fatalf("CountsBefore = %+v, %v", counts, err)
		}
		dup := c3
		dup.ID = newID(t)
		if err := sub.Store.Insert(ctx, dup); err == nil {
			t.Fatal("a place used twice")
		}
		put(t, sub.Store, domain.Entry{BusinessDate: "2026-09-14", Kind: domain.KindReversal, Currency: "SYP", AmountMinor: 200, ReversesID: c3.ID, Note: "x"})
		again := domain.Entry{ID: newID(t), BusinessDate: "2026-09-14", Kind: domain.KindReversal, Currency: "SYP", AmountMinor: 200, ReversesID: c3.ID, Note: "y", OccurredAt: at}
		again.Seq, _ = sub.Store.NextSeq(ctx)
		if err := sub.Store.Insert(ctx, again); err == nil {
			t.Fatal("an entry reversed twice")
		}
	})
}
