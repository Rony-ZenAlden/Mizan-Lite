// Package customerstest is the in-memory debt book, fakes of its ports, and the contract every store must meet — run
// against this fake AND SQLite (L0 D-L0.3).
package customerstest

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/customers"
	"github.com/mizan-erp/mizan/internal/lite/customers/domain"
)

// Fake is an in-memory customers.Store enforcing the schema's rules the service could lean on.
type Fake struct {
	mu        sync.Mutex
	customers map[id.ID]domain.Customer
	entries   []domain.Entry
}

// NewFake returns an empty store.
func NewFake() *Fake { return &Fake{customers: map[id.ID]domain.Customer{}} }

func (f *Fake) InsertCustomer(_ context.Context, c domain.Customer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, other := range f.customers {
		if other.ID == c.ID || other.NameKey() == c.NameKey() {
			return errs.Conflict("database.duplicate", "unique constraint")
		}
	}
	c.RowVersion = 1
	f.customers[c.ID] = c
	return nil
}

func (f *Fake) UpdateCustomer(_ context.Context, c domain.Customer) (domain.Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, ok := f.customers[c.ID]
	if !ok || stored.RowVersion != c.RowVersion {
		return domain.Customer{}, domain.ErrStale()
	}
	for _, other := range f.customers {
		if other.ID != c.ID && other.NameKey() == c.NameKey() {
			return domain.Customer{}, errs.Conflict("database.duplicate", "unique constraint")
		}
	}
	c.RowVersion++
	c.CreatedAt = stored.CreatedAt
	f.customers[c.ID] = c
	return c, nil
}

func (f *Fake) Customer(_ context.Context, customerID id.ID) (domain.Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.customers[customerID]
	if !ok {
		return domain.Customer{}, domain.ErrNotFound()
	}
	return c, nil
}

func (f *Fake) CustomerByNameKey(_ context.Context, key string) (domain.Customer, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.customers {
		if c.NameKey() == key {
			return c, true, nil
		}
	}
	return domain.Customer{}, false, nil
}

func (f *Fake) Search(_ context.Context, q customers.Query) ([]domain.Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Customer
	for _, c := range f.customers {
		if !c.Active && !q.IncludeInactive {
			continue
		}
		byName := q.Text != "" && strings.Contains(c.NameKey(), q.Text)
		byPhone := q.Phone != "" && strings.Contains(c.Phone, q.Phone)
		if (q.Text != "" || q.Phone != "") && !byName && !byPhone {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NameKey() < out[j].NameKey() })
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (f *Fake) Newest(_ context.Context, customerID id.ID, currency string) (domain.Place, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var p domain.Place
	for _, e := range f.entries {
		if e.CustomerID == customerID && e.Currency == currency && e.Seq > p.Seq {
			p = domain.Place{Seq: e.Seq, BalanceMinor: e.BalanceAfterMinor}
		}
	}
	return p, nil
}

func (f *Fake) InsertEntry(_ context.Context, e domain.Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.customers[e.CustomerID]; !ok {
		return errs.Validation("database.reference_violation", "foreign key")
	}
	for _, other := range f.entries {
		if other.ID == e.ID || (other.CustomerID == e.CustomerID && other.Currency == e.Currency && other.Seq == e.Seq) ||
			(e.SaleID != "" && other.SaleID == e.SaleID) || (e.ReversesID != "" && other.ReversesID == e.ReversesID) {
			return errs.Conflict("database.duplicate", "unique constraint")
		}
	}
	if e.Seq < 1 || e.AmountMinor == 0 || e.BalanceAfterMinor != e.BalanceBeforeMinor+e.AmountMinor ||
		(e.Kind == domain.KindCharge) != (e.SaleID != "") || (e.Kind == domain.KindReversal) != (e.ReversesID != "") ||
		((e.Kind == domain.KindPayment || e.Kind == domain.KindWriteOff) && e.BalanceAfterMinor < 0) {
		return errs.Validation("database.constraint_violation", "check constraint")
	}
	f.entries = append(f.entries, e)
	return nil
}

func (f *Fake) Entry(_ context.Context, entryID id.ID) (domain.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.entries {
		if e.ID == entryID {
			return e, nil
		}
	}
	return domain.Entry{}, errs.NotFound(domain.CodeEntryNotFound, "no such entry")
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

func (f *Fake) ChargeOf(_ context.Context, saleID id.ID) (domain.Entry, bool, error) {
	e, ok := f.find(func(e domain.Entry) bool { return e.Kind == domain.KindCharge && e.SaleID == saleID })
	return e, ok, nil
}

func (f *Fake) ReversalOf(_ context.Context, entryID id.ID) (domain.Entry, bool, error) {
	e, ok := f.find(func(e domain.Entry) bool { return e.Kind == domain.KindReversal && e.ReversesID == entryID })
	return e, ok, nil
}

func (f *Fake) sorted(match func(domain.Entry) bool) []domain.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Entry
	for _, e := range f.entries {
		if match(e) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CustomerID != out[j].CustomerID {
			return out[i].CustomerID < out[j].CustomerID
		}
		if out[i].Currency != out[j].Currency {
			return out[i].Currency < out[j].Currency
		}
		return out[i].Seq < out[j].Seq
	})
	return out
}

func (f *Fake) Chain(_ context.Context, customerID id.ID, currency string) ([]domain.Entry, error) {
	return f.sorted(func(e domain.Entry) bool { return e.CustomerID == customerID && e.Currency == currency }), nil
}

func (f *Fake) ChainCurrencies(_ context.Context, customerID id.ID) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, e := range f.sorted(func(e domain.Entry) bool { return e.CustomerID == customerID }) {
		if !seen[e.Currency] {
			seen[e.Currency] = true
			out = append(out, e.Currency)
		}
	}
	return out, nil
}

func (f *Fake) Owing(ctx context.Context) ([]id.ID, error) {
	newest := map[[2]string]domain.Entry{}
	for _, e := range f.sorted(func(domain.Entry) bool { return true }) {
		newest[[2]string{e.CustomerID.String(), e.Currency}] = e
	}
	seen := map[id.ID]bool{}
	var out []id.ID
	for _, e := range newest {
		if e.BalanceAfterMinor != 0 && !seen[e.CustomerID] {
			seen[e.CustomerID] = true
			out = append(out, e.CustomerID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func (f *Fake) Day(_ context.Context, businessDate string) ([]domain.Entry, error) {
	return f.sorted(func(e domain.Entry) bool { return e.BusinessDate == businessDate }), nil
}

func (f *Fake) Each(_ context.Context, fn func(domain.Entry) error) error {
	for _, e := range f.sorted(func(domain.Entry) bool { return true }) {
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

// Rates is a fake rates port: 15,000 by default.
type Rates struct {
	mu     sync.Mutex
	RateID id.ID
	Nano   int64
	Set    bool
}

// NewRates returns a rate of 15,000.
func NewRates() *Rates {
	rateID, _ := id.New()
	return &Rates{RateID: rateID, Nano: 15_000_000_000_000, Set: true}
}

// Change sets a new rate.
func (r *Rates) Change(nano int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RateID, _ = id.New()
	r.Nano = nano
}

func (r *Rates) InForce(context.Context) (id.ID, int64, string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.RateID, r.Nano, "SYP", r.Set, nil
}

// Settings is fake settings: the 500-pound note.
type Settings struct{ Note int64 }

func (s Settings) CashNote(context.Context) (int64, error) { return s.Note, nil }

// Currencies is the seeded pair.
type Currencies struct{}

func (Currencies) Currencies(context.Context) ([]domain.Currency, error) {
	return []domain.Currency{{Code: "SYP", Decimals: 0}, {Code: "USD", Decimals: 2}}, nil
}

// Gate is a recording owner gate.
type Gate struct {
	mu       sync.Mutex
	Elevated bool
	Acts     []customers.GuardedAct
	Asked    int
}

func (g *Gate) Require(_ context.Context, act customers.GuardedAct) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Asked++
	if !g.Elevated {
		return errs.Permission(customers.CodeOwnerRequired, "the owner's PIN is required")
	}
	g.Acts = append(g.Acts, act)
	return nil
}

func (g *Gate) Allowed(context.Context) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.Elevated
}
