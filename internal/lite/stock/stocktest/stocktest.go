// Package stocktest is the in-memory stock store, a fake catalogue, a recording owner gate, and the contract every
// store must meet. The contract runs against this fake AND against SQLite (L0 D-L0.3), so the fake cannot quietly
// behave unlike the database it stands in for.
package stocktest

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/stock"
	"github.com/mizan-erp/mizan/internal/lite/stock/domain"
)

// ErrInjected is returned once a failure has been armed.
var ErrInjected = errors.New("stocktest: injected failure")

// Currencies are the seeded currencies, as migration 0002 inserts them.
var Currencies = []domain.Currency{{Code: "SYP", Decimals: 0}, {Code: "USD", Decimals: 2}}

// Fake is an in-memory stock.Store enforcing the schema's rules that the service could otherwise lean on: one place
// per product, a reversal once, a level's version.
type Fake struct {
	mu              sync.Mutex
	levels          map[id.ID]domain.Level
	movements       []domain.Movement
	failSaveLevelAt int // 0: never; n: the n-th SaveLevel fails
	saves           int
}

// NewFake returns an empty store.
func NewFake() *Fake { return &Fake{levels: map[id.ID]domain.Level{}} }

// FailSaveLevel makes the n-th SaveLevel from now fail, after its movement was appended.
func (f *Fake) FailSaveLevel(n int) { f.mu.Lock(); f.failSaveLevelAt, f.saves = n, 0; f.mu.Unlock() }

// Snapshot returns every stored movement in the order appended.
func (f *Fake) Snapshot() []domain.Movement {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Movement(nil), f.movements...)
}

// Plant replaces the stored level as it is — for verifier tests that need a level no act would write.
func (f *Fake) Plant(l domain.Level) { f.mu.Lock(); f.levels[l.ProductID] = l; f.mu.Unlock() }

func (f *Fake) Level(_ context.Context, productID id.ID) (domain.Level, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if l, ok := f.levels[productID]; ok {
		return l, nil
	}
	return domain.Level{ProductID: productID}, nil
}

func (f *Fake) Levels(context.Context) ([]domain.Level, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Level, 0, len(f.levels))
	for _, l := range f.levels {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProductID < out[j].ProductID })
	return out, nil
}

func (f *Fake) Movement(_ context.Context, movementID id.ID) (domain.Movement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.movements {
		if m.ID == movementID {
			return m, nil
		}
	}
	return domain.Movement{}, domain.ErrMovementNotFound()
}

func (f *Fake) Movements(_ context.Context, productID id.ID, limit int) ([]domain.Movement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Movement
	for i := len(f.movements) - 1; i >= 0; i-- {
		if f.movements[i].ProductID == productID {
			out = append(out, f.movements[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq > out[j].Seq })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *Fake) EachMovement(_ context.Context, fn func(domain.Movement) error) error {
	f.mu.Lock()
	ordered := append([]domain.Movement(nil), f.movements...)
	f.mu.Unlock()
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ProductID != ordered[j].ProductID {
			return ordered[i].ProductID < ordered[j].ProductID
		}
		return ordered[i].Seq < ordered[j].Seq
	})
	for _, m := range ordered {
		if err := fn(m); err != nil {
			return err
		}
	}
	return nil
}

func (f *Fake) Append(_ context.Context, m domain.Movement) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, other := range f.movements {
		if other.ID == m.ID || (other.ProductID == m.ProductID && other.Seq == m.Seq) ||
			(m.ReversesID != "" && other.ReversesID == m.ReversesID) {
			return errs.Conflict("database.duplicate", "unique constraint")
		}
	}
	if m.OnHandAfterMicro != m.OnHandBeforeMicro+m.QuantityMicro || m.Seq < 1 {
		return errs.Validation("database.constraint_violation", "check constraint")
	}
	f.movements = append(f.movements, m)
	return nil
}

func (f *Fake) SaveLevel(_ context.Context, l domain.Level) (domain.Level, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	if f.failSaveLevelAt > 0 && f.saves == f.failSaveLevelAt {
		return domain.Level{}, ErrInjected
	}
	stored, exists := f.levels[l.ProductID]
	switch {
	case l.RowVersion == 0 && exists:
		return domain.Level{}, errs.Conflict("database.duplicate", "primary key")
	case l.RowVersion != 0 && (!exists || stored.RowVersion != l.RowVersion):
		return domain.Level{}, domain.ErrStale()
	}
	l.RowVersion++
	f.levels[l.ProductID] = l
	return l, nil
}

// Catalogue is a fake catalogue port.
type Catalogue struct {
	mu       sync.Mutex
	products map[id.ID]domain.Product
	packages map[id.ID]domain.Package
}

// NewCatalogue returns an empty catalogue.
func NewCatalogue() *Catalogue {
	return &Catalogue{products: map[id.ID]domain.Product{}, packages: map[id.ID]domain.Package{}}
}

// CodeProductNotFound mirrors the catalogue's code; the fake cannot import that module.
const CodeProductNotFound = "lite.catalog.not_found"

// Add registers a product and returns its id.
func (c *Catalogue) Add(t testing.TB, unitDecimals int, active bool) id.ID {
	t.Helper()
	productID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.products[productID] = domain.Product{ID: productID, UnitDecimals: unitDecimals, Active: active}
	return productID
}

// SetActive changes whether a product is active.
func (c *Catalogue) SetActive(productID id.ID, active bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.products[productID]
	p.Active = active
	c.products[productID] = p
}

// Link makes a package product open into content.
func (c *Catalogue) Link(packageID, contentID id.ID, contentQuantityMicro int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.packages[packageID] = domain.Package{ContentProductID: contentID, ContentQuantityMicro: contentQuantityMicro}
}

func (c *Catalogue) Product(_ context.Context, productID id.ID) (domain.Product, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.products[productID]
	if !ok {
		return domain.Product{}, errs.NotFound(CodeProductNotFound, "no such product")
	}
	return p, nil
}

func (c *Catalogue) Products(context.Context) ([]domain.Product, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]domain.Product, 0, len(c.products))
	for _, p := range c.products {
		out = append(out, p)
	}
	return out, nil
}

func (c *Catalogue) Package(_ context.Context, packageID id.ID) (domain.Package, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.packages[packageID]
	return p, ok, nil
}

func (c *Catalogue) Currencies(context.Context) ([]domain.Currency, error) {
	return append([]domain.Currency(nil), Currencies...), nil
}

// Gate is a recording owner gate. With Elevated false it refuses as the owner module would.
type Gate struct {
	mu       sync.Mutex
	Elevated bool
	Acts     []stock.GuardedAct
	Asked    int
	Viewed   int
}

// Require implements stock.OwnerGate.
func (g *Gate) Require(_ context.Context, act stock.GuardedAct) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Asked++
	if !g.Elevated {
		return errs.Permission(stock.CodeOwnerRequired, "the owner's PIN is required")
	}
	g.Acts = append(g.Acts, act)
	return nil
}

// Allowed implements stock.OwnerGate.
func (g *Gate) Allowed(context.Context) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Viewed++
	return g.Elevated
}
