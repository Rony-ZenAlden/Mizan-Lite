package bootstrap_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/modules/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/modules/catalog/domain"
	"github.com/mizan-erp/mizan/internal/modules/partner"
)

// TestOneQueryFindsThingsFromMoreThanOneModule
//
// # DoD criterion 6, and it can only be tested here
//
// The registry's own tests use stubs, which prove the fan-out, the bounding and the ordering and
// say nothing about whether any module's query works — or whether the composition root registered
// it. Both are the failure this is written against: a searcher that compiles, is never
// registered, and silently contributes nothing.
//
// **A seam is only proven by a caller** (Phase 6), and 7.6 found the same shape in the worst
// possible form — thirteen number series nothing ever created, because every test made its own.
func TestOneQueryFindsThingsFromMoreThanOneModule(t *testing.T) {
	app, ctx, companyID := traded(t)

	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	if _, _, err := app.Catalog.CreateProduct(ctx, catalog.NewProductInput{
		CompanyID: companyID, Code: "AHM-1", Name: "Ahmadi olive oil",
		Type: catalogdomain.TypeGoods, StockUnit: "PCS",
	}); err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if _, err := app.Partner.CreatePartner(ctx, partner.NewPartnerInput{
		CompanyID: companyID, Code: "CUST-1", Name: "Ahmad Trading",
		IsCustomer: true, Phone: "0555123456",
	}); err != nil {
		t.Fatalf("CreatePartner: %v", err)
	}

	response, err := app.Search.Search(ctx, companyID, "ahma", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Failed) != 0 {
		t.Errorf("searchers failed: %v", response.Failed)
	}

	kinds := make(map[string]string, 4)
	for _, result := range response.Results {
		kinds[result.Kind] = result.Label
	}
	if kinds["catalog.product"] != "Ahmadi olive oil" {
		t.Errorf("the product was not found: %v", kinds)
	}
	if kinds["partner.partner"] != "Ahmad Trading" {
		t.Errorf("the partner was not found: %v", kinds)
	}

	// Every registered searcher answered. A searcher that compiles, is never registered, and
	// contributes nothing is invisible to every other test in this file.
	registered := app.Search.Names()
	if len(registered) < 4 {
		t.Errorf("%d searchers registered (%v), want the four modules that have something "+
			"findable", len(registered), registered)
	}

	// Phone, not just name: it is how a shop identifies a returning customer, who gives a number
	// rather than a code.
	byPhone, err := app.Search.Search(ctx, companyID, "5551", 10)
	if err != nil {
		t.Fatalf("Search by phone: %v", err)
	}
	var foundByPhone bool
	for _, result := range byPhone.Results {
		if result.Kind == "partner.partner" {
			foundByPhone = true
			if !strings.Contains(result.Subtitle, "0555123456") {
				t.Errorf("subtitle = %q, and a shop with four customers called Mohammed needs "+
					"the phone number in it", result.Subtitle)
			}
		}
	}
	if !foundByPhone {
		t.Error("a customer could not be found by their phone number")
	}
}

// TestEverySearcherEscapesItsWildcards
//
// `search.Like` escapes `%` and `_`, and SQLite honours the escape ONLY when the query says
// `ESCAPE '\'`. The two are a pair, and a searcher that uses the function and omits the clause has
// escaping that does nothing — which is invisible until somebody searches for a product called
// "50%" and gets the whole catalogue.
//
// A percent sign is a real product name. An underscore is a real SKU.
func TestEverySearcherEscapesItsWildcards(t *testing.T) {
	app, ctx, companyID := traded(t)

	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	for _, product := range []struct{ code, name string }{
		{"DISC", "50% off bundle"},
		{"PLAIN", "Ordinary widget"},
	} {
		if _, _, err := app.Catalog.CreateProduct(ctx, catalog.NewProductInput{
			CompanyID: companyID, Code: product.code, Name: product.name,
			Type: catalogdomain.TypeGoods, StockUnit: "PCS",
		}); err != nil {
			t.Fatalf("CreateProduct(%s): %v", product.code, err)
		}
	}

	// "%" unescaped is "match everything". Escaped, it matches the one product whose name
	// contains a percent sign.
	response, err := app.Search.Search(ctx, companyID, "0%", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, result := range response.Results {
		if result.Kind == "catalog.product" && result.Label == "Ordinary widget" {
			t.Fatalf("searching for %q matched %q — the wildcard was not escaped, or the "+
				"ESCAPE clause is missing from the query", "0%", result.Label)
		}
	}
	var foundTheBundle bool
	for _, result := range response.Results {
		if result.Label == "50% off bundle" {
			foundTheBundle = true
		}
	}
	if !foundTheBundle {
		t.Error("a product whose name contains a percent sign cannot be found by it")
	}
}

// TestEverySearcherHonoursItsOwnEscape
//
// # The gap the 8.5 write-up left, closed
//
// `TestEverySearcherEscapesItsWildcards` exercises the CATALOGUE query and nothing else. The
// partner, sales and purchasing queries use the same `ESCAPE '\'` clause and had no test that
// would catch one of them losing it — which is the "proven about one of three documents is proven
// about none" finding from 7.6, in a fourth place.
//
// Rather than four near-identical data fixtures, this reads the SOURCE: every `LIKE ?` in a
// searcher's query must be followed by its escape clause. A blunt check, and blunt is what makes
// it survive somebody adding a fifth searcher.
func TestEverySearcherHonoursItsOwnEscape(t *testing.T) {
	searchQueries := map[string]string{
		"catalog":    "../modules/catalog/infra/sqlite/search.go",
		"partner":    "../modules/partner/infra/sqlite/search.go",
		"sales":      "../modules/sales/infra/sqlite/search.go",
		"purchasing": "../modules/purchasing/infra/sqlite/search.go",
	}

	// `LIKE ?` optionally followed by whitespace and the escape clause. The negative lookahead
	// Go's regexp does not have is expressed by matching the whole thing and counting.
	likes := regexp.MustCompile(`LIKE \?`)
	escaped := regexp.MustCompile(`LIKE \? ESCAPE '\\'`)

	for module, path := range searchQueries {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s contributes a searcher and its query file is missing: %v", module, err)
			continue
		}
		total := len(likes.FindAll(source, -1))
		withEscape := len(escaped.FindAll(source, -1))

		if total == 0 {
			t.Errorf("%s's search query has no LIKE at all, so this check is looking at the "+
				"wrong file", module)
			continue
		}
		if withEscape != total {
			t.Errorf("%s: %d of %d LIKE clauses carry ESCAPE — the ones that do not will match "+
				"a query containing %% against everything", module, withEscape, total)
		}
	}
}

// TestASearchOverAnEmptyCompanyFindsNothingAndDoesNotFail
//
// DoD criterion 11.
func TestASearchOverAnEmptyCompanyFindsNothingAndDoesNotFail(t *testing.T) {
	app, ctx, companyID := traded(t)

	response, err := app.Search.Search(ctx, companyID, "anything", 10)
	if err != nil {
		t.Fatalf("Search over an empty company: %v", err)
	}
	if len(response.Results) != 0 {
		t.Errorf("%d results in an empty company: %+v", len(response.Results),
			response.Results)
	}
	if len(response.Failed) != 0 {
		t.Errorf("searchers failed on an empty company: %v", response.Failed)
	}
}
