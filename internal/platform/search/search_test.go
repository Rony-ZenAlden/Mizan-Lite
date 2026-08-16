package search_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/search"
)

type stub struct {
	name    string
	results []search.Result
	err     error
	// sawLimit records what the registry passed down, so the per-searcher bound can be asserted
	// rather than assumed.
	sawLimit int
}

func (s *stub) Name() string { return s.name }

func (s *stub) Search(
	_ context.Context, _ id.ID, _ string, limit int,
) ([]search.Result, error) {
	s.sawLimit = limit
	return s.results, s.err
}

// TestOneBrokenSearcherDoesNotBreakTheSearch
//
// A search box that stops working entirely because one module's query is broken is a search box
// that fails for reasons the user cannot see or fix. What the other modules found is returned,
// and the one that did not answer is NAMED — 7.4 D3's rule again: report rather than refuse, so
// the evidence survives.
func TestOneBrokenSearcherDoesNotBreakTheSearch(t *testing.T) {
	working := &stub{name: "catalog", results: []search.Result{
		{Kind: "catalog.product", Label: "Widget"},
	}}
	broken := &stub{name: "sales", err: errors.New("the table is gone")}

	response, err := search.New(working, broken).Search(
		context.Background(), id.ID("c"), "wid", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != 1 {
		t.Errorf("%d results, want the one the working searcher found", len(response.Results))
	}
	if len(response.Failed) != 1 || response.Failed[0] != "sales" {
		t.Errorf("failed = %v, want [sales] — a search that silently drops a source is a "+
			"search whose emptiness cannot be interpreted", response.Failed)
	}
}

// TestEachSearcherIsBoundedSeparately
//
// A shop searching "AH" must not get two hundred products and no partners, purely because the
// catalogue was asked first. Bounding the TOTAL would do exactly that, and "find me Ahmad" is the
// query it would fail on.
func TestEachSearcherIsBoundedSeparately(t *testing.T) {
	// The first searcher must actually RETURN something, or a shared budget and a separate one
	// are the same number: a drill subtracting what had already been found changed nothing while
	// both stubs returned nothing at all.
	first := &stub{name: "catalog", results: []search.Result{
		{Kind: "catalog.product", Label: "Ahmadi oil"},
		{Kind: "catalog.product", Label: "Ahmadi soap"},
		{Kind: "catalog.product", Label: "Ahmadi rice"},
	}}
	second := &stub{name: "partner", results: []search.Result{
		{Kind: "partner.partner", Label: "Ahmad Trading"},
	}}

	if _, err := search.New(first, second).Search(
		context.Background(), id.ID("c"), "ah", 5); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if first.sawLimit != 5 || second.sawLimit != 5 {
		t.Errorf("limits were %d and %d, want 5 each — a shared budget means whoever answers "+
			"first crowds the others out", first.sawLimit, second.sawLimit)
	}
}

// TestASearcherThatFindsNothingIsNotAFailure
//
// Empty and broken are different answers, and a caller deciding whether to say "nothing found" or
// "search is having trouble" needs to tell them apart.
func TestASearcherThatFindsNothingIsNotAFailure(t *testing.T) {
	response, err := search.New(&stub{name: "catalog"}).Search(
		context.Background(), id.ID("c"), "zz", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Failed) != 0 {
		t.Errorf("failed = %v — finding nothing was reported as a failure", response.Failed)
	}
}

// TestResultsAreGroupedByKindAndRankedWithinIt
//
// NOT one global relevance score. Comparing "how well does this product match" with "how well
// does this invoice match" needs a scale neither module knows about, and inventing one would make
// the order look meaningful when it is arbitrary.
func TestResultsAreGroupedByKindAndRankedWithinIt(t *testing.T) {
	catalog := &stub{name: "catalog", results: []search.Result{
		{Kind: "catalog.product", Label: "Zinc", Rank: 0},
		{Kind: "catalog.product", Label: "Aluminium", Rank: 2},
	}}
	partners := &stub{name: "partner", results: []search.Result{
		{Kind: "partner.partner", Label: "Ahmad", Rank: 0},
	}}

	// A one-character query is refused before any searcher is asked: it matches most of a
	// catalogue, and a screen firing on every keystroke would send it.
	_, err := search.New(partners, catalog).Search(
		context.Background(), id.ID("c"), "a", 10)
	if code := errs.CodeOf(err); code != search.CodeQueryTooShort {
		t.Fatalf("a one-character query gave %q, want %q", code, search.CodeQueryTooShort)
	}

	response, err := search.New(partners, catalog).Search(
		context.Background(), id.ID("c"), "an", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != 3 {
		t.Fatalf("%d results, want 3", len(response.Results))
	}
	// Catalogue before partner, because the KIND sorts first — and registration order was the
	// other way round, which is what makes this an assertion about the sort.
	if response.Results[0].Kind != "catalog.product" {
		t.Errorf("first kind = %q, want catalog.product", response.Results[0].Kind)
	}
	// Within the kind, the better rank wins even though its label sorts later.
	if response.Results[0].Label != "Zinc" {
		t.Errorf("first label = %q, want Zinc — rank 0 beats rank 2 regardless of alphabet",
			response.Results[0].Label)
	}
}

// TestAWildcardInTheQueryMatchesLiterally
//
// A query containing `%` would otherwise match everything, so a customer searching for a product
// called "50%" gets the whole catalogue. `_` is worse: it matches any character silently.
func TestAWildcardInTheQueryMatchesLiterally(t *testing.T) {
	for _, test := range []struct{ query, want string }{
		{"50%", `%50\%%`},
		{"a_b", `%a\_b%`},
		{`back\slash`, `%back\\slash%`},
		{"Ahmad", "%ahmad%"},
	} {
		if got := search.Like(test.query); got != test.want {
			t.Errorf("Like(%q) = %q, want %q", test.query, got, test.want)
		}
	}
}

// TestAQueryIsTrimmedBeforeItIsMeasured
//
// A screen sends what was typed, and "  a " is one character with two spaces. Measuring before
// trimming would run a one-character search; trimming after measuring would run one too.
func TestAQueryIsTrimmedBeforeItIsMeasured(t *testing.T) {
	registry := search.New(&stub{name: "catalog"})

	if _, err := registry.Search(context.Background(), id.ID("c"), "  a  ", 10); err == nil {
		t.Error("a padded one-character query was accepted")
	}

	response, err := registry.Search(context.Background(), id.ID("c"), "  ab  ", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if response.Query != "ab" {
		t.Errorf("query = %q, want the trimmed %q", response.Query, "ab")
	}
}
