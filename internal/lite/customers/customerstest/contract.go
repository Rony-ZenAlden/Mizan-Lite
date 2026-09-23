package customerstest

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/customers"
	"github.com/mizan-erp/mizan/internal/lite/customers/domain"
)

// Subject is a fresh, empty store, and what an entry must reference: a sale and a rate.
type Subject struct {
	Store   customers.Store
	NewSale func(t *testing.T) id.ID
	NewRate func(t *testing.T) id.ID
}

func mustID(t *testing.T) id.ID {
	t.Helper()
	v, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// ACustomer is a customer as the service would create one.
func ACustomer(t *testing.T, name string) domain.Customer {
	t.Helper()
	c, err := domain.NewCustomer(mustID(t), domain.Draft{Name: name, Phone: "0933 123 456", Note: "الحلاق", City: "حلب"})
	if err != nil {
		t.Fatal(err)
	}
	c.CreatedAt = time.Date(2026, 9, 14, 7, 0, 0, 0, time.UTC)
	return c
}

// place appends e to its chain in store, as the service would.
func place(ctx context.Context, t *testing.T, store customers.Store, e domain.Entry) domain.Entry {
	t.Helper()
	p, err := store.Newest(ctx, e.CustomerID, e.Currency)
	if err != nil {
		t.Fatal(err)
	}
	placed, err := p.Append(e)
	if err != nil {
		t.Fatal(err)
	}
	placed.ID = mustID(t)
	if placed.BusinessDate == "" {
		placed.BusinessDate = "2026-09-14"
	}
	if placed.OccurredAt.IsZero() {
		placed.OccurredAt = time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	}
	if err = store.InsertEntry(ctx, placed); err != nil {
		t.Fatalf("inserting %s: %v", placed.Kind, err)
	}
	return placed
}

// StoreContract is the behaviour every customers.Store must have.
func StoreContract(t *testing.T, newSubject func(t *testing.T) Subject) {
	t.Helper()
	ctx := context.Background()

	t.Run("a customer reads back, is found by name key and by phone, and edits at its version", func(t *testing.T) {
		sub := newSubject(t)
		c := ACustomer(t, "أبو محمد")
		if err := sub.Store.InsertCustomer(ctx, c); err != nil {
			t.Fatal(err)
		}
		got, err := sub.Store.Customer(ctx, c.ID)
		if err != nil || got.Name != c.Name || got.Phone != c.Phone || got.Note != c.Note || got.City != "حلب" || !got.Active || got.RowVersion != 1 || !got.CreatedAt.Equal(c.CreatedAt) {
			t.Fatalf("Customer = %+v, %v", got, err)
		}
		if byKey, found, _ := sub.Store.CustomerByNameKey(ctx, "ابو محمد"); !found || byKey.ID != c.ID {
			t.Fatal("not found by its normalised name")
		}
		if found, _ := sub.Store.Search(ctx, customers.Query{Phone: "123"}); len(found) != 1 {
			t.Fatalf("by phone = %+v", found)
		}
		if found, _ := sub.Store.Search(ctx, customers.Query{Text: "محمد"}); len(found) != 1 {
			t.Fatalf("by name = %+v", found)
		}
		if err = sub.Store.InsertCustomer(ctx, ACustomer(t, "ابو محمد")); err == nil {
			t.Fatal("two customers with one name key")
		}
		c.Phone, c.City, c.Active = "", "", false
		updated, err := sub.Store.UpdateCustomer(ctx, c)
		if err != nil || updated.RowVersion != 2 {
			t.Fatalf("Update = %+v, %v", updated, err)
		}
		// An emptied city reads back empty — stored as nothing, not as an empty string the CHECK refuses.
		if again, _ := sub.Store.Customer(ctx, c.ID); again.City != "" {
			t.Fatalf("the city was not cleared: %q", again.City)
		}
		if _, err := sub.Store.UpdateCustomer(ctx, c); errs.CodeOf(err) != "database.concurrent_modification" {
			t.Fatalf("a stale update: %v", err)
		}
		if found, _ := sub.Store.Search(ctx, customers.Query{}); len(found) != 0 {
			t.Fatal("an inactive customer listed")
		}
		if found, _ := sub.Store.Search(ctx, customers.Query{IncludeInactive: true}); len(found) != 1 || found[0].Phone != "" {
			t.Fatalf("include inactive = %+v", found)
		}
		if _, err := sub.Store.Customer(ctx, mustID(t)); errs.CodeOf(err) != domain.CodeNotFound {
			t.Fatalf("a missing customer: %v", err)
		}
	})

	t.Run("an entry reads back exactly, and the newest place is the chain's last", func(t *testing.T) {
		sub := newSubject(t)
		c := ACustomer(t, "سمير")
		if err := sub.Store.InsertCustomer(ctx, c); err != nil {
			t.Fatal(err)
		}
		if p, err := sub.Store.Newest(ctx, c.ID, "USD"); err != nil || p != (domain.Place{}) {
			t.Fatalf("an empty chain = %+v, %v", p, err)
		}
		opening := place(ctx, t, sub.Store, domain.Entry{CustomerID: c.ID, Currency: "USD", Kind: domain.KindOpening, AmountMinor: 1_625, CustomerName: c.Name, Note: "صفحة 12"})
		payment := place(ctx, t, sub.Store, domain.Entry{CustomerID: c.ID, Currency: "USD", Kind: domain.KindPayment, AmountMinor: -667, CustomerName: c.Name,
			Cash:       domain.Cash{RateID: sub.NewRate(t), RateNano: 15_000_000_000_000, CashNoteMinor: 500, TenderedCurrency: "SYP", TenderedMinor: 100_000, ChangeCurrency: "SYP"},
			OccurredAt: time.Date(2026, 9, 14, 9, 30, 0, 123_000_000, time.UTC)})
		got, err := sub.Store.Entry(ctx, payment.ID)
		if err != nil || !sameEntry(got, payment) {
			t.Fatalf("Entry = %+v, %v\nwant %+v", got, err, payment)
		}
		if p, _ := sub.Store.Newest(ctx, c.ID, "USD"); p != (domain.Place{Seq: 2, BalanceMinor: 958}) {
			t.Fatalf("newest = %+v", p)
		}
		pounds := place(ctx, t, sub.Store, domain.Entry{CustomerID: c.ID, Currency: "SYP", Kind: domain.KindOpening, AmountMinor: 100_000, CustomerName: c.Name})
		if pounds.Seq != 1 {
			t.Fatal("the pounds chain did not start at place 1")
		}
		chain, _ := sub.Store.Chain(ctx, c.ID, "USD")
		if len(chain) != 2 || !sameEntry(chain[0], opening) || chain[1].ID != payment.ID {
			t.Fatalf("Chain = %+v", chain)
		}
		if cur, _ := sub.Store.ChainCurrencies(ctx, c.ID); len(cur) != 2 {
			t.Fatalf("ChainCurrencies = %v", cur)
		}
		if _, err := sub.Store.Entry(ctx, mustID(t)); errs.CodeOf(err) != domain.CodeEntryNotFound {
			t.Fatalf("a missing entry: %v", err)
		}
		dup := payment
		dup.ID = mustID(t)
		if err := sub.Store.InsertEntry(ctx, dup); err == nil {
			t.Fatal("two entries in one place")
		}
	})

	t.Run("a sale has one charge; an entry one reversal; owing and the day are what the chains say", func(t *testing.T) {
		sub := newSubject(t)
		a, b := ACustomer(t, "سمير"), ACustomer(t, "خالد")
		for _, c := range []domain.Customer{a, b} {
			if err := sub.Store.InsertCustomer(ctx, c); err != nil {
				t.Fatal(err)
			}
		}
		sale := sub.NewSale(t)
		charge := place(ctx, t, sub.Store, domain.Entry{CustomerID: a.ID, Currency: "USD", Kind: domain.KindCharge, AmountMinor: 800, SaleID: sale, CustomerName: a.Name})
		if got, found, err := sub.Store.ChargeOf(ctx, sale); err != nil || !found || got.ID != charge.ID {
			t.Fatalf("ChargeOf = %+v %v %v", got, found, err)
		}
		second := charge
		second.ID, second.Seq, second.BalanceBeforeMinor, second.BalanceAfterMinor = mustID(t), 2, 800, 1_600
		if err := sub.Store.InsertEntry(ctx, second); err == nil {
			t.Fatal("a second charge for one sale")
		}
		opening := place(ctx, t, sub.Store, domain.Entry{CustomerID: b.ID, Currency: "SYP", Kind: domain.KindOpening, AmountMinor: 50_000, CustomerName: b.Name, BusinessDate: "2026-09-13"})
		if owing, _ := sub.Store.Owing(ctx); len(owing) != 2 {
			t.Fatalf("Owing = %v", owing)
		}
		reversal := place(ctx, t, sub.Store, domain.Entry{CustomerID: b.ID, Currency: "SYP", Kind: domain.KindReversal, AmountMinor: -50_000, ReversesID: opening.ID, CustomerName: b.Name, Note: "خطأ"})
		if got, found, _ := sub.Store.ReversalOf(ctx, opening.ID); !found || got.ID != reversal.ID {
			t.Fatal("ReversalOf")
		}
		again := reversal
		again.ID, again.Seq, again.BalanceBeforeMinor, again.BalanceAfterMinor = mustID(t), 3, 0, -50_000
		if err := sub.Store.InsertEntry(ctx, again); err == nil {
			t.Fatal("an entry reversed twice")
		}
		if owing, _ := sub.Store.Owing(ctx); len(owing) != 1 || owing[0] != a.ID {
			t.Fatalf("Owing after the reversal = %v", owing)
		}
		if day, _ := sub.Store.Day(ctx, "2026-09-13"); len(day) != 1 || day[0].ID != opening.ID {
			t.Fatalf("Day = %+v", day)
		}
		var walked int
		if err := sub.Store.Each(ctx, func(domain.Entry) error { walked++; return nil }); err != nil || walked != 3 {
			t.Fatalf("Each walked %d, %v", walked, err)
		}
		if _, found, _ := sub.Store.ChargeOf(ctx, mustID(t)); found {
			t.Fatal("a charge for a sale that has none")
		}
	})
}

func sameEntry(a, b domain.Entry) bool {
	if !a.OccurredAt.Equal(b.OccurredAt) {
		return false
	}
	a.OccurredAt, b.OccurredAt = time.Time{}, time.Time{}
	return a == b
}
