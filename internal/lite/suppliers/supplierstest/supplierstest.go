// Package supplierstest is the in-memory payables store and the contract every store must meet: written once, run
// against the fake AND the SQLite store, so the fake cannot quietly behave differently from the database it stands in for.
package supplierstest

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/suppliers"
	"github.com/mizan-erp/mizan/internal/lite/suppliers/domain"
)

// Fake is an in-memory suppliers.Store.
type Fake struct {
	mu        sync.Mutex
	suppliers map[id.ID]domain.Supplier
	entries   []domain.Entry
	purchases map[id.ID]domain.Purchase
}

// NewFake returns an empty store.
func NewFake() *Fake {
	return &Fake{suppliers: map[id.ID]domain.Supplier{}, purchases: map[id.ID]domain.Purchase{}}
}

var _ suppliers.Store = (*Fake)(nil)

func duplicate() error { return errs.Conflict("database.duplicate", "unique constraint") }

func (f *Fake) InsertSupplier(_ context.Context, s domain.Supplier) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, other := range f.suppliers {
		if other.ID == s.ID || other.NameKey() == s.NameKey() {
			return duplicate()
		}
	}
	s.RowVersion = 1
	f.suppliers[s.ID] = s
	return nil
}

func (f *Fake) UpdateSupplier(_ context.Context, s domain.Supplier) (domain.Supplier, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, ok := f.suppliers[s.ID]
	if !ok || stored.RowVersion != s.RowVersion {
		return domain.Supplier{}, domain.ErrStale()
	}
	for _, other := range f.suppliers {
		if other.ID != s.ID && other.NameKey() == s.NameKey() {
			return domain.Supplier{}, duplicate()
		}
	}
	s.RowVersion++
	s.CreatedAt = stored.CreatedAt
	f.suppliers[s.ID] = s
	return s, nil
}

func (f *Fake) Supplier(_ context.Context, supplierID id.ID) (domain.Supplier, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.suppliers[supplierID]
	if !ok {
		return domain.Supplier{}, domain.ErrNotFound()
	}
	return s, nil
}

func (f *Fake) SupplierByNameKey(_ context.Context, key string) (domain.Supplier, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.suppliers {
		if s.NameKey() == key {
			return s, true, nil
		}
	}
	return domain.Supplier{}, false, nil
}

func (f *Fake) Search(_ context.Context, q suppliers.Query) ([]domain.Supplier, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.Supplier{}
	for _, s := range f.suppliers {
		if !q.IncludeInactive && !s.Active {
			continue
		}
		byName := q.Text != "" && strings.Contains(s.NameKey(), q.Text)
		byPhone := q.Phone != "" && strings.Contains(s.Phone, q.Phone)
		if (q.Text == "" && q.Phone == "") || byName || byPhone {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NameKey() < out[j].NameKey() })
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (f *Fake) Newest(_ context.Context, supplierID id.ID, currency string) (domain.Place, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var p domain.Place
	for _, e := range f.entries {
		if e.SupplierID == supplierID && e.Currency == currency && e.Seq > p.Seq {
			p = domain.Place{Seq: e.Seq, BalanceMinor: e.BalanceAfterMinor}
		}
	}
	return p, nil
}

// InsertEntry holds the rules the table's constraints hold: one entry per place, one reversal per entry, a chain that
// adds up, and a supplier that exists.
func (f *Fake) InsertEntry(_ context.Context, e domain.Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.suppliers[e.SupplierID]; !ok {
		return errs.Validation("database.foreign_key", "no such supplier")
	}
	if e.BalanceAfterMinor != e.BalanceBeforeMinor+e.AmountMinor || e.AmountMinor == 0 {
		return errs.Validation("database.constraint_violation", "check")
	}
	for _, other := range f.entries {
		if other.ID == e.ID || (other.SupplierID == e.SupplierID && other.Currency == e.Currency && other.Seq == e.Seq) ||
			(e.ReversesID != "" && other.ReversesID == e.ReversesID) {
			return duplicate()
		}
	}
	f.entries = append(f.entries, e)
	return nil
}

func (f *Fake) find(match func(domain.Entry) bool) (domain.Entry, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.entries {
		if match(e) {
			return e, true
		}
	}
	return domain.Entry{}, false
}

func (f *Fake) Entry(_ context.Context, entryID id.ID) (domain.Entry, error) {
	e, ok := f.find(func(e domain.Entry) bool { return e.ID == entryID })
	if !ok {
		return domain.Entry{}, domain.ErrEntryNotFound()
	}
	return e, nil
}

func (f *Fake) ReversalOf(_ context.Context, entryID id.ID) (domain.Entry, bool, error) {
	e, ok := f.find(func(e domain.Entry) bool { return e.ReversesID == entryID })
	return e, ok, nil
}

func (f *Fake) PurchaseEntry(_ context.Context, purchaseID id.ID) (domain.Entry, bool, error) {
	e, ok := f.find(func(e domain.Entry) bool { return e.PurchaseID == purchaseID && e.Kind == domain.KindPurchase })
	return e, ok, nil
}

func (f *Fake) Chain(_ context.Context, supplierID id.ID, currency string) ([]domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.Entry{}
	for _, e := range f.entries {
		if e.SupplierID == supplierID && e.Currency == currency {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

func (f *Fake) Balances(context.Context) (map[id.ID]map[string]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	newest := map[id.ID]map[string]domain.Entry{}
	for _, e := range f.entries {
		if newest[e.SupplierID] == nil {
			newest[e.SupplierID] = map[string]domain.Entry{}
		}
		if cur, ok := newest[e.SupplierID][e.Currency]; !ok || e.Seq > cur.Seq {
			newest[e.SupplierID][e.Currency] = e
		}
	}
	out := map[id.ID]map[string]int64{}
	for supplierID, byCurrency := range newest {
		out[supplierID] = map[string]int64{}
		for code, e := range byCurrency {
			out[supplierID][code] = e.BalanceAfterMinor
		}
	}
	return out, nil
}

func (f *Fake) Between(_ context.Context, from, to string) ([]domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.Entry{}
	for _, e := range f.entries {
		if e.BusinessDate >= from && e.BusinessDate <= to {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OccurredAt.Before(out[j].OccurredAt) })
	return out, nil
}

func (f *Fake) NextPurchaseNo(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, p := range f.purchases {
		n = max(n, p.PurchaseNo)
	}
	return n + 1, nil
}

func (f *Fake) InsertPurchase(_ context.Context, p domain.Purchase) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.suppliers[p.SupplierID]; !ok {
		return errs.Validation("database.foreign_key", "no such supplier")
	}
	for _, other := range f.purchases {
		if other.ID == p.ID || other.PurchaseNo == p.PurchaseNo {
			return duplicate()
		}
	}
	for _, l := range p.Lines {
		if (l.StockLedgerID == "") != (l.DamagedMicro == l.QuantityMicro) {
			return errs.Validation("database.constraint_violation", "ck_purchase_lines_receipt")
		}
	}
	p.Status, p.RowVersion = domain.StatusPosted, 1
	p.VoidedAt, p.VoidReason = p.VoidedAt.UTC(), ""
	p.Lines = append([]domain.Line(nil), p.Lines...)
	f.purchases[p.ID] = p
	return nil
}

func (f *Fake) Purchase(_ context.Context, purchaseID id.ID) (domain.Purchase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.purchases[purchaseID]
	if !ok {
		return domain.Purchase{}, domain.ErrPurchaseNotFound()
	}
	p.Lines = append([]domain.Line(nil), p.Lines...)
	return p, nil
}

func (f *Fake) Purchases(_ context.Context, q suppliers.PurchaseQuery) ([]domain.Purchase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []domain.Purchase{}
	for _, p := range f.purchases {
		if (q.SupplierID != "" && p.SupplierID != q.SupplierID) || (q.From != "" && p.BusinessDate < q.From) || (q.To != "" && p.BusinessDate > q.To) {
			continue
		}
		p.Lines = append([]domain.Line(nil), p.Lines...)
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PurchaseNo > out[j].PurchaseNo })
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (f *Fake) VoidPurchase(_ context.Context, p domain.Purchase) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, ok := f.purchases[p.ID]
	if !ok || stored.RowVersion != p.RowVersion || stored.Status != domain.StatusPosted {
		return errs.Conflict("database.concurrent_modification", "stale")
	}
	stored.Status, stored.VoidedAt, stored.VoidReason, stored.RowVersion = domain.StatusVoided, p.VoidedAt, p.VoidReason, stored.RowVersion+1
	f.purchases[p.ID] = stored
	return nil
}

// Immediate is a Transactor that runs fn with no transaction, for service tests over a Fake.
type Immediate struct{}

// Do implements suppliers.Transactor.
func (Immediate) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
