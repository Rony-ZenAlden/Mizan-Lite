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

	// Package returns the link a package product has, or found=false.
	Package(ctx context.Context, packageProductID id.ID) (domain.Package, bool, error)
	// Packages returns every link.
	Packages(ctx context.Context) ([]domain.Package, error)
	// IsContent reports whether any package opens into productID.
	IsContent(ctx context.Context, productID id.ID) (bool, error)
	// SavePackage inserts a link whose RowVersion is 0, or updates one still at its RowVersion.
	SavePackage(ctx context.Context, p domain.Package) (domain.Package, error)
	// DeletePackage removes a package product's link, if it has one.
	DeletePackage(ctx context.Context, packageProductID id.ID) error
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

// AllProducts is a limit no catalogue reaches, for the reads that need every product (L2's stock levels).
const AllProducts = 1 << 30

// DefaultLimit bounds a search. A pantry shop's catalogue is hundreds of products, not tens of thousands.
const DefaultLimit = 500

// Service is the catalogue use cases.
type Service struct {
	tx    Transactor
	store Store
	gate  OwnerGate
	newID func() (id.ID, error)
	// rates is where the catalogue learns the rate in force when a price is set (2026-09-23). Wired after construction
	// (UseRates), so the module's fakes keep compiling; nil records no rate, which reads as "nothing to measure".
	rates PricingRate
}

// PricingRate is what the catalogue needs to know about money it does not own: the exchange rate in force, the local
// currency, and the smallest note a local price should land on.
type PricingRate interface {
	// InForce is the rate in force at 10⁻⁹, and the local currency's code; found=false before any rate exists.
	InForce(ctx context.Context) (nano int64, local string, found bool, err error)
	// CashNote is the smallest local note, in local minor units, that a re-priced local price is rounded to.
	CashNote(ctx context.Context) (int64, error)
}

// UseRates gives the catalogue the rate in force. The composition root calls it once.
func (s *Service) UseRates(r PricingRate) { s.rates = r }

// rateNow is the rate in force, or 0 where there is none or no rates were wired.
func (s *Service) rateNow(ctx context.Context) (int64, error) {
	if s.rates == nil {
		return 0, nil
	}
	nano, _, found, err := s.rates.InForce(ctx)
	if err != nil || !found {
		return 0, err
	}
	return nano, nil
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

// ByBarcode returns the product with a barcode as a scanner typed it — digits in any script (L1 H2) — or found=false.
func (s *Service) ByBarcode(ctx context.Context, raw string) (domain.Product, bool, error) {
	return s.store.ByBarcode(ctx, numinput.LatinDigits(raw))
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
		// The rate a price was set at, so the price can later be found stale (2026-09-23). An open-priced product has no
		// price to go stale.
		if !p.OpenPrice {
			if p.PricedRateNano, err = s.rateNow(ctx); err != nil {
				return err
			}
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
	// UnitsPerCarton is how many of the unit make one carton; empty clears it (0.10.0).
	UnitsPerCarton string
}

// Update changes a product's names, barcode and carton size: what describes it, not what it costs.
func (s *Service) Update(ctx context.Context, in UpdateInput) (domain.Product, error) {
	return s.change(ctx, in.ID, in.RowVersion, func(ctx context.Context, current domain.Product, ref domain.Reference) (domain.Product, error) {
		next, err := current.Rename(in.NameAR, in.NameEN)
		if err != nil {
			return current, err
		}
		if next, err = next.WithBarcode(in.Barcode); err != nil {
			return current, err
		}
		if next, err = next.WithCartonSize(in.UnitsPerCarton, ref); err != nil {
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
	// Cost is what the shop pays for one unit, in the same currency (L9). Empty leaves it as it is; "-" clears it.
	Cost string
	// CostDiscount is a supplier's discount, a percentage taken off the Cost typed with it (0.10.0).
	CostDiscount string
	// MarginPercent or MarginAmount works the price out of the cost instead of taking Price as typed. At most one, and
	// only when a cost is known. The form sends whichever box the shopkeeper typed in last.
	MarginPercent string
	MarginAmount  string
}

// ClearCost is what Cost carries to take a cost price off a product: an empty string means "leave it alone", so the
// intention to remove one needs a word of its own.
const ClearCost = "-"

// SetPrice changes a product's price. It requires the owner (Q-L1.1) — unless nothing changes, because a
// form that saves an unchanged price must not ask for a PIN.
func (s *Service) SetPrice(ctx context.Context, in SetPriceInput) (domain.Product, error) {
	return s.change(ctx, in.ID, in.RowVersion, func(ctx context.Context, current domain.Product, ref domain.Reference) (domain.Product, error) {
		// The currency first: the cost is held in the same one, so it must be settled before either is read (L9).
		next, err := current.Reprice(in.Currency, in.Price, ref)
		if err != nil {
			return current, err
		}
		if next, err = applyCost(next, in, ref); err != nil {
			return current, err
		}
		if current.SamePrice(next.PriceCurrency, next.PriceMicro) && current.HasCost == next.HasCost && current.CostMicro == next.CostMicro {
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

// applyCost puts the cost price on the product and, when the form asked for one, works the selling price out of a margin
// instead of the price typed. A margin and a typed price cannot both decide: the margin wins, because a shopkeeper who
// typed one was watching the other being computed in front of them.
func applyCost(p domain.Product, in SetPriceInput, ref domain.Reference) (domain.Product, error) {
	var err error
	switch in.Cost {
	case "":
		// left as it is
	case ClearCost:
		if p, err = p.SetCost("", ref); err != nil {
			return p, err
		}
	default:
		if p, err = p.SetCost(in.Cost, ref); err != nil {
			return p, err
		}
	}
	// A supplier's discount comes off the cost typed with it — never off one stored, which may already carry it (0.10.0).
	if in.CostDiscount != "" {
		if in.Cost == "" || in.Cost == ClearCost {
			return p, errs.Validation(domain.CodeCostDiscountInvalid, "a discount comes off a cost price typed with it").
				WithField(domain.FieldCostDiscount, domain.CodeCostDiscountInvalid, "invalid")
		}
		if p, err = p.DiscountCost(in.CostDiscount, ref); err != nil {
			return p, err
		}
	}
	switch {
	case in.MarginPercent != "" && in.MarginAmount != "":
		return p, errs.Validation(domain.CodeMarginInvalid, "a margin is a percentage or an amount, not both").
			WithField(domain.FieldMargin, domain.CodeMarginInvalid, "one or the other")
	case in.MarginPercent != "":
		return p.PriceFromMarginPercent(in.MarginPercent, ref)
	case in.MarginAmount != "":
		return p.PriceFromMarginAmount(in.MarginAmount, ref)
	}
	return p, nil
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

// All returns every product, active or not.
func (s *Service) All(ctx context.Context) ([]domain.Product, error) {
	return s.store.Search(ctx, Query{IncludeInactive: true, Limit: AllProducts})
}

// Package returns a package product's link, or found=false.
func (s *Service) Package(ctx context.Context, packageProductID id.ID) (domain.Package, bool, error) {
	return s.store.Package(ctx, packageProductID)
}

// Packages returns every link.
func (s *Service) Packages(ctx context.Context) ([]domain.Package, error) {
	return s.store.Packages(ctx)
}

// SetPackageInput links a package product to what it opens into.
type SetPackageInput struct {
	PackageProductID id.ID
	ContentProductID id.ID
	ContentQuantity  string
}

// SetPackage links or relinks a package product (L2 §3.5). One level only (Q-L2.8): the content may not itself open,
// and the package may not be another package's content.
func (s *Service) SetPackage(ctx context.Context, in SetPackageInput) (domain.Package, error) {
	var out domain.Package
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		pkg, err := s.store.Get(ctx, in.PackageProductID)
		if err != nil {
			return err
		}
		content, err := s.store.Get(ctx, in.ContentProductID)
		if err != nil {
			return withPackageField(err)
		}
		ref, err := s.Reference(ctx)
		if err != nil {
			return err
		}
		link, err := domain.NewPackage(pkg, content, in.ContentQuantity, ref)
		if err != nil {
			return err
		}
		_, contentOpens, err := s.store.Package(ctx, content.ID)
		if err != nil || contentOpens {
			return nestedOr(err)
		}
		isContent, err := s.store.IsContent(ctx, pkg.ID)
		if err != nil || isContent {
			return nestedOr(err)
		}
		existing, found, err := s.store.Package(ctx, pkg.ID)
		if err != nil {
			return err
		}
		if found {
			link.RowVersion = existing.RowVersion
			if link == existing {
				out = existing
				return nil
			}
		}
		out, err = s.store.SavePackage(ctx, link)
		return err
	})
	return out, err
}

// ClearPackage removes a package product's link. A product with no link is left as it is.
func (s *Service) ClearPackage(ctx context.Context, packageProductID id.ID) error {
	return s.tx.Do(ctx, func(ctx context.Context) error {
		if _, err := s.store.Get(ctx, packageProductID); err != nil {
			return err
		}
		return s.store.DeletePackage(ctx, packageProductID)
	})
}

func nestedOr(err error) error {
	if err != nil {
		return err
	}
	return errs.Conflict(domain.CodePackageNested, "packages open one level only").
		WithField(domain.FieldPackageContent, domain.CodePackageNested, "nested")
}

func withPackageField(err error) error {
	if typed, ok := errs.AsError(err); ok {
		return typed.WithField(domain.FieldPackageContent, typed.Code, "invalid")
	}
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
		// Every route that moves a price — the form, a margin, a confirmed re-price — passes through here, so the rate
		// the price was set at is taken here and nowhere else.
		if !next.OpenPrice && (next.PriceMicro != current.PriceMicro || next.PriceCurrency != current.PriceCurrency) {
			if next.PricedRateNano, err = s.rateNow(ctx); err != nil {
				return err
			}
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

// SetReorderInput is the level at or below which a product is low, typed in its own unit. "" clears it.
type SetReorderInput struct {
	ID         id.ID
	RowVersion int64
	Level      string
}

// SetReorder records the reorder level (2026-09-20).
//
// Unguarded. It changes no price, no quantity and no money — it changes when a badge appears. Putting it behind the
// owner's PIN would mean a shopkeeper who noticed they keep running out of bread has to fetch the owner to say so.
func (s *Service) SetReorder(ctx context.Context, in SetReorderInput) (domain.Product, error) {
	return s.change(ctx, in.ID, in.RowVersion, func(_ context.Context, current domain.Product, ref domain.Reference) (domain.Product, error) {
		return current.SetReorder(in.Level, ref)
	})
}
