// Package catalogtest is the in-memory catalogue store, a recording owner gate, and the contract every
// store must meet. The contract runs against this fake AND against SQLite (L0 D-L0.3), so the fake cannot
// quietly behave unlike the database it stands in for.
package catalogtest

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/catalog"
	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
)

// ErrInjected is returned once a failure has been armed.
var ErrInjected = errors.New("catalogtest: injected failure")

// Units and Currencies are the seeded reference data, as migration 0002 inserts it.
var (
	Units = []domain.Unit{
		{Code: "kg", Kind: "mass", InputDecimals: 3}, {Code: "l", Kind: "volume", InputDecimals: 3},
		{Code: "piece", Kind: "count"}, {Code: "jar", Kind: "count"}, {Code: "container", Kind: "count"},
		{Code: "tin", Kind: "count"}, {Code: "bag", Kind: "count"}, {Code: "bottle", Kind: "count"},
		{Code: "box", Kind: "count"},
	}
	Currencies = []domain.Currency{{Code: "SYP", Decimals: 0}, {Code: "USD", Decimals: 2}}
)

// Fake is an in-memory catalog.Store enforcing the schema's uniqueness and version rules.
type Fake struct {
	mu         sync.Mutex
	products   map[id.ID]domain.Product
	failUpdate bool
}

// NewFake returns an empty store.
func NewFake() *Fake { return &Fake{products: map[id.ID]domain.Product{}} }

// FailUpdates makes every Update fail.
func (f *Fake) FailUpdates() { f.mu.Lock(); f.failUpdate = true; f.mu.Unlock() }

func (f *Fake) Units(context.Context) ([]domain.Unit, error) {
	return append([]domain.Unit(nil), Units...), nil
}

func (f *Fake) Currencies(context.Context) ([]domain.Currency, error) {
	return append([]domain.Currency(nil), Currencies...), nil
}

func (f *Fake) Get(_ context.Context, productID id.ID) (domain.Product, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.products[productID]
	if !ok {
		return domain.Product{}, domain.ErrNotFound()
	}
	return p, nil
}

func (f *Fake) find(match func(domain.Product) bool) (domain.Product, bool) {
	for _, p := range f.products {
		if match(p) {
			return p, true
		}
	}
	return domain.Product{}, false
}

func (f *Fake) ByNameKey(_ context.Context, key string) (domain.Product, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.find(func(p domain.Product) bool { return p.NameKey() == key })
	return p, ok, nil
}

func (f *Fake) ByBarcode(_ context.Context, barcode string) (domain.Product, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if barcode == "" {
		return domain.Product{}, false, nil
	}
	p, ok := f.find(func(p domain.Product) bool { return p.Barcode == barcode })
	return p, ok, nil
}

func (f *Fake) BySlot(_ context.Context, slot int) (domain.Product, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if slot == 0 {
		return domain.Product{}, false, nil
	}
	p, ok := f.find(func(p domain.Product) bool { return p.QuickSlot == slot })
	return p, ok, nil
}

func (f *Fake) Search(_ context.Context, q catalog.Query) ([]domain.Product, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Product
	for _, p := range f.products {
		if !q.IncludeInactive && !p.Active {
			continue
		}
		if q.Text != "" && !strings.Contains(p.SearchText(), q.Text) {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		ei, ej := q.Barcode != "" && out[i].Barcode == q.Barcode, q.Barcode != "" && out[j].Barcode == q.Barcode
		if ei != ej {
			return ei
		}
		return out[i].NameKey() < out[j].NameKey()
	})
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// violates reports the database error the schema would raise for p, ignoring the row p itself replaces.
func (f *Fake) violates(p domain.Product) error {
	if p.QuickSlot != 0 && !p.Active {
		return errs.Validation("database.constraint_violation", "slot needs active")
	}
	for _, other := range f.products {
		if other.ID == p.ID {
			continue
		}
		if other.NameKey() == p.NameKey() ||
			(p.Barcode != "" && other.Barcode == p.Barcode) ||
			(p.QuickSlot != 0 && other.QuickSlot == p.QuickSlot) {
			return errs.Conflict("database.duplicate", "unique constraint")
		}
	}
	return nil
}

func (f *Fake) Insert(_ context.Context, p domain.Product) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.products[p.ID]; exists {
		return errs.Conflict("database.duplicate", "primary key")
	}
	if err := f.violates(p); err != nil {
		return err
	}
	f.products[p.ID] = p
	return nil
}

func (f *Fake) Update(_ context.Context, p domain.Product) (domain.Product, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failUpdate {
		return domain.Product{}, ErrInjected
	}
	stored, ok := f.products[p.ID]
	if !ok || stored.RowVersion != p.RowVersion {
		return domain.Product{}, domain.ErrStale()
	}
	if err := f.violates(p); err != nil {
		return domain.Product{}, err
	}
	p.RowVersion++
	f.products[p.ID] = p
	return p, nil
}

// Gate is a recording owner gate. With Elevated false it refuses as the owner module would.
type Gate struct {
	mu       sync.Mutex
	Elevated bool
	Acts     []catalog.GuardedAct
	Asked    int
}

// CodeRequired mirrors the owner module's refusal code; the fake cannot import that module.
const CodeRequired = "lite.owner.required"

// Require implements catalog.OwnerGate.
func (g *Gate) Require(_ context.Context, act catalog.GuardedAct) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Asked++
	if !g.Elevated {
		return errs.Permission(CodeRequired, "the owner's PIN is required")
	}
	g.Acts = append(g.Acts, act)
	return nil
}

// StoreContract is the behaviour every catalog.Store must have. newStore returns a fresh, empty store.
func StoreContract(t *testing.T, newStore func(t *testing.T) catalog.Store) {
	t.Helper()
	ctx := context.Background()
	ref := domain.Reference{Units: map[string]domain.Unit{}, Currencies: map[string]domain.Currency{}}
	for _, u := range Units {
		ref.Units[u.Code] = u
	}
	for _, c := range Currencies {
		ref.Currencies[c.Code] = c
	}
	product := func(t *testing.T, nameAR, barcode string) domain.Product {
		t.Helper()
		productID, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		p, err := domain.NewProduct(productID, domain.Draft{
			NameAR: nameAR, NameEN: "English " + nameAR, Barcode: barcode,
			UnitCode: "kg", PriceCurrency: "USD", Price: "3.25",
		}, ref)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("the seeded units and currencies, in display order", func(t *testing.T) {
		s := newStore(t)
		units, err := s.Units(ctx)
		if err != nil || len(units) != len(Units) {
			t.Fatalf("units = %v, %v", units, err)
		}
		for i := range Units {
			if units[i] != Units[i] {
				t.Errorf("unit %d = %+v, want %+v", i, units[i], Units[i])
			}
		}
		currencies, err := s.Currencies(ctx)
		if err != nil || len(currencies) != 2 || currencies[0] != Currencies[0] || currencies[1] != Currencies[1] {
			t.Fatalf("currencies = %v, %v", currencies, err)
		}
	})

	t.Run("an inserted product reads back exactly", func(t *testing.T) {
		s := newStore(t)
		p := product(t, "زيت زيتون", "6223000112345")
		if err := s.Insert(ctx, p); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, p.ID)
		if err != nil || got != p {
			t.Fatalf("Get = %+v, %v\nwant %+v", got, err, p)
		}
	})

	t.Run("a missing product is NotFound", func(t *testing.T) {
		missing, _ := id.New()
		if _, err := newStore(t).Get(ctx, missing); errs.CodeOf(err) != domain.CodeNotFound {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("lookups by name key, barcode and slot", func(t *testing.T) {
		s := newStore(t)
		p := product(t, "إسطنبولي", "ABC1")
		p, _ = p.WithSlot(5)
		if err := s.Insert(ctx, p); err != nil {
			t.Fatal(err)
		}
		if got, ok, err := s.ByNameKey(ctx, "اسطنبولي"); err != nil || !ok || got.ID != p.ID {
			t.Errorf("ByNameKey = %v %v %v", got.ID, ok, err)
		}
		if got, ok, err := s.ByBarcode(ctx, "ABC1"); err != nil || !ok || got.ID != p.ID {
			t.Errorf("ByBarcode = %v %v %v", got.ID, ok, err)
		}
		if got, ok, err := s.BySlot(ctx, 5); err != nil || !ok || got.ID != p.ID {
			t.Errorf("BySlot = %v %v %v", got.ID, ok, err)
		}
		if _, ok, err := s.ByBarcode(ctx, ""); err != nil || ok {
			t.Errorf("an empty barcode matched a product: %v %v", ok, err)
		}
		if _, ok, err := s.BySlot(ctx, 6); err != nil || ok {
			t.Errorf("an empty slot matched: %v %v", ok, err)
		}
	})

	t.Run("two products may both have no barcode", func(t *testing.T) {
		s := newStore(t)
		if err := s.Insert(ctx, product(t, "زيت", "")); err != nil {
			t.Fatal(err)
		}
		if err := s.Insert(ctx, product(t, "سمن", "")); err != nil {
			t.Fatalf("a second product without a barcode was refused: %v", err)
		}
	})

	t.Run("the schema refuses a duplicate name key, barcode or slot", func(t *testing.T) {
		s := newStore(t)
		first := product(t, "أسطنبولي", "B1")
		first, _ = first.WithSlot(1)
		if err := s.Insert(ctx, first); err != nil {
			t.Fatal(err)
		}
		sameName := product(t, "اسطنبولي", "B2")
		if err := s.Insert(ctx, sameName); err == nil {
			t.Error("a second spelling of the same name was stored")
		}
		sameBarcode := product(t, "زعتر", "B1")
		if err := s.Insert(ctx, sameBarcode); err == nil {
			t.Error("a duplicate barcode was stored")
		}
		sameSlot := product(t, "سماق", "B3")
		sameSlot, _ = sameSlot.WithSlot(1)
		if err := s.Insert(ctx, sameSlot); err == nil {
			t.Error("a second product took the same button")
		}
	})

	t.Run("update increments the version and refuses a stale one", func(t *testing.T) {
		s := newStore(t)
		p := product(t, "دبس رمان", "")
		if err := s.Insert(ctx, p); err != nil {
			t.Fatal(err)
		}
		renamed, _ := p.Rename("دبس رمان بلدي", "")
		updated, err := s.Update(ctx, renamed)
		if err != nil || updated.RowVersion != p.RowVersion+1 {
			t.Fatalf("Update = %+v, %v", updated, err)
		}
		if got, _ := s.Get(ctx, p.ID); got != updated {
			t.Fatalf("stored = %+v, want %+v", got, updated)
		}
		if _, err := s.Update(ctx, renamed); errs.CodeOf(err) != "database.concurrent_modification" {
			t.Fatalf("a stale version was written: %v", err)
		}
	})

	t.Run("search matches normalised text, hides inactive by default, ranks an exact barcode first", func(t *testing.T) {
		s := newStore(t)
		oil := product(t, "زيت زيتون", "999")
		ghee := product(t, "سمن بلدي", "")
		ghee, _ = ghee.Rename("سمن بلدي", "Ghee with 999 in its name")
		old := product(t, "زيت قديم", "").Deactivate()
		for _, p := range []domain.Product{oil, ghee, old} {
			if err := s.Insert(ctx, p); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.Search(ctx, catalog.Query{Text: "زيت", Limit: 10})
		if err != nil || len(got) != 1 || got[0].ID != oil.ID {
			t.Fatalf("active search = %v, %v", names(got), err)
		}
		got, _ = s.Search(ctx, catalog.Query{Text: "زيت", IncludeInactive: true, Limit: 10})
		if len(got) != 2 {
			t.Fatalf("search including inactive = %v", names(got))
		}
		got, _ = s.Search(ctx, catalog.Query{Text: "999", Barcode: "999", Limit: 10})
		if len(got) != 2 || got[0].ID != oil.ID {
			t.Fatalf("the exact barcode was not first: %v", names(got))
		}
		got, _ = s.Search(ctx, catalog.Query{Limit: 10})
		if len(got) != 2 {
			t.Fatalf("an empty search lists every active product: %v", names(got))
		}
		got, _ = s.Search(ctx, catalog.Query{Limit: 1})
		if len(got) != 1 {
			t.Fatalf("the limit was not applied: %v", names(got))
		}
	})

	t.Run("search treats LIKE wildcards in the query as text", func(t *testing.T) {
		s := newStore(t)
		sale := product(t, "زيت 50% خصم", "")
		plain := product(t, "زيت 500 مل", "")
		for _, p := range []domain.Product{sale, plain} {
			if err := s.Insert(ctx, p); err != nil {
				t.Fatal(err)
			}
		}
		got, _ := s.Search(ctx, catalog.Query{Text: "50%", Limit: 10})
		if len(got) != 1 || got[0].ID != sale.ID {
			t.Fatalf("'50%%' matched %v — the %% was read as a wildcard", names(got))
		}
		got, _ = s.Search(ctx, catalog.Query{Text: "زيت _0", Limit: 10})
		if len(got) != 0 {
			t.Fatalf("'_' was read as a wildcard: %v", names(got))
		}
	})
}

func names(ps []domain.Product) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.NameAR
	}
	return out
}
