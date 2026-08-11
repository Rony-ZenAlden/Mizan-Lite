// Package domain holds the pricing module's business rules.
//
// Resolution above all: which list answers, at which granularity, for which quantity — and the
// recorded REASON, because a price nobody can explain is a price nobody trusts.
package domain

import (
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable error codes. They double as i18n keys and are part of the public contract.
const (
	CodeInvalidList   = "pricing.invalid_list"
	CodeInvalidItem   = "pricing.invalid_item"
	CodeNoPrice       = "pricing.no_price"
	CodeNoDefaultList = "pricing.no_default_list"
	CodeNegativePrice = "pricing.negative_price"
)

// Direction separates what a business charges from what it pays.
//
// One concept, not two: a supplier's agreed rates and a customer's wholesale list are both price
// lists, and separating them by type would duplicate every column and both resolution functions.
type Direction string

// The directions.
const (
	Sale     Direction = "sale"
	Purchase Direction = "purchase"
)

// List is a set of prices.
type List struct {
	ID           id.ID
	Code         string
	Name         string
	NameKey      string
	CurrencyCode string
	IsDefault    bool
	Direction    Direction
	ValidFrom    string
	ValidTo      string
	IsActive     bool
}

// InForce reports whether the list applies on a date.
//
// An empty bound is open, so a list with no dates is always in force — which is what a shop with
// one price list wants and never has to think about.
func (l List) InForce(date string) bool {
	if !l.IsActive {
		return false
	}
	if l.ValidFrom != "" && date < l.ValidFrom {
		return false
	}
	if l.ValidTo != "" && date > l.ValidTo {
		return false
	}
	return true
}

// Item is one price in a list.
//
// It targets EITHER a product or a variant. A product-level item prices every variant of it; a
// variant-level item overrides that for one. "XXL costs more but every colour costs the same" is
// then two rows rather than twenty.
type Item struct {
	ID          id.ID
	ListID      id.ID
	ProductID   id.ID
	VariantID   id.ID
	PriceMinor  int64
	MinQuantity int64
	IsActive    bool
}

// NewItem builds a price, or refuses.
func NewItem(
	identifier, listID, productID, variantID id.ID, priceMinor, minQuantity int64,
) (Item, error) {
	if identifier.IsZero() || listID.IsZero() {
		return Item{}, errs.Validation(CodeInvalidItem, "a price needs an identity and a list")
	}
	// Exactly one target. Neither prices nothing; both would have two answers and no rule to
	// choose between them.
	if productID.IsZero() == variantID.IsZero() {
		return Item{}, errs.Validation(CodeInvalidItem,
			"a price applies to either a product or one of its variants, not both and not neither")
	}
	if priceMinor < 0 {
		// A negative price would pay the customer to take the goods, and every total derived
		// from it would be wrong in a direction no report flags.
		return Item{}, errs.Validation(CodeNegativePrice, "a price cannot be negative")
	}
	if minQuantity < 0 {
		return Item{}, errs.Validation(CodeInvalidItem, "a quantity break cannot be negative")
	}

	return Item{
		ID: identifier, ListID: listID, ProductID: productID, VariantID: variantID,
		PriceMinor: priceMinor, MinQuantity: minQuantity, IsActive: true,
	}, nil
}

// Source names which rule produced a price.
//
// Recorded and returned, not merely used. §19.3 established the shape for tax and §2.6 asks for
// the same here: "records which list answered". A price a salesperson cannot explain to the
// customer in front of them is a price they will override by hand, and every hand override is a
// discount nobody approved.
type Source string

// The sources, in the order resolution tries them.
const (
	SourcePartnerVariant Source = "partner_list_variant"
	SourcePartnerProduct Source = "partner_list_product"
	SourceBranchVariant  Source = "branch_list_variant"
	SourceBranchProduct  Source = "branch_list_product"
	SourceDefaultVariant Source = "default_list_variant"
	SourceDefaultProduct Source = "default_list_product"
)

// Candidate is one list resolution may consider, with its prices already loaded.
//
// The prices travel WITH the candidate so that Resolve stays pure — no database, no clock —
// and can be tested against a table rather than a fixture. That is the same reason the units
// domain takes units rather than a repository (3.1).
type Candidate struct {
	List   List
	Source Source
	// ProductSource is the source to record when the answer comes from a product-level item
	// rather than a variant-level one, so one candidate covers both granularities of one list.
	ProductSource Source

	VariantItems []Item
	ProductItems []Item
}

// Resolved is a price and the reason for it.
type Resolved struct {
	PriceMinor   int64
	CurrencyCode string
	ListID       id.ID
	ListCode     string
	Source       Source
	// MinQuantity is the break that answered, so a screen can say "10 or more" rather than
	// leaving a salesperson to wonder why the price changed.
	MinQuantity int64
}

// Resolve picks the price for a variant.
//
// # The order
//
//  1. the partner's assigned list
//  2. the branch's list
//  3. the company default list
//
// and within each, a variant-level price before a product-level one. The five levels §2.6 listed
// are these three lists times two granularities: the same expressive power through one mechanism
// rather than two, and — decisively — no price column on a table another module owns.
//
// # Quantity breaks
//
// Within a list, the HIGHEST break the line qualifies for wins. Ten units at the "10 or more"
// price is what a customer expects; taking the first matching row instead would charge them the
// base price and lose the sale the discount was created to win.
//
// # No price is an ERROR
//
// Not zero. A variant nobody has priced is a configuration gap, and returning zero would sell it
// for nothing — silently, on a receipt that looks perfectly ordinary.
func Resolve(
	candidates []Candidate, variantID id.ID, quantityMicro int64, date string,
) (Resolved, error) {
	for _, candidate := range candidates {
		if !candidate.List.InForce(date) {
			continue
		}
		// A variant-level price beats a product-level one WITHIN the same list, and both beat
		// anything in a lower-priority list — which is why the search is per candidate rather
		// than across every item at once.
		if answer, found := best(
			candidate, candidate.VariantItems, quantityMicro, candidate.Source); found {
			return answer, nil
		}
		if answer, found := best(
			candidate, candidate.ProductItems, quantityMicro, candidate.ProductSource); found {
			return answer, nil
		}
	}

	return Resolved{}, errs.NotFound(CodeNoPrice,
		"no price list gives this item a price").WithParam("variant", string(variantID))
}

// best picks the highest quantity break the line qualifies for.
func best(c Candidate, list []Item, quantityMicro int64, source Source) (Resolved, bool) {
	var (
		chosen Item
		found  bool
	)
	for _, item := range list {
		if !item.IsActive || item.MinQuantity > quantityMicro {
			continue
		}
		// Highest break wins. Two items cannot share a break — the unique index forbids it — so
		// there is no tie to resolve arbitrarily.
		if !found || item.MinQuantity > chosen.MinQuantity {
			chosen, found = item, true
		}
	}
	if !found {
		return Resolved{}, false
	}
	return Resolved{
		PriceMinor: chosen.PriceMinor, CurrencyCode: c.List.CurrencyCode,
		ListID: c.List.ID, ListCode: c.List.Code, Source: source,
		MinQuantity: chosen.MinQuantity,
	}, true
}
