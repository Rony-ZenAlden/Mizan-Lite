package domain_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/catalog/domain"
)

func values(codes ...string) []domain.AttributeValue {
	out := make([]domain.AttributeValue, len(codes))
	for i, code := range codes {
		out[i] = domain.AttributeValue{ID: id.ID(code), Code: code, Name: code, IsActive: true}
	}
	return out
}

func dimension(code string, valueCodes ...string) domain.Dimension {
	return domain.Dimension{
		AttributeID: id.ID(code), AttributeCode: code, Values: values(valueCodes...),
	}
}

func shirt() domain.Product {
	return domain.Product{ID: id.ID("shirt"), Code: "SHIRT", Name: "Shirt", IsActive: true}
}

func variant(sku, combination string, active, history bool) domain.Variant {
	return domain.Variant{
		ID: id.ID(sku), ProductID: id.ID("shirt"), SKU: sku, Combination: combination,
		IsActive: active, HasHistory: history,
	}
}

// The default variant a product is born with, which generation must never touch.
func defaultVariant() domain.Variant {
	return domain.Variant{
		ID: id.ID("default"), ProductID: id.ID("shirt"), SKU: "SHIRT",
		IsDefault: true, Combination: "", IsActive: true,
	}
}

func skus(specs []domain.VariantSpec) []string {
	out := make([]string, len(specs))
	for i, spec := range specs {
		out[i] = spec.SKU
	}
	return out
}

func variantSKUs(list []domain.Variant) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = v.SKU
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── the Cartesian product ───────────────────────────────────────────────────────

func TestGenerationIsTheCartesianProduct(t *testing.T) {
	plan, err := domain.GeneratePlan(shirt(),
		[]domain.Dimension{dimension("COLOUR", "RED", "BLUE"), dimension("SIZE", "S", "M", "L")},
		nil, []domain.Variant{defaultVariant()})
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	// 2 colours × 3 sizes.
	if len(plan.Create) != 6 {
		t.Fatalf("%d variants planned, want 6: %v", len(plan.Create), skus(plan.Create))
	}
	want := []string{
		"SHIRT-BLUE-L", "SHIRT-BLUE-M", "SHIRT-BLUE-S",
		"SHIRT-RED-L", "SHIRT-RED-M", "SHIRT-RED-S",
	}
	if got := skus(plan.Create); !equal(got, want) {
		t.Errorf("planned %v, want %v", got, want)
	}
	// The default variant is left alone, always — it is the row §A.1 guarantees.
	if len(plan.Delete) != 0 || len(plan.Deactivate) != 0 {
		t.Errorf("the default variant was scheduled for removal: %+v", plan)
	}
}

// A plan whose rows shuffle between refreshes is one nobody trusts, and a SKU that changed
// between runs would orphan every label already printed.
func TestAPlanIsDeterministic(t *testing.T) {
	dimensions := []domain.Dimension{
		dimension("SIZE", "L", "S", "M"), dimension("COLOUR", "BLUE", "RED"),
	}

	first, err := domain.GeneratePlan(shirt(), dimensions, nil, nil)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	second, err := domain.GeneratePlan(shirt(), dimensions, nil, nil)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if !equal(skus(first.Create), skus(second.Create)) {
		t.Errorf("two runs planned %v then %v", skus(first.Create), skus(second.Create))
	}

	// And the attribute order in the input does not change the output.
	reordered, err := domain.GeneratePlan(shirt(), []domain.Dimension{
		dimension("COLOUR", "RED", "BLUE"), dimension("SIZE", "S", "M", "L"),
	}, nil, nil)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if !equal(skus(first.Create), skus(reordered.Create)) {
		t.Errorf("attribute order changed the plan: %v vs %v",
			skus(first.Create), skus(reordered.Create))
	}
}

// ── exclusions ──────────────────────────────────────────────────────────────────

// The manufacturer does not make red in XXL. Without exclusions the choice is a variant that
// sits at zero stock forever, polluting every picker and every stock count, or dropping the
// whole attribute.
func TestAnExclusionRemovesExactlyOneCombination(t *testing.T) {
	exclusion, err := domain.NewExclusion(id.ID("x1"), []domain.Assignment{
		{AttributeCode: "COLOUR", ValueCode: "RED"},
		{AttributeCode: "SIZE", ValueCode: "XXL"},
	}, "not manufactured")
	if err != nil {
		t.Fatalf("NewExclusion: %v", err)
	}

	plan, err := domain.GeneratePlan(shirt(),
		[]domain.Dimension{dimension("COLOUR", "RED", "BLUE"), dimension("SIZE", "L", "XXL")},
		[]domain.Exclusion{exclusion}, nil)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	// 2 × 2 minus the one excluded.
	want := []string{"SHIRT-BLUE-L", "SHIRT-BLUE-XXL", "SHIRT-RED-L"}
	if got := skus(plan.Create); !equal(got, want) {
		t.Errorf("planned %v, want %v", got, want)
	}
}

// A partial exclusion is a SUBSET test, which is what lets a manufacturer who drops a colour say
// so once rather than listing every size it came in.
func TestAPartialExclusionRemovesEveryCombinationContainingIt(t *testing.T) {
	exclusion, err := domain.NewExclusion(id.ID("x1"), []domain.Assignment{
		{AttributeCode: "COLOUR", ValueCode: "RED"},
	}, "discontinued colour")
	if err != nil {
		t.Fatalf("NewExclusion: %v", err)
	}

	plan, err := domain.GeneratePlan(shirt(),
		[]domain.Dimension{dimension("COLOUR", "RED", "BLUE"), dimension("SIZE", "S", "M", "L")},
		[]domain.Exclusion{exclusion}, nil)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	want := []string{"SHIRT-BLUE-L", "SHIRT-BLUE-M", "SHIRT-BLUE-S"}
	if got := skus(plan.Create); !equal(got, want) {
		t.Errorf("planned %v, want %v", got, want)
	}
}

// An exclusion naming nothing would match every combination and silently empty the product's
// catalog, so it cannot be built.
func TestAnExclusionMustNameSomething(t *testing.T) {
	if _, err := domain.NewExclusion(id.ID("x1"), nil, "everything"); err == nil {
		t.Fatal("an exclusion matching every variant was accepted")
	} else if code := errs.CodeOf(err); code != domain.CodeInvalidExclusion {
		t.Errorf("code = %q, want %q", code, domain.CodeInvalidExclusion)
	}

	// And one already in storage cannot do the damage either.
	plan, err := domain.GeneratePlan(shirt(),
		[]domain.Dimension{dimension("COLOUR", "RED", "BLUE")},
		[]domain.Exclusion{{ID: id.ID("legacy"), Combination: ""}}, nil)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if len(plan.Create) != 2 {
		t.Errorf("an empty exclusion wiped the catalog: %d planned, want 2", len(plan.Create))
	}
}

// ── the diff: create, keep, deactivate, delete ──────────────────────────────────

// The rule that matters, and it is decided by the DOMAIN rather than by the caller or the
// screen: a variant that appears on a document is deactivated; one that never did is deleted.
func TestAVariantWithHistoryIsDeactivatedAndOneWithoutIsDeleted(t *testing.T) {
	existing := []domain.Variant{
		defaultVariant(),
		variant("SHIRT-RED-S", "COLOUR:RED|SIZE:S", true, true),  // sold; dropping RED
		variant("SHIRT-RED-M", "COLOUR:RED|SIZE:M", true, false), // never used
		variant("SHIRT-BLUE-S", "COLOUR:BLUE|SIZE:S", true, false),
	}

	// RED is gone from the offered values.
	plan, err := domain.GeneratePlan(shirt(),
		[]domain.Dimension{dimension("COLOUR", "BLUE"), dimension("SIZE", "S", "M")},
		nil, existing)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	if got := variantSKUs(plan.Deactivate); !equal(got, []string{"SHIRT-RED-S"}) {
		t.Errorf("deactivating %v, want [SHIRT-RED-S] — the one with history", got)
	}
	if got := variantSKUs(plan.Delete); !equal(got, []string{"SHIRT-RED-M"}) {
		t.Errorf("deleting %v, want [SHIRT-RED-M] — the one without", got)
	}
	if got := skus(plan.Create); !equal(got, []string{"SHIRT-BLUE-M"}) {
		t.Errorf("creating %v, want [SHIRT-BLUE-M]", got)
	}
	// SHIRT-BLUE-S is still wanted and already active; so is the default.
	if plan.Unchanged != 2 {
		t.Errorf("unchanged = %d, want 2", plan.Unchanged)
	}
}

// Bringing a dropped colour back reuses the row, which keeps its history, its barcodes, and its
// SKU. Creating a new one would orphan every label already printed for it.
func TestAWantedVariantThatWasRetiredIsReactivatedNotRecreated(t *testing.T) {
	existing := []domain.Variant{
		defaultVariant(),
		variant("SHIRT-RED-S", "COLOUR:RED|SIZE:S", false, true), // retired, being brought back
	}

	plan, err := domain.GeneratePlan(shirt(),
		[]domain.Dimension{dimension("COLOUR", "RED"), dimension("SIZE", "S")},
		nil, existing)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	if len(plan.Create) != 0 {
		t.Errorf("a retired variant was re-created rather than revived: %v", skus(plan.Create))
	}
	if got := variantSKUs(plan.Reactivate); !equal(got, []string{"SHIRT-RED-S"}) {
		t.Errorf("reactivating %v, want [SHIRT-RED-S]", got)
	}
}

// An already-retired variant that is still unwanted is left exactly as it is. Deactivating it
// again would write a row version and an audit entry for a change that did not happen.
func TestAnAlreadyRetiredUnwantedVariantIsLeftAlone(t *testing.T) {
	existing := []domain.Variant{
		defaultVariant(),
		variant("SHIRT-RED-S", "COLOUR:RED|SIZE:S", false, true),
	}

	plan, err := domain.GeneratePlan(shirt(),
		[]domain.Dimension{dimension("COLOUR", "BLUE"), dimension("SIZE", "S")},
		nil, existing)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if len(plan.Deactivate) != 0 {
		t.Errorf("an already-retired variant was deactivated again: %v",
			variantSKUs(plan.Deactivate))
	}
	if len(plan.Delete) != 0 {
		t.Errorf("a variant with history was deleted: %v", variantSKUs(plan.Delete))
	}
}

// Re-running generation over an unchanged product must propose nothing, so that "apply" is safe
// to press twice and a preview showing changes always means there are changes.
func TestGeneratingTwiceProposesNothingTheSecondTime(t *testing.T) {
	dimensions := []domain.Dimension{
		dimension("COLOUR", "RED", "BLUE"), dimension("SIZE", "S", "M"),
	}

	first, err := domain.GeneratePlan(shirt(), dimensions, nil, []domain.Variant{defaultVariant()})
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	// Pretend the plan was applied.
	existing := []domain.Variant{defaultVariant()}
	for _, spec := range first.Create {
		existing = append(existing, variant(spec.SKU, spec.Combination, true, false))
	}

	second, err := domain.GeneratePlan(shirt(), dimensions, nil, existing)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if !second.IsEmpty() {
		t.Errorf("re-generating proposed changes: %+v", second)
	}
	if second.Unchanged != len(existing) {
		t.Errorf("unchanged = %d, want %d", second.Unchanged, len(existing))
	}
}

// ── the refusals ────────────────────────────────────────────────────────────────

// An empty plan would read as "nothing to do" when the real answer is "you have not chosen a
// dimension yet".
func TestGeneratingWithNoDimensionsIsRefused(t *testing.T) {
	_, err := domain.GeneratePlan(shirt(), nil, nil, nil)
	if err == nil {
		t.Fatal("generation ran on a product with no variant-defining attributes")
	}
	if code := errs.CodeOf(err); code != domain.CodeNoDimensions {
		t.Errorf("code = %q, want %q", code, domain.CodeNoDimensions)
	}
}

// One dimension with no values makes the Cartesian product EMPTY, which would present as
// "delete every variant you have" — a catastrophic reading of a half-finished configuration.
func TestADimensionWithNoValuesIsRefusedRatherThanEmptyingTheProduct(t *testing.T) {
	existing := []domain.Variant{
		defaultVariant(),
		variant("SHIRT-RED-S", "COLOUR:RED|SIZE:S", true, false),
		variant("SHIRT-RED-M", "COLOUR:RED|SIZE:M", true, false),
	}

	_, err := domain.GeneratePlan(shirt(),
		[]domain.Dimension{dimension("COLOUR", "RED"), {AttributeCode: "SIZE"}},
		nil, existing)
	if err == nil {
		t.Fatal("a dimension offering no values produced a plan")
	}
	if code := errs.CodeOf(err); code != domain.CodeNoValuesOffered {
		t.Errorf("code = %q, want %q", code, domain.CodeNoValuesOffered)
	}
	typed, _ := errs.AsError(err)
	if typed.Params["attribute"] != "SIZE" {
		t.Errorf("params = %v, want the attribute that offers nothing", typed.Params)
	}
}

// Five attributes with six values each is 7,776 variants, and the person who added the fifth was
// thinking about the fifth. The cap turns an unusable catalog back into an error, before
// anything is written.
func TestAnExplosionOfVariantsIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	big := []domain.Dimension{
		dimension("A", "1", "2", "3", "4", "5", "6"),
		dimension("B", "1", "2", "3", "4", "5", "6"),
		dimension("C", "1", "2", "3", "4", "5", "6"),
		dimension("D", "1", "2", "3", "4", "5", "6"),
		dimension("E", "1", "2", "3", "4", "5", "6"),
	}

	_, err := domain.GeneratePlan(shirt(), big, nil, nil)
	if err == nil {
		t.Fatal("7776 variants were planned without complaint")
	}
	if code := errs.CodeOf(err); code != domain.CodeTooManyVariants {
		t.Errorf("code = %q, want %q", code, domain.CodeTooManyVariants)
	}
	// The limit is named, because "too many" without a number is not actionable.
	typed, _ := errs.AsError(err)
	if typed.Params["limit"] == "" {
		t.Errorf("params = %v, want the limit named", typed.Params)
	}
}

// ── §B.5: a packaging is not a unit of measure ──────────────────────────────────

func TestAPackagingHoldsAPositiveQuantity(t *testing.T) {
	identifiers := ids(t, 2)

	box, err := domain.NewPackaging(identifiers[0], identifiers[1], "box", "Box of 12", 12_000_000)
	if err != nil {
		t.Fatalf("NewPackaging: %v", err)
	}
	if box.Code != "BOX" {
		t.Errorf("code = %q, want BOX", box.Code)
	}
	if box.QuantityMicro != 12_000_000 {
		t.Errorf("quantity = %d, want 12000000", box.QuantityMicro)
	}

	// A box of zero would make every scan of it add nothing; a negative one would remove stock
	// on a sale. Both are silent at the till.
	for _, quantity := range []int64{0, -12_000_000} {
		if _, err = domain.NewPackaging(
			identifiers[0], identifiers[1], "BOX", "Box", quantity); err == nil {
			t.Errorf("a packaging holding %d was accepted", quantity)
		}
	}
}

// ── §A.5: a scan resolves to one variant and a quantity ─────────────────────────

func TestAUnitBarcodeResolvesToOneOfTheStockUnit(t *testing.T) {
	identifiers := ids(t, 2)

	barcode, err := domain.NewBarcode(identifiers[0], identifiers[1], "5901234123457", "")
	if err != nil {
		t.Fatalf("NewBarcode: %v", err)
	}
	if barcode.Type != domain.BarcodeInternal {
		t.Errorf("type = %q, want the internal default", barcode.Type)
	}

	scan := domain.Resolve(barcode, nil)
	if scan.VariantID != identifiers[1] {
		t.Error("the scan resolved to the wrong variant")
	}
	if scan.QuantityMicro != 1_000_000 {
		t.Errorf("quantity = %d, want one of the stock unit", scan.QuantityMicro)
	}
	if !scan.PackagingID.IsZero() {
		t.Error("a unit barcode resolved to a packaging")
	}
}

// Scanning the box at the till adds 12, not 1. That is the whole reason a barcode may point at a
// packaging as well as a variant.
func TestAPackagingBarcodeResolvesToItsQuantity(t *testing.T) {
	identifiers := ids(t, 3)

	box, err := domain.NewPackaging(identifiers[0], identifiers[2], "BOX", "Box of 12", 12_000_000)
	if err != nil {
		t.Fatalf("NewPackaging: %v", err)
	}
	barcode, err := domain.NewBarcode(identifiers[1], identifiers[2], "5901234123464", "ean13")
	if err != nil {
		t.Fatalf("NewBarcode: %v", err)
	}
	barcode.PackagingID = box.ID

	scan := domain.Resolve(barcode, &box)
	if scan.VariantID != identifiers[2] {
		t.Error("the scan resolved to the wrong variant")
	}
	if scan.QuantityMicro != 12_000_000 {
		t.Errorf("quantity = %d, want 12000000 — the box holds twelve", scan.QuantityMicro)
	}
	if scan.PackagingID != box.ID {
		t.Error("the scan did not report which packaging answered")
	}
}

// A barcode is scanned, not typed: the scanner sends exactly what is printed, and folding case
// would make two distinct printed codes collide — the one thing a barcode must never do.
func TestABarcodeIsNotCaseFolded(t *testing.T) {
	identifiers := ids(t, 2)

	barcode, err := domain.NewBarcode(identifiers[0], identifiers[1], "  abc123  ", "code128")
	if err != nil {
		t.Fatalf("NewBarcode: %v", err)
	}
	if barcode.Code != "abc123" {
		t.Errorf("code = %q, want abc123 — trimmed but not upper-cased", barcode.Code)
	}
}

func TestABarcodeNeedsACodeAndAVariant(t *testing.T) {
	identifiers := ids(t, 2)

	if _, err := domain.NewBarcode(identifiers[0], identifiers[1], "   ", ""); err == nil {
		t.Error("a blank barcode was accepted")
	}
	if _, err := domain.NewBarcode(identifiers[0], id.ID(""), "123", ""); err == nil {
		t.Error("a barcode pointing at nothing was accepted")
	}
}
