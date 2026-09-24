package supplierstest

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/suppliers"
	"github.com/mizan-erp/mizan/internal/lite/suppliers/domain"
)

// Subject is a store under test, with what its rows must point at: a product, and a receipt in the stock book.
type Subject struct {
	Store      suppliers.Store
	NewProduct func(t *testing.T) id.ID
	NewReceipt func(t *testing.T, productID id.ID) id.ID
}

func mustID(t *testing.T) id.ID {
	t.Helper()
	v, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// ASupplier is a supplier as the service would create one.
func ASupplier(t *testing.T, name string) domain.Supplier {
	t.Helper()
	s, err := domain.NewSupplier(mustID(t), domain.Draft{Name: name, Phone: "0988 703 785", City: "حلب", Note: "مبيعات"})
	if err != nil {
		t.Fatal(err)
	}
	s.CreatedAt = time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
	return s
}

// place appends e to its chain in store, as the service would.
func place(ctx context.Context, t *testing.T, store suppliers.Store, e domain.Entry) domain.Entry {
	t.Helper()
	p, err := store.Newest(ctx, e.SupplierID, e.Currency)
	if err != nil {
		t.Fatal(err)
	}
	placed, err := p.Append(e)
	if err != nil {
		t.Fatal(err)
	}
	placed.ID = mustID(t)
	placed.BusinessDate = "2026-09-24"
	placed.OccurredAt = time.Date(2026, 9, 24, 8, 0, int(placed.Seq), 0, time.UTC)
	if err = store.InsertEntry(ctx, placed); err != nil {
		t.Fatalf("inserting %s: %v", placed.Kind, err)
	}
	return placed
}

// APurchase is a purchase of two lines — one received, one wholly damaged — as the service would record it.
func APurchase(t *testing.T, sub Subject, supplier domain.Supplier, no int64) domain.Purchase {
	t.Helper()
	cups, plates := sub.NewProduct(t), sub.NewProduct(t)
	return domain.Purchase{ID: mustID(t), PurchaseNo: no, SupplierID: supplier.ID, SupplierName: supplier.Name, BusinessDate: "2026-09-24",
		OccurredAt: time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC), Currency: "USD", SupplierRef: "2970",
		GrossMinor: 10_800, LineDiscountMinor: 1_080, InvoiceDiscountMinor: 20, PaidNowMinor: 5_000, PaidFrom: domain.SourceDrawer,
		Status: domain.StatusPosted, Note: "delivered", RowVersion: 1,
		Lines: []domain.Line{
			{ID: mustID(t), LineNo: 1, ProductID: cups, NameAR: "فنجان قهوة", NameEN: "Coffee cup", UnitCode: "jar",
				QuantityMicro: 8_000_000, DamagedMicro: 2_000_000, UnitCostMicro: 18_000_000, DiscountPercentMicro: 10_000_000,
				GrossMinor: 10_800, LineDiscountMinor: 1_080, InvoiceShareMinor: 20, StockLedgerID: sub.NewReceipt(t, cups)},
			{ID: mustID(t), LineNo: 2, ProductID: plates, NameAR: "صحون", UnitCode: "piece",
				QuantityMicro: 3_000_000, DamagedMicro: 3_000_000, UnitCostMicro: 5_000_000},
		}}
}

// StoreContract is the behaviour every suppliers.Store must have.
func StoreContract(t *testing.T, newSubject func(t *testing.T) Subject) {
	t.Helper()
	ctx := context.Background()

	t.Run("a supplier reads back, is found by name key and by phone, and edits at its version", func(t *testing.T) {
		sub := newSubject(t)
		s := ASupplier(t, "المروى")
		if err := sub.Store.InsertSupplier(ctx, s); err != nil {
			t.Fatal(err)
		}
		got, err := sub.Store.Supplier(ctx, s.ID)
		if err != nil || got.Name != s.Name || got.Phone != s.Phone || got.City != "حلب" || got.Note != s.Note || !got.Active ||
			got.RowVersion != 1 || !got.CreatedAt.Equal(s.CreatedAt) {
			t.Fatalf("Supplier = %+v, %v", got, err)
		}
		if byKey, found, _ := sub.Store.SupplierByNameKey(ctx, s.NameKey()); !found || byKey.ID != s.ID {
			t.Fatal("not found by its normalised name")
		}
		if found, _ := sub.Store.Search(ctx, suppliers.Query{Phone: "703"}); len(found) != 1 {
			t.Fatalf("by phone = %+v", found)
		}
		if err = sub.Store.InsertSupplier(ctx, ASupplier(t, "المروى")); err == nil {
			t.Fatal("two suppliers with one name key")
		}
		s.City, s.Active = "", false
		updated, err := sub.Store.UpdateSupplier(ctx, s)
		if err != nil || updated.RowVersion != 2 {
			t.Fatalf("Update = %+v, %v", updated, err)
		}
		if _, err := sub.Store.UpdateSupplier(ctx, s); errs.CodeOf(err) != "database.concurrent_modification" {
			t.Fatalf("a stale update: %v", err)
		}
		if found, _ := sub.Store.Search(ctx, suppliers.Query{}); len(found) != 0 {
			t.Fatalf("an inactive supplier was listed: %+v", found)
		}
		if again, _ := sub.Store.Supplier(ctx, s.ID); again.City != "" {
			t.Fatalf("an emptied city reads back %q", again.City)
		}
		if _, err := sub.Store.Supplier(ctx, mustID(t)); errs.CodeOf(err) != domain.CodeNotFound {
			t.Fatalf("a missing supplier: %v", err)
		}
	})

	t.Run("the book's chains read back in place order, with balances, reversals and the day", func(t *testing.T) {
		sub := newSubject(t)
		s := ASupplier(t, "المروى")
		if err := sub.Store.InsertSupplier(ctx, s); err != nil {
			t.Fatal(err)
		}
		opening := place(ctx, t, sub.Store, domain.Entry{SupplierID: s.ID, Currency: "USD", Kind: domain.KindOpening, AmountMinor: 20_000, SupplierName: s.Name})
		pay := place(ctx, t, sub.Store, domain.Entry{SupplierID: s.ID, Currency: "USD", Kind: domain.KindPayment, AmountMinor: -5_000,
			Source: domain.SourceDrawer, SupplierName: s.Name, Note: "cash"})
		place(ctx, t, sub.Store, domain.Entry{SupplierID: s.ID, Currency: "SYP", Kind: domain.KindOpening, AmountMinor: 90_000, SupplierName: s.Name})
		rev := place(ctx, t, sub.Store, domain.Entry{SupplierID: s.ID, Currency: "USD", Kind: domain.KindReversal, AmountMinor: 5_000,
			ReversesID: pay.ID, Source: domain.SourceDrawer, SupplierName: s.Name, Note: "twice"})

		chain, err := sub.Store.Chain(ctx, s.ID, "USD")
		if err != nil || len(chain) != 3 || chain[0].ID != opening.ID || chain[2].ID != rev.ID || chain[2].BalanceAfterMinor != 20_000 {
			t.Fatalf("Chain = %+v, %v", chain, err)
		}
		if got := chain[1]; got.Source != domain.SourceDrawer || got.Note != "cash" || got.SupplierName != s.Name || !got.OccurredAt.Equal(pay.OccurredAt) {
			t.Fatalf("a payment reads back as %+v", got)
		}
		if newest, _ := sub.Store.Newest(ctx, s.ID, "USD"); newest.Seq != 3 || newest.BalanceMinor != 20_000 {
			t.Fatalf("Newest = %+v", newest)
		}
		if got, found, _ := sub.Store.ReversalOf(ctx, pay.ID); !found || got.ID != rev.ID {
			t.Fatal("the reversal is not found by what it reverses")
		}
		balances, _ := sub.Store.Balances(ctx)
		if balances[s.ID]["USD"] != 20_000 || balances[s.ID]["SYP"] != 90_000 {
			t.Fatalf("Balances = %+v", balances)
		}
		if day, _ := sub.Store.Between(ctx, "2026-09-24", "2026-09-24"); len(day) != 4 {
			t.Fatalf("the day's entries: %d", len(day))
		}
		if none, _ := sub.Store.Between(ctx, "2026-09-25", "2026-12-31"); len(none) != 0 {
			t.Fatalf("entries of days that have not come: %d", len(none))
		}
		// One place per chain and one reversal per entry: a second of either is refused.
		dup := pay
		dup.ID = mustID(t)
		if err := sub.Store.InsertEntry(ctx, dup); err == nil {
			t.Fatal("a place used twice")
		}
	})

	t.Run("a purchase reads back with its lines, is listed newest first, and voids once at its version", func(t *testing.T) {
		sub := newSubject(t)
		s := ASupplier(t, "المروى")
		if err := sub.Store.InsertSupplier(ctx, s); err != nil {
			t.Fatal(err)
		}
		if n, err := sub.Store.NextPurchaseNo(ctx); err != nil || n != 1 {
			t.Fatalf("the first purchase number = %d, %v", n, err)
		}
		first := APurchase(t, sub, s, 1)
		if err := sub.Store.InsertPurchase(ctx, first); err != nil {
			t.Fatal(err)
		}
		got, err := sub.Store.Purchase(ctx, first.ID)
		if err != nil || !samePurchase(got, first) {
			t.Fatalf("Purchase = %+v, %v\nwant %+v", got, err, first)
		}
		entry := place(ctx, t, sub.Store, domain.Entry{SupplierID: s.ID, Currency: "USD", Kind: domain.KindPurchase,
			AmountMinor: first.DueMinor(), PurchaseID: first.ID, SupplierName: s.Name})
		if e, found, _ := sub.Store.PurchaseEntry(ctx, first.ID); !found || e.ID != entry.ID {
			t.Fatal("the purchase's entry is not found by the purchase")
		}
		second := APurchase(t, sub, s, 2)
		second.BusinessDate = "2026-09-25"
		if err = sub.Store.InsertPurchase(ctx, second); err != nil {
			t.Fatal(err)
		}
		if err = sub.Store.InsertPurchase(ctx, APurchase(t, sub, s, 2)); err == nil {
			t.Fatal("purchase number 2 was used twice")
		}
		list, err := sub.Store.Purchases(ctx, suppliers.PurchaseQuery{SupplierID: s.ID})
		if err != nil || len(list) != 2 || list[0].PurchaseNo != 2 || len(list[1].Lines) != 2 {
			t.Fatalf("Purchases = %+v, %v", list, err)
		}
		if one, _ := sub.Store.Purchases(ctx, suppliers.PurchaseQuery{From: "2026-09-25"}); len(one) != 1 || one[0].ID != second.ID {
			t.Fatalf("by date = %+v", one)
		}
		void := got
		void.Status, void.VoidedAt, void.VoidReason = domain.StatusVoided, time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), "entered twice"
		if err := sub.Store.VoidPurchase(ctx, void); err != nil {
			t.Fatal(err)
		}
		if err := sub.Store.VoidPurchase(ctx, void); err == nil {
			t.Fatal("a purchase voided twice")
		}
		after, _ := sub.Store.Purchase(ctx, first.ID)
		if after.Status != domain.StatusVoided || after.VoidReason != "entered twice" || !after.VoidedAt.Equal(void.VoidedAt) || after.RowVersion != 2 {
			t.Fatalf("after the void: %+v", after)
		}
		if _, err := sub.Store.Purchase(ctx, mustID(t)); errs.CodeOf(err) != domain.CodePurchaseNotFound {
			t.Fatalf("a missing purchase: %v", err)
		}
	})
}

func samePurchase(a, b domain.Purchase) bool {
	if len(a.Lines) != len(b.Lines) || !a.OccurredAt.Equal(b.OccurredAt) || !a.VoidedAt.Equal(b.VoidedAt) || a.VoidReason != b.VoidReason {
		return false
	}
	for i := range a.Lines {
		if a.Lines[i] != b.Lines[i] {
			return false
		}
	}
	return a.ID == b.ID && a.PurchaseNo == b.PurchaseNo && a.SupplierID == b.SupplierID && a.SupplierName == b.SupplierName &&
		a.BusinessDate == b.BusinessDate && a.Currency == b.Currency && a.RateNano == b.RateNano && a.SupplierRef == b.SupplierRef &&
		a.GrossMinor == b.GrossMinor && a.LineDiscountMinor == b.LineDiscountMinor && a.InvoiceDiscountMinor == b.InvoiceDiscountMinor &&
		a.PaidNowMinor == b.PaidNowMinor && a.PaidFrom == b.PaidFrom && a.Status == b.Status && a.Note == b.Note && a.RowVersion == b.RowVersion
}
