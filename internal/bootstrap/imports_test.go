package bootstrap_test

import (
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/imports"
	"github.com/mizan-erp/mizan/internal/modules/partner"
)

const productsCSV = `code,name,unit,type
OIL-1,Olive oil 1L,PCS,goods
RICE-5,Rice 5kg,PCS,goods
SOAP,Hand soap,PCS,goods
`

// TestAnImportGoesThroughTheServiceAndGetsEverythingItDoes
//
// # DoD criterion 8, and the reason the whole design is what it is
//
// An importer with its own INSERT would put rows in the table. This one calls `CreateProduct`, so
// every imported product gets what a product created on a screen gets: a refused duplicate code, a
// resolved unit, a DEFAULT VARIANT, and an audit entry — inside one transaction.
//
// The default variant is the assertion that proves it. Nothing in the CSV mentions a variant, and
// no importer writing INSERTs would create one, so a product with a variant is a product that went
// through the service.
func TestAnImportGoesThroughTheServiceAndGetsEverythingItDoes(t *testing.T) {
	app, ctx, companyID := traded(t)
	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}

	report, err := app.Imports.Import(
		ctx, companyID, imports.Products, []byte(productsCSV), false)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.Succeeded != 3 || report.Failed != 0 {
		t.Fatalf("imported %d of %d, %d failed: %+v",
			report.Succeeded, report.Total, report.Failed, report.Rows)
	}

	products, err := app.Catalog.Products(ctx, companyID)
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	if len(products) != 3 {
		t.Fatalf("%d products in the catalogue, want 3", len(products))
	}

	for _, product := range products {
		variants, variantErr := app.Catalog.Variants(ctx, product.ID)
		if variantErr != nil {
			t.Fatalf("Variants: %v", variantErr)
		}
		// Nothing in the file mentions a variant. A product that has one went through the
		// service; a product that does not was inserted around it.
		if len(variants) == 0 {
			t.Errorf("%q has no default variant — it was written to the table rather than "+
				"created through the service", product.Code)
		}
	}

	// And the audit trail has it, which an INSERT would also have skipped.
	entries, err := app.Audit.Entries(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	var created int
	for _, entry := range entries {
		if strings.Contains(entry.Action, "product") && strings.Contains(entry.Action, "created") {
			created++
		}
	}
	if created < 3 {
		t.Errorf("%d product-creation audit entries for 3 imported products", created)
	}
}

// TestAFailingRowDoesNotDiscardTheRowsThatSucceeded
//
// A partial import that says exactly what did not go in is more useful than an all-or-nothing one
// that says only "row 3 was bad". A shop with a 4,000-row file has better things to do than find
// the bad row by bisection.
func TestAFailingRowDoesNotDiscardTheRowsThatSucceeded(t *testing.T) {
	app, ctx, companyID := traded(t)
	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}

	// The third row repeats the first's code, which the SERVICE refuses — not this package.
	const withADuplicate = `code,name,unit
OIL-1,Olive oil 1L,PCS
RICE-5,Rice 5kg,PCS
OIL-1,Olive oil again,PCS
SOAP,Hand soap,PCS
`

	report, err := app.Imports.Import(
		ctx, companyID, imports.Products, []byte(withADuplicate), false)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if report.Succeeded != 3 || report.Failed != 1 {
		t.Fatalf("succeeded %d, failed %d — want 3 and 1", report.Succeeded, report.Failed)
	}

	// Found by SEARCHING rather than indexing. The first version of this test indexed Rows[3]
	// and got the row after the failure — the same off-by-one the `Line` field exists to prevent,
	// made in the test that checks it.
	var failure imports.RowOutcome
	for _, row := range report.Rows {
		if !row.OK {
			failure = row
		}
	}
	if failure.Line == 0 {
		t.Fatalf("no row failed: %+v", report.Rows)
	}
	if failure.Line != 4 {
		t.Errorf("the failure is reported at line %d, want 4 — the line the user sees in their "+
			"spreadsheet", failure.Line)
	}
	if failure.Key != "OIL-1" {
		t.Errorf("the failure names %q", failure.Key)
	}
	if failure.Message == "" {
		t.Error("the failure has no message, so the user cannot act on it")
	}

	// And the good rows are IN. The row after the failure especially: an importer that stopped at
	// the first error would have left SOAP out.
	products, err := app.Catalog.Products(ctx, companyID)
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	if len(products) != 3 {
		t.Errorf("%d products, want 3 — the rows after the failure were discarded", len(products))
	}
}

// TestADryRunWritesNothing
//
// The screen's default posture. Importing four thousand products into a live shop is not a thing
// to do twice.
func TestADryRunWritesNothing(t *testing.T) {
	app, ctx, companyID := traded(t)
	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}

	report, err := app.Imports.Import(
		ctx, companyID, imports.Products, []byte(productsCSV), true)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !report.DryRun {
		t.Error("a dry run does not say it was one")
	}
	if report.Total != 3 {
		t.Errorf("a dry run read %d rows, want 3", report.Total)
	}

	products, err := app.Catalog.Products(ctx, companyID)
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	if len(products) != 0 {
		t.Errorf("a dry run created %d products", len(products))
	}
}

// TestPartnersImportThroughTheirOwnService
func TestPartnersImportThroughTheirOwnService(t *testing.T) {
	app, ctx, companyID := traded(t)

	const partnersCSV = `code,name,phone,supplier,payment_terms_days
CUST-1,Ahmad Trading,0555123456,,30
SUPP-1,Acme Supplies,0555999888,yes,60
`

	report, err := app.Imports.Import(
		ctx, companyID, imports.Partners, []byte(partnersCSV), false)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if report.Succeeded != 2 {
		t.Fatalf("imported %d of 2: %+v", report.Succeeded, report.Rows)
	}

	partners, err := app.Partner.Partners(ctx, companyID, partner.Filter{})
	if err != nil {
		t.Fatalf("Partners: %v", err)
	}
	if len(partners) != 2 {
		t.Fatalf("%d partners, want 2", len(partners))
	}

	byCode := map[string]bool{}
	for _, p := range partners {
		byCode[p.Code] = p.IsSupplier
	}
	if byCode["SUPP-1"] != true {
		t.Error("the supplier column was not read")
	}
	// A row that says nothing about either is a CUSTOMER, which is what most rows in most files
	// are. A partner who is neither is a row nobody can use.
	if byCode["CUST-1"] != false {
		t.Error("a row with no supplier flag was imported as a supplier")
	}
}

// TestAFileThatIsNotWhatItClaimsIsRefusedBeforeAnythingIsWritten
func TestAFileThatIsNotWhatItClaimsIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	app, ctx, companyID := traded(t)

	for _, test := range []struct{ name, content string }{
		{"no code column", "name,unit\nOlive oil,PCS\n"},
		{"header only", "code,name\n"},
		{"empty", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := app.Imports.Import(
				ctx, companyID, imports.Products, []byte(test.content), false)
			if err == nil {
				t.Fatal("the file was accepted")
			}
		})
	}

	products, err := app.Catalog.Products(ctx, companyID)
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	if len(products) != 0 {
		t.Errorf("%d products were created by files that were refused", len(products))
	}
}
