// Package catalog is Mizan Lite's product catalogue: units, currencies, products, and the till's quick grid.
package catalog

import (
	"context"
	"strconv"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/numinput"
	"github.com/mizan-erp/mizan/internal/lite/textkey"
)

// The acts only the owner may do (Q-L1.1). Their codes are what the owner's history records.
const (
	ActPriceChange = "catalog.price.change"
	ActDeactivate  = "catalog.product.deactivate"
)

// Store persists the catalogue. infra/sqlite implements it; catalogtest.Fake implements it in memory, and
// one contract suite runs against both.
type Store interface {
	Units(ctx context.Context) ([]domain.Unit, error)
	Currencies(ctx context.Context) ([]domain.Currency, error)
	// Get returns the product, or domain.ErrNotFound.
	Get(ctx context.Context, productID id.ID) (domain.Product, error)
	// ByNameKey, ByBarcode and BySlot return the product holding that key, or found=false.
	ByNameKey(ctx context.Context, key string) (domain.Product, bool, error)
	ByBarcode(ctx context.Context, barcode string) (domain.Product, bool, error)
	BySlot(ctx context.Context, slot int) (domain.Product, bool, error)
	Search(ctx context.Context, q Query) ([]domain.Product, error)
	Insert(ctx context.Context, p domain.Product) error
	// Update writes p if the stored row is still at p.RowVersion, and increments the version.
	// A row that moved on is refused with domain.ErrStale.
	Update(ctx context.Context, p domain.Product) (domain.Product, error)
}

// Transactor runs fn atomically. platform/database.Store satisfies it.
type Transactor interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// OwnerGate is what the catalogue needs from the owner: permission for an act only the owner may do.
//
// Declared here, by the module that needs it, and satisfied in the composition root — so catalog never
// imports the owner module (L1 §7.6, D-L1.9).
type OwnerGate interface {
	// Require returns nil when the application is in owner mode, and records the act IN the caller's
	// transaction — an act that rolls back leaves no record that it happened. Otherwise it returns a
	// Permission error the frontend answers with the PIN dialog.
	Require(ctx context.Context, act GuardedAct) error
}

// GuardedAct describes an owner-only act, for the owner's history. Never a credential.
type GuardedAct struct {
	Action    string
	SubjectID id.ID
	Before    string
	After     string
}

// Query is a product search.
type Query struct {
	// Text is matched, normalised, against names and barcode; empty lists everything.
	Text string
	// Barcode is matched exactly and ranked first. Set by the service from Text.
	Barcode         string
	IncludeInactive bool
	Limit           int
}

// DefaultLimit bounds a search. A pantry shop's catalogue is hundreds of products, not tens of thousands.
const DefaultLimit = 500

// Service is the catalogue use cases.
type Service struct {
	tx    Transactor
	store Store
	gate  OwnerGate
	newID func() (id.ID, error)
}

// NewService builds the service. Every dependency is required.
func NewService(tx Transactor, store Store, gate OwnerGate) *Service {
	return &Service{tx: tx, store: store, gate: gate, newID: id.New}
}

// Units lists the seeded units in display order.
func (s *Service) Units(ctx context.Context) ([]domain.Unit, error) { return s.store.Units(ctx) }

// Currencies lists the seeded currencies in display order.
func (s *Service) Currencies(ctx context.Context) ([]domain.Currency, error) {
	return s.store.Currencies(ctx)
}

// Reference loads what products are validated against.
func (s *Service) Reference(ctx context.Context) (domain.Reference, error) {
	units, err := s.store.Units(ctx)
	if err != nil {
		return domain.Reference{}, err
	}
	currencies, err := s.store.Currencies(ctx)
	if err != nil {
		return domain.Reference{}, err
	}
	ref := domain.Reference{Units: map[string]domain.Unit{}, Currencies: map[string]domain.Currency{}}
	for _, u := range units {
		ref.Units[u.Code] = u
	}
	for _, c := range currencies {
		ref.Currencies[c.Code] = c
	}
	return ref, nil
}

// Search finds products by name or barcode, in either spelling a reader would use (L1 §5).
func (s *Service) Search(ctx context.Context, text string, includeInactive bool) ([]domain.Product, error) {
	return s.store.Search(ctx, Query{
		Text:            textkey.Normalise(text),
		Barcode:         numinput.LatinDigits(text),
		IncludeInactive: includeInactive,
		Limit:           DefaultLimit,
	})
}

// Get returns one product.
func (s *Service) Get(ctx context.Context, productID id.ID) (domain.Product, error) {
	return s.store.Get(ctx, productID)
}

// Create adds a product.
func (s *Service) Create(ctx context.Context, d domain.Draft) (domain.Product, error) {
	var out domain.Product
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		ref, err := s.Reference(ctx)
		if err != nil {
			return err
		}
		productID, err := s.newID()
		if err != nil {
			return err
		}
		p, err := domain.NewProduct(productID, d, ref)
		if err != nil {
			return err
		}
		if err := s.checkUnique(ctx, p); err != nil {
			return err
		}
		if err := s.store.Insert(ctx, p); err != nil {
			return err
		}
		out = p
		return nil
	})
	return out, err
}

// UpdateInput changes the names and barcode. The unit is not editable in L1: from L2 a unit cannot change
// once stock has moved in it, and a method written before that rule exists would have to be taken away.
type UpdateInput struct {
	ID         id.ID
	RowVersion int64
	NameAR     string
	NameEN     string
	Barcode    string
}

// Update changes a product's names and barcode.
func (s *Service) Update(ctx context.Context, in UpdateInput) (domain.Product, error) {
	return s.change(ctx, in.ID, in.RowVersion, func(ctx context.Context, current domain.Product, _ domain.Reference) (domain.Product, error) {
		next, err := current.Rename(in.NameAR, in.NameEN)
		if err != nil {
			return current, err
		}
		if next, err = next.WithBarcode(in.Barcode); err != nil {
			return current, err
		}
		return next, s.checkUnique(ctx, next)
	})
}

// SetPriceInput changes a price and its currency.
type SetPriceInput struct {
	ID         id.ID
	RowVersion int64
	Currency   string
	Price      string
}

// SetPrice changes a product's price. It requires the owner (Q-L1.1) — unless nothing changes, because a
// form that saves an unchanged price must not ask for a PIN.
func (s *Service) SetPrice(ctx context.Context, in SetPriceInput) (domain.Product, error) {
	return s.change(ctx, in.ID, in.RowVersion, func(ctx context.Context, current domain.Product, ref domain.Reference) (domain.Product, error) {
		next, err := current.Reprice(in.Currency, in.Price, ref)
		if err != nil {
			return current, err
		}
		if current.SamePrice(next.PriceCurrency, next.PriceMicro) {
			return current, nil
		}
		return next, s.gate.Require(ctx, GuardedAct{
			Action:    ActPriceChange,
			SubjectID: current.ID,
			Before:    current.PriceCurrency + " " + current.PriceText(ref),
			After:     next.PriceCurrency + " " + next.PriceText(ref),
		})
	})
}

// SetActiveInput takes a product out of sale or returns it.
type SetActiveInput struct {
	ID         id.ID
	RowVersion int64
	Active     bool
}

// SetActive deactivates or reactivates a product. Deactivation requires the owner (Q-L1.1): taking a product
// out of sale is how it stops being sellable, and there is no delete. Reactivation does not.
func (s *Service) SetActive(ctx context.Context, in SetActiveInput) (domain.Product, error) {
	return s.change(ctx, in.ID, in.RowVersion, func(ctx context.Context, current domain.Product, _ domain.Reference) (domain.Product, error) {
		switch {
		case in.Active == current.Active:
			return current, nil
		case in.Active:
			return current.Activate(), nil
		default:
			return current.Deactivate(), s.gate.Require(ctx, GuardedAct{
				Action: ActDeactivate, SubjectID: current.ID, Before: "active", After: "inactive",
			})
		}
	})
}

// SetQuickSlot puts a product on a till button, or takes it off with slot 0. A product already on that
// button is taken off it in the same transaction, so two products never claim one button.
func (s *Service) SetQuickSlot(ctx context.Context, productID id.ID, slot int) (domain.Product, error) {
	var out domain.Product
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		current, err := s.store.Get(ctx, productID)
		if err != nil {
			return err
		}
		next, err := current.WithSlot(slot)
		if err != nil {
			return err
		}
		if next.QuickSlot == current.QuickSlot {
			out = current
			return nil
		}
		if slot > 0 {
			// The holder is written first: the UNIQUE constraint on the slot would refuse the product taking
			// a button its holder still has.
			if err = s.unpinHolder(ctx, slot, current.ID); err != nil {
				return err
			}
		}
		out, err = s.store.Update(ctx, next)
		return err
	})
	return out, err
}

// unpinHolder takes whatever product other than productID holds slot off it.
func (s *Service) unpinHolder(ctx context.Context, slot int, productID id.ID) error {
	holder, held, err := s.store.BySlot(ctx, slot)
	if err != nil || !held || holder.ID == productID {
		return err
	}
	unpinned, err := holder.WithSlot(0)
	if err != nil {
		return err
	}
	_, err = s.store.Update(ctx, unpinned)
	return err
}

// change is the shape every versioned edit shares: load, refuse a stale version, apply, write — in one
// transaction, so a guard that records an act and a write that then fails leave nothing behind.
func (s *Service) change(
	ctx context.Context, productID id.ID, rowVersion int64,
	apply func(ctx context.Context, current domain.Product, ref domain.Reference) (domain.Product, error),
) (domain.Product, error) {
	var out domain.Product
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		current, err := s.store.Get(ctx, productID)
		if err != nil {
			return err
		}
		// Refused before anything is asked of the owner: a PIN typed for an edit that cannot be saved is
		// a PIN typed for nothing.
		if current.RowVersion != rowVersion {
			return domain.ErrStale()
		}
		ref, err := s.Reference(ctx)
		if err != nil {
			return err
		}
		next, err := apply(ctx, current, ref)
		if err != nil {
			return err
		}
		if next == current {
			out = current
			return nil
		}
		out, err = s.store.Update(ctx, next)
		return err
	})
	return out, err
}

// checkUnique refuses a name another product already has under any spelling, and a barcode another product
// already has. The UNIQUE constraints are the backstop; this is what gives a person a message that names
// the problem, and offers reactivation when the other product is inactive.
func (s *Service) checkUnique(ctx context.Context, p domain.Product) error {
	existing, found, err := s.store.ByNameKey(ctx, p.NameKey())
	if err != nil {
		return err
	}
	if found && existing.ID != p.ID {
		return errs.Conflict(domain.CodeDuplicateName, "a product with this name exists").
			WithField(domain.FieldNameAR, domain.CodeDuplicateName, "duplicate").
			WithParam("existingId", existing.ID.String()).
			WithParam("existingName", existing.NameAR).
			WithParam("existingActive", strconv.FormatBool(existing.Active))
	}
	if p.Barcode == "" {
		return nil
	}
	existing, found, err = s.store.ByBarcode(ctx, p.Barcode)
	if err != nil {
		return err
	}
	if found && existing.ID != p.ID {
		return errs.Conflict(domain.CodeDuplicateBarcode, "another product has this barcode").
			WithField(domain.FieldBarcode, domain.CodeDuplicateBarcode, "duplicate").
			WithParam("existingId", existing.ID.String()).
			WithParam("existingName", existing.NameAR)
	}
	return nil
}
