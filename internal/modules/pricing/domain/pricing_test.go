package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/pricing/domain"
)

const (
	variantID = id.ID("variant-red-l")
	productID = id.ID("product-shirt")
	today     = "2026-06-15"
)

func list(code string, isDefault bool) domain.List {
	return domain.List{
		ID: id.ID(code), Code: code, Name: code, CurrencyCode: "SYP",
		IsDefault: isDefault, Direction: domain.Sale, IsActive: true,
	}
}

func item(target id.ID, priceMinor, minQuantity int64, variantLevel bool) domain.Item {
	built := domain.Item{
		ID: target + "-item", PriceMinor: priceMinor,
		MinQuantity: minQuantity, IsActive: true,
	}
	if variantLevel {
		built.VariantID = target
	} else {
		built.ProductID = target
	}
	return built
}

// candidates builds the three-level chain in priority order, with whichever prices each holds.
func candidates(partner, branch, base domain.Candidate) []domain.Candidate {
	return []domain.Candidate{partner, branch, base}
}

func partnerList(variantItems, productItems []domain.Item) domain.Candidate {
	return domain.Candidate{
		List:   list("WHOLESALE", false),
		Source: domain.SourcePartnerVariant, ProductSource: domain.SourcePartnerProduct,
		VariantItems: variantItems, ProductItems: productItems,
	}
}

func branchList(variantItems, productItems []domain.Item) domain.Candidate {
	return domain.Candidate{
		List:   list("SHOP2", false),
		Source: domain.SourceBranchVariant, ProductSource: domain.SourceBranchProduct,
		VariantItems: variantItems, ProductItems: productItems,
	}
}

func defaultList(variantItems, productItems []domain.Item) domain.Candidate {
	return domain.Candidate{
		List:   list("RETAIL", true),
		Source: domain.SourceDefaultVariant, ProductSource: domain.SourceDefaultProduct,
		VariantItems: variantItems, ProductItems: productItems,
	}
}

// ── the documented order ────────────────────────────────────────────────────────

// §2.6's order, one case per level, each proving that the level above it was consulted first.
func TestResolutionFollowsTheDocumentedOrder(t *testing.T) {
	cases := []struct {
		name       string
		chain      []domain.Candidate
		wantMinor  int64
		wantSource domain.Source
	}{
		{
			"the partner's list wins over everything",
			candidates(
				partnerList([]domain.Item{item(variantID, 800, 0, true)}, nil),
				branchList([]domain.Item{item(variantID, 900, 0, true)}, nil),
				defaultList([]domain.Item{item(variantID, 1000, 0, true)}, nil),
			),
			800, domain.SourcePartnerVariant,
		},
		{
			"the branch's list wins when the partner has none",
			candidates(
				partnerList(nil, nil),
				branchList([]domain.Item{item(variantID, 900, 0, true)}, nil),
				defaultList([]domain.Item{item(variantID, 1000, 0, true)}, nil),
			),
			900, domain.SourceBranchVariant,
		},
		{
			"the company default is the last stop",
			candidates(
				partnerList(nil, nil), branchList(nil, nil),
				defaultList([]domain.Item{item(variantID, 1000, 0, true)}, nil),
			),
			1000, domain.SourceDefaultVariant,
		},
		{
			"a variant price beats a product price in the SAME list",
			candidates(
				partnerList(nil, nil), branchList(nil, nil),
				defaultList(
					[]domain.Item{item(variantID, 1200, 0, true)},
					[]domain.Item{item(productID, 1000, 0, false)},
				),
			),
			1200, domain.SourceDefaultVariant,
		},
		{
			"a product price answers when the variant has none",
			candidates(
				partnerList(nil, nil), branchList(nil, nil),
				defaultList(nil, []domain.Item{item(productID, 1000, 0, false)}),
			),
			1000, domain.SourceDefaultProduct,
		},
		{
			"a HIGHER list's product price beats a lower list's variant price",
			candidates(
				partnerList(nil, []domain.Item{item(productID, 700, 0, false)}),
				branchList(nil, nil),
				defaultList([]domain.Item{item(variantID, 1000, 0, true)}, nil),
			),
			700, domain.SourcePartnerProduct,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := domain.Resolve(tc.chain, variantID, 1_000_000, today)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if resolved.PriceMinor != tc.wantMinor {
				t.Errorf("price = %d, want %d", resolved.PriceMinor, tc.wantMinor)
			}
			// The recorded reason matters as much as the number: a price a salesperson cannot
			// explain is one they override by hand, and every hand override is an unapproved
			// discount.
			if resolved.Source != tc.wantSource {
				t.Errorf("source = %q, want %q", resolved.Source, tc.wantSource)
			}
		})
	}
}

// The last case above deserves stating on its own: list priority outranks granularity. A
// customer's negotiated blanket price applies even where the retail list prices that exact
// variant, which is the whole reason they negotiated it.
func TestListPriorityOutranksGranularity(t *testing.T) {
	resolved, err := domain.Resolve(candidates(
		partnerList(nil, []domain.Item{item(productID, 700, 0, false)}),
		branchList(nil, nil),
		defaultList([]domain.Item{item(variantID, 1000, 0, true)}, nil),
	), variantID, 1_000_000, today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.PriceMinor != 700 {
		t.Errorf("price = %d, want the partner's blanket 700", resolved.PriceMinor)
	}
}

// ── quantity breaks ─────────────────────────────────────────────────────────────

// The highest break the line qualifies for wins. Taking the first match instead would charge the
// base price on an order of ten and lose the sale the discount existed to win.
func TestTheHighestQualifyingQuantityBreakWins(t *testing.T) {
	breaks := []domain.Item{
		item(variantID, 1000, 0, true),          // 1+
		item(variantID, 900, 10_000_000, true),  // 10+
		item(variantID, 800, 100_000_000, true), // 100+
	}

	cases := []struct {
		quantityMicro int64
		wantMinor     int64
		wantBreak     int64
	}{
		{1_000_000, 1000, 0},
		{9_000_000, 1000, 0},
		{10_000_000, 900, 10_000_000}, // exactly at the break
		{99_000_000, 900, 10_000_000},
		{100_000_000, 800, 100_000_000},
		{500_000_000, 800, 100_000_000},
	}

	for _, tc := range cases {
		resolved, err := domain.Resolve(
			candidates(partnerList(nil, nil), branchList(nil, nil), defaultList(breaks, nil)),
			variantID, tc.quantityMicro, today)
		if err != nil {
			t.Fatalf("Resolve(%d): %v", tc.quantityMicro, err)
		}
		if resolved.PriceMinor != tc.wantMinor {
			t.Errorf("%d units priced at %d, want %d",
				tc.quantityMicro, resolved.PriceMinor, tc.wantMinor)
		}
		// The break is reported, so a screen can say "10 or more" rather than leaving a
		// salesperson to wonder why the price changed.
		if resolved.MinQuantity != tc.wantBreak {
			t.Errorf("%d units reported break %d, want %d",
				tc.quantityMicro, resolved.MinQuantity, tc.wantBreak)
		}
	}
}

// Breaks are not ordered in the database, and nothing guarantees they will be.
func TestQuantityBreaksResolveRegardlessOfOrder(t *testing.T) {
	shuffled := []domain.Item{
		item(variantID, 800, 100_000_000, true),
		item(variantID, 1000, 0, true),
		item(variantID, 900, 10_000_000, true),
	}

	resolved, err := domain.Resolve(
		candidates(partnerList(nil, nil), branchList(nil, nil), defaultList(shuffled, nil)),
		variantID, 50_000_000, today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.PriceMinor != 900 {
		t.Errorf("price = %d, want 900", resolved.PriceMinor)
	}
}

// A break the line does not reach is not merely skipped — the list must fall through to the
// next candidate if nothing in it qualifies.
func TestAListWhoseOnlyPriceNeedsMoreUnitsIsSkipped(t *testing.T) {
	resolved, err := domain.Resolve(candidates(
		partnerList([]domain.Item{item(variantID, 800, 100_000_000, true)}, nil),
		branchList(nil, nil),
		defaultList([]domain.Item{item(variantID, 1000, 0, true)}, nil),
	), variantID, 1_000_000, today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.PriceMinor != 1000 || resolved.Source != domain.SourceDefaultVariant {
		t.Errorf("resolved %d from %q, want 1000 from the default list",
			resolved.PriceMinor, resolved.Source)
	}
}

// ── validity dates ──────────────────────────────────────────────────────────────

func TestAListOutOfDateIsSkipped(t *testing.T) {
	seasonal := partnerList([]domain.Item{item(variantID, 500, 0, true)}, nil)
	seasonal.List.ValidFrom = "2026-12-01"
	seasonal.List.ValidTo = "2026-12-31"

	// In June the sale list has not started.
	resolved, err := domain.Resolve(candidates(
		seasonal, branchList(nil, nil),
		defaultList([]domain.Item{item(variantID, 1000, 0, true)}, nil),
	), variantID, 1_000_000, "2026-06-15")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.PriceMinor != 1000 {
		t.Errorf("price = %d in June, want the default 1000", resolved.PriceMinor)
	}

	// In December it does.
	resolved, err = domain.Resolve(candidates(
		seasonal, branchList(nil, nil),
		defaultList([]domain.Item{item(variantID, 1000, 0, true)}, nil),
	), variantID, 1_000_000, "2026-12-15")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.PriceMinor != 500 {
		t.Errorf("price = %d in December, want the seasonal 500", resolved.PriceMinor)
	}
}

// A list with no dates is always in force, which is what a shop with one price list wants and
// never has to think about.
func TestAListWithNoDatesIsAlwaysInForce(t *testing.T) {
	plain := list("RETAIL", true)
	for _, date := range []string{"1999-01-01", "2026-06-15", "2099-12-31"} {
		if !plain.InForce(date) {
			t.Errorf("a dateless list was out of force on %s", date)
		}
	}
}

func TestARetiredListIsSkipped(t *testing.T) {
	retired := partnerList([]domain.Item{item(variantID, 500, 0, true)}, nil)
	retired.List.IsActive = false

	resolved, err := domain.Resolve(candidates(
		retired, branchList(nil, nil),
		defaultList([]domain.Item{item(variantID, 1000, 0, true)}, nil),
	), variantID, 1_000_000, today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.PriceMinor != 1000 {
		t.Errorf("price = %d, want the default 1000 — the retired list answered", resolved.PriceMinor)
	}
}

func TestARetiredItemIsSkipped(t *testing.T) {
	retired := item(variantID, 500, 0, true)
	retired.IsActive = false

	resolved, err := domain.Resolve(candidates(
		partnerList([]domain.Item{retired}, nil), branchList(nil, nil),
		defaultList([]domain.Item{item(variantID, 1000, 0, true)}, nil),
	), variantID, 1_000_000, today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.PriceMinor != 1000 {
		t.Errorf("price = %d, want 1000 — a retired price answered", resolved.PriceMinor)
	}
}

// ── no price is an ERROR ────────────────────────────────────────────────────────

// Not zero. A variant nobody has priced is a configuration gap; returning zero would sell it for
// nothing, silently, on a receipt that looks perfectly ordinary.
func TestAnUnpricedVariantIsAnErrorNotZero(t *testing.T) {
	_, err := domain.Resolve(
		candidates(partnerList(nil, nil), branchList(nil, nil), defaultList(nil, nil)),
		variantID, 1_000_000, today)
	if err == nil {
		t.Fatal("an unpriced variant resolved to something")
	}
	if code := errs.CodeOf(err); code != domain.CodeNoPrice {
		t.Errorf("code = %q, want %q", code, domain.CodeNoPrice)
	}
	typed, _ := errs.AsError(err)
	if typed.Params["variant"] != string(variantID) {
		t.Errorf("params = %v, want the variant named", typed.Params)
	}
}

func TestNoCandidatesAtAllIsAnError(t *testing.T) {
	if _, err := domain.Resolve(nil, variantID, 1_000_000, today); err == nil {
		t.Fatal("a price was resolved with no lists at all")
	}
}

// A price of zero is a real price — a free sample, a promotional item — and must be
// distinguishable from "no price". This is the pair to the test above.
func TestAPriceOfZeroIsAPriceNotAnAbsence(t *testing.T) {
	resolved, err := domain.Resolve(candidates(
		partnerList([]domain.Item{item(variantID, 0, 0, true)}, nil),
		branchList(nil, nil),
		defaultList([]domain.Item{item(variantID, 1000, 0, true)}, nil),
	), variantID, 1_000_000, today)
	if err != nil {
		t.Fatalf("a free item was treated as unpriced: %v", err)
	}
	if resolved.PriceMinor != 0 {
		t.Errorf("price = %d, want 0", resolved.PriceMinor)
	}
	if resolved.Source != domain.SourcePartnerVariant {
		t.Errorf("source = %q, want the partner list that priced it at zero", resolved.Source)
	}
}

// ── construction ────────────────────────────────────────────────────────────────

// A row pricing neither target prices nothing; one pricing both would have two answers and no
// rule to choose between them.
func TestAPriceTargetsExactlyOneThing(t *testing.T) {
	identifier, listID := id.ID("i"), id.ID("l")

	if _, err := domain.NewItem(identifier, listID, id.ID(""), id.ID(""), 100, 0); err == nil {
		t.Error("a price targeting nothing was accepted")
	}
	if _, err := domain.NewItem(identifier, listID, productID, variantID, 100, 0); err == nil {
		t.Error("a price targeting both a product and a variant was accepted")
	}
	if _, err := domain.NewItem(identifier, listID, productID, id.ID(""), 100, 0); err != nil {
		t.Errorf("a product-level price was refused: %v", err)
	}
	if _, err := domain.NewItem(identifier, listID, id.ID(""), variantID, 100, 0); err != nil {
		t.Errorf("a variant-level price was refused: %v", err)
	}
}

// A negative price would pay the customer to take the goods, and every total derived from it
// would be wrong in a direction no report flags.
func TestANegativePriceIsRefused(t *testing.T) {
	_, err := domain.NewItem(id.ID("i"), id.ID("l"), productID, id.ID(""), -1, 0)
	if err == nil {
		t.Fatal("a negative price was accepted")
	}
	if code := errs.CodeOf(err); code != domain.CodeNegativePrice {
		t.Errorf("code = %q, want %q", code, domain.CodeNegativePrice)
	}
}
