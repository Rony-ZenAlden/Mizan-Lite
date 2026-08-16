// Package search is the one genuinely cross-module mechanism Phase 8 adds.
//
// # Why a registry and not a package that knows every module
//
// Everything else in Phase 8 is answered by the module that owns the data. Search cannot be: one
// box must find a product, a partner, an invoice and a bill, and no module can see the other
// three.
//
// The alternatives were a package importing every module — which `module-isolation` forbids, and
// which would be the single file that breaks whenever any module changes a column — or a search
// index maintained by events, which is a projection with all the obligations Phase 8's analysis
// refused: its own rebuild, its own verifier, its own drift report.
//
// So each module contributes a SEARCHER and the composition root collects them, which is the
// shape the module contract already uses for jobs and permissions. A module that becomes
// searchable adds one method; nothing here changes.
package search

import (
	"context"
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable codes.
const (
	CodeQueryTooShort = "search.query_too_short"
)

// MinQuery is the shortest query that will be run.
//
// One character matches most of a catalogue, and a screen that fires on every keystroke would ask
// for it. Refusing is better than returning ten thousand rows nobody reads.
const MinQuery = 2

// Result is one thing found.
//
// # Deliberately not a typed union
//
// A caller navigates by KIND — "this is a partner, open the partner screen" — and a union would
// need extending in three places every time a module became searchable. That is the fourth place
// to forget, and this codebase has paid for three of those already.
type Result struct {
	// Kind is what was found, as the module names it: "catalog.product", "partner.partner",
	// "sales.invoice". The prefix is the module, so a caller can route without a lookup table.
	Kind string
	// ID is what to open.
	ID id.ID
	// Label is the primary line: a name, a document number.
	Label string
	// Subtitle is the disambiguating line: an SKU, a partner name, a date and amount. A shop with
	// four customers called Mohammed needs it, and a result list without it is unusable.
	Subtitle string

	// Rank orders results WITHIN a kind, lower first. A searcher sets it from how well the row
	// matched — an exact code beats a name that merely contains the query.
	Rank int
}

// Searcher is what a module contributes.
//
// One method, taking the raw query. Deliberately NOT a "search these tables with these columns"
// description: a module knows how its own data should be matched, and a generic matcher would
// have to be told about SKUs, barcodes, and document numbers by the modules that own them —
// which is the coupling this design exists to avoid.
type Searcher interface {
	// Name identifies the contributor, for the coverage test and for a caller that wants to
	// report which sources answered.
	Name() string
	// Search returns what this module can find, already limited to `limit` rows.
	Search(ctx context.Context, companyID id.ID, query string, limit int) ([]Result, error)
}

// Registry fans one query out to every searcher.
type Registry struct {
	searchers []Searcher
}

// New builds a registry over the searchers given.
func New(searchers ...Searcher) *Registry {
	return &Registry{searchers: searchers}
}

// Names lists the contributors, in registration order.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.searchers))
	for _, searcher := range r.searchers {
		out = append(out, searcher.Name())
	}
	return out
}

// Response is what a search returns, with what failed alongside what was found.
type Response struct {
	Query   string
	Results []Result

	// Failed names the searchers that returned an error.
	//
	// # Why a partial answer rather than no answer
	//
	// A search that fails entirely because one module's query is broken is a search box that
	// stops working for reasons the user cannot see or fix. Returning what the other modules
	// found, and SAYING which one did not answer, is the honest shape — and it is 7.4 D3's rule
	// again: report rather than refuse, so the evidence survives.
	Failed []string
}

// Search asks every searcher and merges the answers.
//
// `perSearcher` bounds each contributor rather than the total. A shop searching "AH" would
// otherwise get two hundred products and no partners, purely because the catalogue answered
// first — which is the wrong answer to "find me Ahmad".
func (r *Registry) Search(
	ctx context.Context, companyID id.ID, query string, perSearcher int,
) (Response, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < MinQuery {
		return Response{}, errs.Validation(CodeQueryTooShort,
			"a search needs at least two characters")
	}
	if perSearcher <= 0 {
		perSearcher = 10
	}

	response := Response{Query: query, Results: make([]Result, 0, len(r.searchers)*perSearcher)}
	for _, searcher := range r.searchers {
		found, err := searcher.Search(ctx, companyID, query, perSearcher)
		if err != nil {
			response.Failed = append(response.Failed, searcher.Name())
			continue
		}
		response.Results = append(response.Results, found...)
	}

	// Grouped by kind, ranked within it, then alphabetical. NOT one global relevance score:
	// comparing "how well does this product match" with "how well does this invoice match" needs
	// a scale neither module knows about, and inventing one would make the order look
	// meaningful when it is arbitrary.
	sort.SliceStable(response.Results, func(i, j int) bool {
		left, right := response.Results[i], response.Results[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Rank != right.Rank {
			return left.Rank < right.Rank
		}
		return left.Label < right.Label
	})
	return response, nil
}

// Like turns a query into a LIKE pattern with its wildcards escaped.
//
// # Why this is here and not in each searcher
//
// A query containing `%` would otherwise match everything, and one containing `_` would match any
// character — so a customer searching for a product called "50%" gets the whole catalogue. Every
// searcher needs the same escaping, and the fourth one to be written is the one that forgets.
//
// The escape character is `\`, which SQLite honours ONLY when the query says `ESCAPE '\'`. The
// two are a pair: a searcher that uses this function and omits the clause has escaping that does
// nothing, and `TestEverySearcherEscapesItsWildcards` is what catches that.
func Like(query string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(strings.ToLower(query)) + "%"
}
