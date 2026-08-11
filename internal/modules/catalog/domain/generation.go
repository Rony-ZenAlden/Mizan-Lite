package domain

import (
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// Stable codes for generation and barcodes.
const (
	CodeNoDimensions     = "catalog.no_variant_dimensions"
	CodeNoValuesOffered  = "catalog.no_values_offered"
	CodeTooManyVariants  = "catalog.too_many_variants"
	CodeInvalidExclusion = "catalog.invalid_exclusion"
	CodeInvalidBarcode   = "catalog.invalid_barcode"
	CodeInvalidPackaging = "catalog.invalid_packaging"
	CodeBarcodeInUse     = "catalog.barcode_in_use"
)

// maxGeneratedVariants caps one generation run.
//
// Not a technical limit — 10,000 rows is nothing — but a guard against the arithmetic nobody
// does in their head: five attributes with six values each is 7,776 variants, and the person who
// added the fifth attribute was thinking about the fifth attribute. The cap turns a catalog
// nobody can use back into an error naming the number, before anything is written.
const maxGeneratedVariants = 1000

// Dimension is one variant-defining attribute and the values a product offers for it.
type Dimension struct {
	AttributeID   id.ID
	AttributeCode string
	Values        []AttributeValue
}

// Exclusion is a combination a product never makes.
//
// PARTIAL: 'COLOUR:RED|SIZE:XXL' excludes exactly that one, while 'COLOUR:RED' excludes every
// red variant whatever its size. That is what makes it usable — a manufacturer who drops a
// colour says so once rather than listing every size it came in.
type Exclusion struct {
	ID          id.ID
	Combination string
	Reason      string
}

// Plan is what generation proposes, before anything is written.
//
// # Why a plan rather than an action
//
// §2.4: adding a fifth colour to a product that has four sizes silently creating twenty SKUs is
// exactly the surprise a careful ERP avoids. The caller previews this, sees "20 new, 4 to
// deactivate, 0 to delete", and decides — the same staged-change shape exchange rates use
// (§18.4).
//
// The split between Deactivate and Delete is the rule that matters, and it is decided HERE
// rather than by the caller: a variant with history is deactivated, always, because deleting one
// that appears on a two-year-old invoice breaks reprinting and every historical report.
type Plan struct {
	// Create are combinations that should exist and do not.
	Create []VariantSpec
	// Reactivate are variants that exist, are retired, and are wanted again — which is what
	// happens when a business brings back a colour it dropped. Reusing the row keeps its
	// history, its barcodes, and its SKU.
	Reactivate []Variant
	// Deactivate are variants that are no longer wanted but have history.
	Deactivate []Variant
	// Delete are variants that are no longer wanted and never appeared on anything.
	Delete []Variant
	// Unchanged is the count of variants the plan leaves alone, so a preview can say
	// "12 unchanged" rather than showing an empty screen that looks like a failure.
	Unchanged int
}

// IsEmpty reports whether applying the plan would change nothing.
func (p Plan) IsEmpty() bool {
	return len(p.Create) == 0 && len(p.Reactivate) == 0 &&
		len(p.Deactivate) == 0 && len(p.Delete) == 0
}

// VariantSpec is one variant the plan would create.
type VariantSpec struct {
	SKU         string
	Combination string
	Assignments []Assignment
}

// GeneratePlan works out what a product's variants should be.
//
// # The algorithm (§A.4)
//
//  1. Cartesian product of the variant-defining attributes' offered values.
//  2. Remove anything an exclusion matches.
//  3. Diff against what exists, by combination.
//
// Deterministic in every respect: the dimensions are processed in a fixed order and the
// combination string is sorted, so two runs over the same data produce the same SKUs. A SKU that
// changed between runs would orphan every label already printed.
func GeneratePlan(
	product Product, dimensions []Dimension, exclusions []Exclusion, existing []Variant,
) (Plan, error) {
	if len(dimensions) == 0 {
		// A product with no variant-defining attributes has exactly its default variant, and
		// generating is meaningless rather than harmless — an empty plan would read as "nothing
		// to do" when the real answer is "you have not chosen a dimension yet".
		return Plan{}, errs.Validation(CodeNoDimensions,
			"this product has no variant-defining attributes").WithParam("product", product.Code)
	}

	ordered := make([]Dimension, len(dimensions))
	copy(ordered, dimensions)
	sort.Slice(ordered, func(i, j int) bool {
		return strings.ToUpper(ordered[i].AttributeCode) < strings.ToUpper(ordered[j].AttributeCode)
	})

	total := 1
	for _, dimension := range ordered {
		if len(dimension.Values) == 0 {
			// One dimension with no values makes the Cartesian product empty, which would
			// present as "delete every variant you have" — a catastrophic reading of what is
			// almost certainly a half-finished configuration.
			return Plan{}, errs.Validation(CodeNoValuesOffered,
				"this attribute defines variants but the product offers none of its values").
				WithParam("attribute", dimension.AttributeCode)
		}
		total *= len(dimension.Values)
		if total > maxGeneratedVariants {
			return Plan{}, errs.Validation(CodeTooManyVariants,
				"this combination of attributes would create too many variants").
				WithParam("product", product.Code).
				WithParam("limit", itoa(maxGeneratedVariants))
		}
	}

	wanted := make(map[string]VariantSpec, total)
	for _, assignments := range cartesian(ordered) {
		combination := Combination(assignments)
		if excludedBy(combination, exclusions) {
			continue
		}
		wanted[combination] = VariantSpec{
			SKU:         SKUFor(product.Code, assignments),
			Combination: combination,
			Assignments: assignments,
		}
	}

	plan := Plan{}
	have := make(map[string]bool, len(existing))

	for _, variant := range existing {
		// The default variant of a product that has just gained its first dimension has an empty
		// combination and is not in `wanted`. It is never deleted or deactivated: it is the row
		// §A.1 guarantees, and 3.2's RequireDeletable refuses it anyway. It is left alone, and
		// the caller adopts it as the first real combination if it wants to.
		if variant.IsDefault && variant.Combination == "" {
			plan.Unchanged++
			continue
		}
		have[variant.Combination] = true

		if _, keep := wanted[variant.Combination]; keep {
			if variant.IsActive {
				plan.Unchanged++
			} else {
				plan.Reactivate = append(plan.Reactivate, variant)
			}
			continue
		}

		// No longer wanted. History decides which of the two happens, and the domain decides
		// that — not the caller, and not the screen.
		if variant.HasHistory {
			if variant.IsActive {
				plan.Deactivate = append(plan.Deactivate, variant)
			} else {
				plan.Unchanged++
			}
			continue
		}
		plan.Delete = append(plan.Delete, variant)
	}

	for combination, spec := range wanted {
		if !have[combination] {
			plan.Create = append(plan.Create, spec)
		}
	}

	// Sorted, so a preview lists variants in a stable order and two runs of the same plan read
	// identically. A preview whose rows shuffle between refreshes is one nobody trusts.
	sort.Slice(plan.Create, func(i, j int) bool { return plan.Create[i].SKU < plan.Create[j].SKU })
	sortVariants(plan.Reactivate)
	sortVariants(plan.Deactivate)
	sortVariants(plan.Delete)

	return plan, nil
}

func sortVariants(list []Variant) {
	sort.Slice(list, func(i, j int) bool { return list[i].SKU < list[j].SKU })
}

// cartesian expands the dimensions into every combination of one value from each.
func cartesian(dimensions []Dimension) [][]Assignment {
	out := [][]Assignment{{}}
	for _, dimension := range dimensions {
		next := make([][]Assignment, 0, len(out)*len(dimension.Values))
		for _, prefix := range out {
			for _, value := range dimension.Values {
				combined := make([]Assignment, len(prefix), len(prefix)+1)
				copy(combined, prefix)
				next = append(next, append(combined, Assignment{
					AttributeID:   dimension.AttributeID,
					AttributeCode: dimension.AttributeCode,
					ValueID:       value.ID,
					ValueCode:     value.Code,
				}))
			}
		}
		out = next
	}
	return out
}

// excludedBy reports whether an exclusion covers a combination.
//
// An exclusion is a SUBSET test, not equality: every pair the exclusion names must appear in the
// combination, and pairs the exclusion does not mention are free. That is what lets
// 'COLOUR:RED' remove every red variant while 'COLOUR:RED|SIZE:XXL' removes exactly one.
func excludedBy(combination string, exclusions []Exclusion) bool {
	if combination == "" {
		return false
	}
	present := make(map[string]bool, 4)
	for _, pair := range strings.Split(combination, "|") {
		present[pair] = true
	}

	for _, exclusion := range exclusions {
		if exclusion.Combination == "" {
			// An empty exclusion would match every combination and wipe the product out. It
			// cannot be created — NewExclusion refuses it — and is ignored here so that a row
			// written before that rule existed cannot do the damage either.
			continue
		}
		matched := true
		for _, pair := range strings.Split(exclusion.Combination, "|") {
			if !present[pair] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// NewExclusion builds an exclusion, or refuses.
func NewExclusion(identifier id.ID, assignments []Assignment, reason string) (Exclusion, error) {
	if identifier.IsZero() {
		return Exclusion{}, errs.Validation(CodeInvalidExclusion,
			"an exclusion needs an identity")
	}
	if len(assignments) == 0 {
		// An exclusion naming nothing matches everything, which would silently empty the
		// product's catalog.
		return Exclusion{}, errs.Validation(CodeInvalidExclusion,
			"an exclusion must name at least one attribute value")
	}
	return Exclusion{
		ID: identifier, Combination: Combination(assignments), Reason: strings.TrimSpace(reason),
	}, nil
}

// ── packagings ──────────────────────────────────────────────────────────────────

// Packaging is a product-specific bundle: a box of 12 (§B.5).
//
// Not a unit of measure. A kilogram is a kilogram for every product; a box is 12 for one and 24
// for another, so modelling it as a UoM produces hundreds of near-duplicate units and a
// conversion factor that means nothing.
type Packaging struct {
	ID            id.ID
	ProductID     id.ID
	Code          string
	Name          string
	NameKey       string
	QuantityMicro int64
	IsDefault     bool
	IsActive      bool
}

// NewPackaging builds a packaging, or refuses.
func NewPackaging(
	identifier, productID id.ID, code, name string, quantityMicro int64,
) (Packaging, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	name = strings.TrimSpace(name)

	if identifier.IsZero() || productID.IsZero() {
		return Packaging{}, errs.Validation(CodeInvalidPackaging,
			"a packaging needs an identity and a product")
	}
	if code == "" || name == "" {
		return Packaging{}, errs.Validation(CodeInvalidPackaging,
			"a packaging needs a code and a name")
	}
	if quantityMicro <= 0 {
		// A box of zero would make every scan of it add nothing, and a negative one would
		// remove stock on a sale. Both are silent at the till.
		return Packaging{}, errs.Validation(CodeInvalidPackaging,
			"a packaging must hold a positive quantity").WithParam("code", code)
	}
	return Packaging{
		ID: identifier, ProductID: productID, Code: code, Name: name,
		QuantityMicro: quantityMicro, IsActive: true,
	}, nil
}

// ── barcodes ────────────────────────────────────────────────────────────────────

// Barcode points at a variant, and optionally at a packaging of it.
type Barcode struct {
	ID          id.ID
	VariantID   id.ID
	PackagingID id.ID
	Code        string
	Type        string
	IsPrimary   bool
	IsActive    bool
}

// The barcode symbologies recorded. Not validated — a supplier whose "EAN-13" has a wrong check
// digit still has to be sellable, and refusing the scan helps nobody.
const (
	BarcodeInternal = "internal"
	BarcodeEAN13    = "ean13"
	BarcodeUPCA     = "upca"
	BarcodeCode128  = "code128"
	BarcodeQR       = "qr"
)

// NewBarcode builds a barcode, or refuses.
func NewBarcode(
	identifier, variantID id.ID, code, barcodeType string,
) (Barcode, error) {
	code = strings.TrimSpace(code)

	if identifier.IsZero() || variantID.IsZero() {
		return Barcode{}, errs.Validation(CodeInvalidBarcode,
			"a barcode needs an identity and a variant")
	}
	if code == "" {
		return Barcode{}, errs.Validation(CodeInvalidBarcode, "a barcode needs a code").
			WithField("code", CodeInvalidBarcode, "required")
	}
	// Not upper-cased, unlike every other code in this module: a barcode is scanned, not typed,
	// and the scanner sends exactly what is printed. Folding case would make two distinct
	// printed codes collide, which is the one thing a barcode must never do.
	if barcodeType == "" {
		barcodeType = BarcodeInternal
	}
	return Barcode{
		ID: identifier, VariantID: variantID, Code: code, Type: barcodeType, IsActive: true,
	}, nil
}

// Scan is what a scanned barcode resolves to.
//
// §A.5: "a barcode resolves to exactly one variant, and a packaging barcode resolves to a
// quantity". Both halves are here, because a till needs both in one answer: which variant, and
// how many of it.
type Scan struct {
	VariantID     id.ID
	PackagingID   id.ID
	QuantityMicro int64
}

// Resolve turns a barcode and its optional packaging into a variant and a quantity.
//
// A barcode with no packaging is one of the stock unit. A box barcode is however many the
// packaging holds — which is what makes scanning the box at the till add 12 rather than 1.
func Resolve(barcode Barcode, packaging *Packaging) Scan {
	scan := Scan{VariantID: barcode.VariantID, QuantityMicro: 1_000_000}
	if packaging != nil {
		scan.PackagingID = packaging.ID
		scan.QuantityMicro = packaging.QuantityMicro
	}
	return scan
}
