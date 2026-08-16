package catalog

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/search"
)

// searcherName is how this contributor identifies itself.
const searcherName = "catalog"

// KindProduct is what a catalogue hit is. The prefix is the module, so a caller routes on it
// without a lookup table.
const KindProduct = "catalog.product"

// Searcher lets the composition root add products to the global search.
//
// A method on the service rather than a separate type: the searcher needs exactly what the
// service already has, and a second struct holding the same repos would be a second place for
// the company filter to be forgotten.
func (s *Service) Searcher() search.Searcher { return productSearcher{svc: s} }

type productSearcher struct{ svc *Service }

func (p productSearcher) Name() string { return searcherName }

// Search finds products by code, name, or SKU.
//
// # Why SKU is included and why it ranks first
//
// A shopkeeper with a barcode scanner searches by SKU, and the SKU is exact — so a row matching
// it is what they meant, while a row whose NAME contains the same characters is a coincidence.
// Ranking is what stops the coincidence appearing above the answer.
func (p productSearcher) Search(
	ctx context.Context, companyID id.ID, query string, limit int,
) ([]search.Result, error) {
	return p.svc.repos.SearchProducts(ctx, companyID, query, limit)
}
