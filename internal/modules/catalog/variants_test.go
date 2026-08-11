package catalog_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	"github.com/mizan-erp/mizan/internal/modules/catalog/domain"
)

// shirtWithDimensions builds the running example: a shirt in two colours and two sizes, with its
// attributes linked and nothing generated yet.
func shirtWithDimensions(t *testing.T) (fixture, domain.Product) {
	t.Helper()
	f := seeded(t)

	for _, in := range []catalog.NewAttributeInput{
		{
			CompanyID: f.companyID, Code: "COLOUR", Name: "Colour",
			Values: []catalog.AttributeValueInput{
				{Code: "RED", Name: "Red"}, {Code: "BLUE", Name: "Blue"},
			},
		},
		{
			CompanyID: f.companyID, Code: "SIZE", Name: "Size",
			Values: []catalog.AttributeValueInput{
				{Code: "S", Name: "Small"}, {Code: "L", Name: "Large"},
			},
		},
	} {
		if _, err := f.svc.CreateAttribute(f.ctx, in); err != nil {
			t.Fatalf("CreateAttribute(%s): %v", in.Code, err)
		}
	}

	product := f.product(t, "SHIRT", "Shirt", "PCS")
	for _, code := range []string{"COLOUR", "SIZE"} {
		if err := f.svc.LinkAttribute(f.ctx, catalog.LinkAttributeInput{
			CompanyID: f.companyID, ProductCode: "SHIRT", AttributeCode: code,
			VariantDefining: true,
		}); err != nil {
			t.Fatalf("LinkAttribute(%s): %v", code, err)
		}
	}
	return f, product
}

func activeSKUs(t *testing.T, f fixture, product domain.Product) []string {
	t.Helper()
	variants, err := f.svc.Variants(f.ctx, product.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	out := make([]string, 0, len(variants))
	for _, v := range variants {
		if v.IsActive {
			out = append(out, v.SKU)
		}
	}
	return out
}

// ── planning is not applying ────────────────────────────────────────────────────

// The preview half of §2.4. Adding a dimension to a product with four sizes silently creating
// twenty SKUs is exactly the surprise this exists to avoid, so planning must touch nothing.
func TestPlanningChangesNothing(t *testing.T) {
	f, product := shirtWithDimensions(t)

	plan, err := f.svc.PlanVariants(f.ctx, f.companyID, "SHIRT")
	if err != nil {
		t.Fatalf("PlanVariants: %v", err)
	}
	if len(plan.Create) != 4 {
		t.Fatalf("%d planned, want 4", len(plan.Create))
	}

	// Still only the default variant in storage.
	variants, err := f.svc.Variants(f.ctx, product.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	if len(variants) != 1 || !variants[0].IsDefault {
		t.Errorf("planning wrote %d variants: %+v", len(variants), variants)
	}
}

func TestApplyingAPlanCreatesTheVariants(t *testing.T) {
	f, product := shirtWithDimensions(t)

	plan, err := f.svc.ApplyVariantPlan(f.ctx, f.companyID, "SHIRT")
	if err != nil {
		t.Fatalf("ApplyVariantPlan: %v", err)
	}
	if len(plan.Create) != 4 {
		t.Fatalf("%d created, want 4", len(plan.Create))
	}

	// Four generated plus the default that §A.1 guarantees.
	variants, err := f.svc.Variants(f.ctx, product.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	if len(variants) != 5 {
		t.Fatalf("%d variants, want 4 generated plus the default", len(variants))
	}

	// The default is still there and still the default: generation never touches it.
	if !variants[0].IsDefault {
		t.Error("the default variant is no longer first, or no longer the default")
	}

	// And each generated variant recorded which values it is.
	var assignments int
	if err = f.store.Reader(f.ctx).QueryRowContext(f.ctx,
		`SELECT COUNT(*) FROM product_variant_values`).Scan(&assignments); err != nil {
		t.Fatalf("counting assignments: %v", err)
	}
	if assignments != 8 { // 4 variants × 2 dimensions
		t.Errorf("%d attribute assignments recorded, want 8", assignments)
	}
}

// Pressing apply twice must be safe, so that a preview showing changes always means there are
// changes.
func TestApplyingTwiceChangesNothingTheSecondTime(t *testing.T) {
	f, product := shirtWithDimensions(t)

	if _, err := f.svc.ApplyVariantPlan(f.ctx, f.companyID, "SHIRT"); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	before := activeSKUs(t, f, product)

	second, err := f.svc.ApplyVariantPlan(f.ctx, f.companyID, "SHIRT")
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if !second.IsEmpty() {
		t.Errorf("the second apply proposed changes: %+v", second)
	}
	after := activeSKUs(t, f, product)
	if len(after) != len(before) {
		t.Errorf("%d variants after a no-op apply, want %d", len(after), len(before))
	}
}

// ── the rule that matters: history decides delete or deactivate ─────────────────

func TestAVariantThatHasBeenSoldIsDeactivatedAndOneThatHasNotIsDeleted(t *testing.T) {
	f, product := shirtWithDimensions(t)

	if _, err := f.svc.ApplyVariantPlan(f.ctx, f.companyID, "SHIRT"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	variants, err := f.svc.Variants(f.ctx, product.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	var sold domain.Variant
	for _, v := range variants {
		if v.SKU == "SHIRT-RED-L" {
			sold = v
		}
	}
	if sold.ID.IsZero() {
		t.Fatal("SHIRT-RED-L was not generated")
	}
	// Phase 5 will do this when the variant first appears on an invoice.
	if err = f.svc.MarkVariantHistory(f.ctx, sold.ID); err != nil {
		t.Fatalf("MarkVariantHistory: %v", err)
	}

	// Drop RED entirely.
	if err = f.svc.Exclude(f.ctx, catalog.ExcludeInput{
		CompanyID: f.companyID, ProductCode: "SHIRT",
		Values: map[string]string{"COLOUR": "RED"}, Reason: "discontinued",
	}); err != nil {
		t.Fatalf("Exclude: %v", err)
	}

	plan, err := f.svc.ApplyVariantPlan(f.ctx, f.companyID, "SHIRT")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(plan.Deactivate) != 1 || plan.Deactivate[0].SKU != "SHIRT-RED-L" {
		t.Errorf("deactivated %+v, want SHIRT-RED-L — the one with history", plan.Deactivate)
	}
	if len(plan.Delete) != 1 || plan.Delete[0].SKU != "SHIRT-RED-S" {
		t.Errorf("deleted %+v, want SHIRT-RED-S — the one without", plan.Delete)
	}

	// The sold variant SURVIVES, retired, so a two-year-old invoice still reprints.
	all, err := f.svc.Variants(f.ctx, product.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	found := false
	for _, v := range all {
		if v.SKU == "SHIRT-RED-L" {
			found = true
			if v.IsActive {
				t.Error("a discontinued variant is still active")
			}
		}
		if v.SKU == "SHIRT-RED-S" {
			t.Error("an unused variant survived deletion")
		}
	}
	if !found {
		t.Error("a variant that appears on documents was deleted")
	}
}

// Bringing the colour back revives the SAME row, keeping its history and its barcodes. Creating
// a new one would orphan every label already printed.
func TestBringingAColourBackRevivesTheOriginalRow(t *testing.T) {
	f, product := shirtWithDimensions(t)

	if _, err := f.svc.ApplyVariantPlan(f.ctx, f.companyID, "SHIRT"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	original := map[string]string{}
	variants, err := f.svc.Variants(f.ctx, product.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	for _, v := range variants {
		original[v.SKU] = string(v.ID)
		if v.SKU == "SHIRT-RED-L" {
			if err = f.svc.MarkVariantHistory(f.ctx, v.ID); err != nil {
				t.Fatalf("MarkVariantHistory: %v", err)
			}
		}
	}

	if err = f.svc.Exclude(f.ctx, catalog.ExcludeInput{
		CompanyID: f.companyID, ProductCode: "SHIRT",
		Values: map[string]string{"COLOUR": "RED", "SIZE": "L"},
	}); err != nil {
		t.Fatalf("Exclude: %v", err)
	}
	if _, err = f.svc.ApplyVariantPlan(f.ctx, f.companyID, "SHIRT"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Remove the exclusion — the colour is back.
	if _, err = f.store.Writer(f.ctx).ExecContext(f.ctx,
		`DELETE FROM variant_exclusions`); err != nil {
		t.Fatalf("removing the exclusion: %v", err)
	}

	plan, err := f.svc.ApplyVariantPlan(f.ctx, f.companyID, "SHIRT")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(plan.Create) != 0 {
		t.Errorf("the revived variant was re-created: %+v", plan.Create)
	}
	if len(plan.Reactivate) != 1 || plan.Reactivate[0].SKU != "SHIRT-RED-L" {
		t.Fatalf("reactivated %+v, want SHIRT-RED-L", plan.Reactivate)
	}
	// The SAME row, which is what keeps its history and its barcodes.
	if string(plan.Reactivate[0].ID) != original["SHIRT-RED-L"] {
		t.Error("a new row was created instead of reviving the original")
	}
}

// ── exclusions ──────────────────────────────────────────────────────────────────

func TestAnExclusionRemovesOneCombinationFromGeneration(t *testing.T) {
	f, product := shirtWithDimensions(t)

	if err := f.svc.Exclude(f.ctx, catalog.ExcludeInput{
		CompanyID: f.companyID, ProductCode: "SHIRT",
		Values: map[string]string{"COLOUR": "RED", "SIZE": "L"},
		Reason: "not manufactured",
	}); err != nil {
		t.Fatalf("Exclude: %v", err)
	}

	if _, err := f.svc.ApplyVariantPlan(f.ctx, f.companyID, "SHIRT"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	skus := activeSKUs(t, f, product)
	for _, sku := range skus {
		if sku == "SHIRT-RED-L" {
			t.Error("the excluded combination was generated")
		}
	}
	// 4 minus 1, plus the default.
	if len(skus) != 4 {
		t.Errorf("%d active variants, want 3 generated plus the default: %v", len(skus), skus)
	}
}

// Excluding a value the product does not offer is a mistype, not a no-op. A user who watches
// nothing happen has no way to tell which.
func TestExcludingAValueTheProductDoesNotOfferIsRefused(t *testing.T) {
	f, _ := shirtWithDimensions(t)

	err := f.svc.Exclude(f.ctx, catalog.ExcludeInput{
		CompanyID: f.companyID, ProductCode: "SHIRT",
		Values: map[string]string{"COLOUR": "TEAL"},
	})
	if err == nil {
		t.Fatal("an exclusion naming a value the product does not offer was accepted")
	}
	if code := errs.CodeOf(err); code != catalog.CodeUnknownValue {
		t.Errorf("code = %q, want %q", code, catalog.CodeUnknownValue)
	}
}

// ── §A.5: a scan resolves to one variant and a quantity ─────────────────────────

func TestScanningAUnitBarcodeResolvesToOne(t *testing.T) {
	f := seeded(t)
	product := f.product(t, "CEMENT", "Cement", "PCS")
	variants, err := f.svc.Variants(f.ctx, product.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}

	if _, err = f.svc.CreateBarcode(f.ctx, catalog.NewBarcodeInput{
		VariantID: variants[0].ID, ProductID: product.ID,
		Code: "5901234123457", Type: domain.BarcodeEAN13, IsPrimary: true,
	}); err != nil {
		t.Fatalf("CreateBarcode: %v", err)
	}

	scan, err := f.svc.ScanBarcode(f.ctx, "5901234123457")
	if err != nil {
		t.Fatalf("ScanBarcode: %v", err)
	}
	if scan.VariantID != variants[0].ID {
		t.Error("the scan resolved to the wrong variant")
	}
	if scan.QuantityMicro != 1_000_000 {
		t.Errorf("quantity = %d, want one", scan.QuantityMicro)
	}
}

// Scanning the box at the till adds 12, not 1 — the whole reason a barcode may name a packaging.
func TestScanningABoxBarcodeResolvesToTwelve(t *testing.T) {
	f := seeded(t)
	product := f.product(t, "CEMENT", "Cement", "PCS")
	variants, err := f.svc.Variants(f.ctx, product.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}

	if _, err = f.svc.CreatePackaging(f.ctx, catalog.NewPackagingInput{
		CompanyID: f.companyID, ProductCode: "CEMENT",
		Code: "BOX", Name: "Box of 12", QuantityMicro: 12_000_000,
	}); err != nil {
		t.Fatalf("CreatePackaging: %v", err)
	}
	if _, err = f.svc.CreateBarcode(f.ctx, catalog.NewBarcodeInput{
		VariantID: variants[0].ID, ProductID: product.ID, PackagingCode: "BOX",
		Code: "5901234123464", Type: domain.BarcodeEAN13,
	}); err != nil {
		t.Fatalf("CreateBarcode: %v", err)
	}

	scan, err := f.svc.ScanBarcode(f.ctx, "5901234123464")
	if err != nil {
		t.Fatalf("ScanBarcode: %v", err)
	}
	if scan.VariantID != variants[0].ID {
		t.Error("the box scan resolved to the wrong variant")
	}
	if scan.QuantityMicro != 12_000_000 {
		t.Errorf("quantity = %d, want 12000000 — the box holds twelve", scan.QuantityMicro)
	}
}

// A till that scans a barcode and finds two variants has no correct behaviour available to it,
// so the same code cannot exist twice — across products, not merely within one.
func TestABarcodeResolvesToExactlyOneVariant(t *testing.T) {
	f := seeded(t)
	cement := f.product(t, "CEMENT", "Cement", "PCS")
	sand := f.product(t, "SAND", "Sand", "KG")

	cementVariants, err := f.svc.Variants(f.ctx, cement.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	sandVariants, err := f.svc.Variants(f.ctx, sand.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}

	if _, err = f.svc.CreateBarcode(f.ctx, catalog.NewBarcodeInput{
		VariantID: cementVariants[0].ID, ProductID: cement.ID, Code: "1234567890128",
	}); err != nil {
		t.Fatalf("CreateBarcode: %v", err)
	}

	// The same code on a different product, in the same company. Refused.
	_, err = f.svc.CreateBarcode(f.ctx, catalog.NewBarcodeInput{
		VariantID: sandVariants[0].ID, ProductID: sand.ID, Code: "1234567890128",
	})
	if err == nil {
		t.Fatal("one barcode was attached to two variants")
	}
	if code := errs.CodeOf(err); code != domain.CodeBarcodeInUse {
		t.Errorf("code = %q, want %q", code, domain.CodeBarcodeInUse)
	}

	// And the scan still answers with the original.
	scan, err := f.svc.ScanBarcode(f.ctx, "1234567890128")
	if err != nil {
		t.Fatalf("ScanBarcode: %v", err)
	}
	if scan.VariantID != cementVariants[0].ID {
		t.Error("the scan resolved to the refused variant")
	}
}

// The uniqueness that matters is the SCHEMA's, not the service's.
//
// This test exists because the mutation drill for the index PASSED: CreateBarcode looks the code
// up and refuses a duplicate before the index is ever consulted, so scoping the index per
// variant changed nothing any test could see. But the service guard only protects writes that go
// through the service, and the comment on ux_barcodes_code claims more than that — it claims a
// bad import cannot produce two. So this writes straight to the table, the way
// TestAProductCannotHaveTwoDefaultVariants does, and pins the constraint itself.
func TestTheDatabaseRefusesADuplicateBarcodeEvenWithoutTheService(t *testing.T) {
	f := seeded(t)
	cement := f.product(t, "CEMENT", "Cement", "PCS")
	sand := f.product(t, "SAND", "Sand", "KG")

	cementVariants, err := f.svc.Variants(f.ctx, cement.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	sandVariants, err := f.svc.Variants(f.ctx, sand.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}

	const insert = `
		INSERT INTO barcodes (
			id, variant_id, packaging_id, code, barcode_type, is_primary, is_active,
			row_version, created_at, updated_at
		) VALUES (?, ?, NULL, '1234567890128', 'ean13', 0, 1, 1, '2026-01-01', '2026-01-01')`

	if _, err = f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"b1", string(cementVariants[0].ID)); err != nil {
		t.Fatalf("inserting the first barcode: %v", err)
	}
	// The same printed code on a different product. A till scanning it would have two answers
	// and no correct behaviour available to it.
	if _, err = f.store.Writer(f.ctx).ExecContext(f.ctx, insert,
		"b2", string(sandVariants[0].ID)); err == nil {
		t.Fatal("the database accepted one barcode on two variants")
	}
}

// A code taken out of service and then scanned should stop the sale, not quietly sell last
// year's item.
func TestARetiredBarcodeIsNotRecognised(t *testing.T) {
	f := seeded(t)
	product := f.product(t, "CEMENT", "Cement", "PCS")
	variants, err := f.svc.Variants(f.ctx, product.ID)
	if err != nil {
		t.Fatalf("Variants: %v", err)
	}
	if _, err = f.svc.CreateBarcode(f.ctx, catalog.NewBarcodeInput{
		VariantID: variants[0].ID, ProductID: product.ID, Code: "OLD-CODE",
	}); err != nil {
		t.Fatalf("CreateBarcode: %v", err)
	}

	if _, err = f.store.Writer(f.ctx).ExecContext(f.ctx,
		`UPDATE barcodes SET is_active = 0 WHERE code = 'OLD-CODE'`); err != nil {
		t.Fatalf("retiring the barcode: %v", err)
	}

	if _, err = f.svc.ScanBarcode(f.ctx, "OLD-CODE"); err == nil {
		t.Fatal("a retired barcode still sold an item")
	} else if code := errs.CodeOf(err); code != catalog.CodeUnknownBarcode {
		t.Errorf("code = %q, want %q", code, catalog.CodeUnknownBarcode)
	}
}

func TestAnUnknownBarcodeIsRefusedByName(t *testing.T) {
	f := seeded(t)

	_, err := f.svc.ScanBarcode(f.ctx, "NOT-A-CODE")
	if err == nil {
		t.Fatal("an unknown barcode resolved to something")
	}
	typed, _ := errs.AsError(err)
	if typed.Params["code"] != "NOT-A-CODE" {
		t.Errorf("params = %v, want the code that was scanned", typed.Params)
	}
}

// ── audit ───────────────────────────────────────────────────────────────────────

func TestGeneratingVariantsIsAuditedOnceWithItsCounts(t *testing.T) {
	f, _ := shirtWithDimensions(t)

	if _, err := f.svc.ApplyVariantPlan(f.ctx, f.companyID, "SHIRT"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	entries, err := f.audit.Entries(f.ctx, audit.Filter{EntityType: catalog.EntityVariants})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != catalog.ActionVariantsGenerated {
		t.Fatalf("entries = %+v, want one generation entry", entries)
	}

	// A second apply changes nothing, so it records nothing — an entry per click saying "no
	// variants changed" is the noise that trains people to ignore the trail.
	if _, err = f.svc.ApplyVariantPlan(f.ctx, f.companyID, "SHIRT"); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	entries, err = f.audit.Entries(f.ctx, audit.Filter{EntityType: catalog.EntityVariants})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("%d entries after a no-op apply, want 1", len(entries))
	}
}
