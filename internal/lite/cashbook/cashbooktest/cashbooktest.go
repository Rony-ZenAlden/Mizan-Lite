// Package cashbooktest is the in-memory cash book, fakes of its ports, and the contract every store must meet — run
// against this fake AND SQLite (L0 D-L0.3).
package cashbooktest

import (
	"context"
	"sort"
	"sync"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/cashbook"
	"github.com/mizan-erp/mizan/internal/lite/cashbook/domain"
)

// Fake is an in-memory cashbook.Store enforcing the schema's rules the service could lean on.
type Fake struct {
	mu      sync.Mutex
	entries []domain.Entry
}

// NewFake returns an empty book.
func NewFake() *Fake { return &Fake{} }

func (f *Fake) NextSeq(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.entries)) + 1, nil
}

func (f *Fake) Insert(_ context.Context, e domain.Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.entries {
		if o.ID == e.ID || o.Seq == e.Seq || (e.ReversesID != "" && o.ReversesID == e.ReversesID) {
			return errs.Conflict("database.duplicate", "unique constraint")
		}
	}
	if e.Seq < 1 || e.AmountMinor < 0 || (e.Kind != domain.KindCount && e.Kind != domain.KindReversal && e.AmountMinor == 0) ||
		(e.Kind == domain.KindReversal) != (e.ReversesID != "") {
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

func (f *Fake) ReversalOf(_ context.Context, entryID id.ID) (domain.Entry, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.entries {
		if e.ReversesID == entryID {
			return e, true, nil
		}
	}
	return domain.Entry{}, false, nil
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
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

func (f *Fake) Between(_ context.Context, from, to string) ([]domain.Entry, error) {
	return f.sorted(func(e domain.Entry) bool { return e.BusinessDate >= from && e.BusinessDate <= to }), nil
}

func (f *Fake) CountsBefore(_ context.Context, businessDate, currency string) ([]domain.Entry, error) {
	return f.sorted(func(e domain.Entry) bool {
		return e.Kind == domain.KindCount && e.Currency == currency && e.BusinessDate < businessDate
	}), nil
}

// Rates is a fake rates port at 15,000.
type Rates struct {
	RateID id.ID
	Nano   int64
	Set    bool
}

// NewRates returns a rate of 15,000.
func NewRates() *Rates {
	rateID, _ := id.New()
	return &Rates{RateID: rateID, Nano: 15_000_000_000_000, Set: true}
}

func (r *Rates) InForce(context.Context) (id.ID, int64, string, bool, error) {
	return r.RateID, r.Nano, "SYP", r.Set, nil
}

// Currencies is the seeded pair.
type Currencies struct{}

func (Currencies) Currencies(context.Context) ([]domain.Currency, error) {
	return []domain.Currency{{Code: "SYP", Decimals: 0}, {Code: "USD", Decimals: 2}}, nil
}

// Gate is a recording owner gate.
type Gate struct {
	mu       sync.Mutex
	Elevated bool
	Acts     []cashbook.GuardedAct
}

func (g *Gate) Require(_ context.Context, act cashbook.GuardedAct) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.Elevated {
		return errs.Permission(cashbook.CodeOwnerRequired, "the owner's PIN is required")
	}
	g.Acts = append(g.Acts, act)
	return nil
}

// Expected is a fake drawer figure per currency.
type Expected struct {
	mu    sync.Mutex
	Cash  map[string]int64
	Asked []string
}

func (e *Expected) ExpectedCash(_ context.Context, businessDate, currency string) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Asked = append(e.Asked, businessDate+" "+currency)
	return e.Cash[currency], nil
}
