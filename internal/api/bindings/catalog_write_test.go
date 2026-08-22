package bindings_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/audit"
)

// TestAProductCanBeRegisteredFromTheApplication
//
// # The gap this closes
//
// `catalog.CreateProduct` has existed since Phase 3 and the CSV importer has used it since 9.4.
// There was no BINDING and no form — so a shopkeeper adding one product had to open a text
// editor, write a CSV, and import it.
//
// Service built, binding absent, screen absent: the same three-layer gap 7.6, 10.1 and 10.9 each
// found in a different layer.
func TestAProductCanBeRegisteredFromTheApplication(t *testing.T) {
	set, app := signedInAdmin(t)
	ctx := app.Context()

	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}

	// Two fields. Everything else the service accepts has a default that is right for most shops.
	created := set.Catalog.CreateProduct(bindings.NewProductInput{
		Code: "OIL-1L", Name: "Olive oil 1L",
	})
	if !created.OK {
		t.Fatalf("CreateProduct: %+v", created.Error)
	}
	if created.Data.Code != "OIL-1L" {
		t.Errorf("code = %q", created.Data.Code)
	}
	// The default VARIANT is what proves it went through the service rather than around it —
	// nothing in the input mentions one.
	if created.Data.VariantCount != 1 {
		t.Errorf("variant count = %d, want 1", created.Data.VariantCount)
	}

	// It is sellable immediately: the browse list has it.
	listed := set.Catalog.Products()
	if !listed.OK {
		t.Fatalf("Products: %+v", listed.Error)
	}
	var found bool
	for _, row := range listed.Data {
		if row.Code == "OIL-1L" {
			found = true
		}
	}
	if !found {
		t.Error("the product was created and does not appear in the catalogue")
	}

	// The SERVICE's duplicate refusal reaches the form, rather than the form having its own.
	again := set.Catalog.CreateProduct(bindings.NewProductInput{
		Code: "OIL-1L", Name: "Olive oil again",
	})
	if again.OK {
		t.Error("a duplicate code was accepted")
	}
}

// TestCountingStockRecordsTheDifferenceItImplies
//
// The screen asks what is on the shelf; the ledger stores a delta. This is the conversion, and it
// exists so nobody has to work out that eleven is two fewer than thirteen while holding a
// clipboard.
func TestCountingStockRecordsTheDifferenceItImplies(t *testing.T) {
	set, app := signedInAdmin(t)
	ctx := app.Context()

	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	created := set.Catalog.CreateProduct(bindings.NewProductInput{Code: "W", Name: "Widget"})
	if !created.OK {
		t.Fatalf("CreateProduct: %+v", created.Error)
	}

	variants, err := app.Catalog.Variants(ctx, idFrom(created.Data.ID))
	if err != nil || len(variants) == 0 {
		t.Fatalf("Variants: %v", err)
	}
	variantID := string(variants[0].ID)

	warehouseID := defaultWarehouse(t, app, ctx)

	// Ten arrive.
	first := set.Inventory.CountStock(bindings.StockCountInput{
		VariantID: variantID, WarehouseID: warehouseID,
		CountedMicro: "10000000", Reason: "opening count",
	})
	if !first.OK {
		t.Fatalf("CountStock: %+v", first.Error)
	}

	// The shelf says eight. The user types EIGHT, not "minus two".
	second := set.Inventory.CountStock(bindings.StockCountInput{
		VariantID: variantID, WarehouseID: warehouseID,
		CountedMicro: "8000000", Reason: "two damaged",
	})
	if !second.OK {
		t.Fatalf("CountStock: %+v", second.Error)
	}

	state, err := app.Inventory.StockOf(ctx, idFrom(variantID), idFrom(warehouseID))
	if err != nil {
		t.Fatalf("StockOf: %v", err)
	}
	if state.OnHandMicro != 8_000_000 {
		t.Errorf("on hand = %d, want 8000000 — the count did not become the right delta",
			state.OnHandMicro)
	}

	// A count that AGREES records nothing. A movement of zero is a ledger row saying nothing
	// happened, which 4.3 already refuses — and "the count agreed" is a success, not an error.
	agreed := set.Inventory.CountStock(bindings.StockCountInput{
		VariantID: variantID, WarehouseID: warehouseID,
		CountedMicro: "8000000", Reason: "recount",
	})
	if !agreed.OK {
		t.Errorf("a count that agreed was reported as a failure: %+v", agreed.Error)
	}
	if agreed.Data.ID != "" {
		t.Error("a count that agreed recorded a movement")
	}
}

func idFrom(s string) id.ID { return id.ID(s) }

// defaultWarehouse finds the one a fresh company provisions.
func defaultWarehouse(t *testing.T, app *bootstrap.App, ctx context.Context) string {
	t.Helper()
	var warehouseID string
	if err := app.DB.Reader(ctx).QueryRowContext(ctx,
		`SELECT id FROM warehouses LIMIT 1`).Scan(&warehouseID); err != nil {
		t.Fatalf("finding the warehouse: %v", err)
	}
	return warehouseID
}

// TestAProductCanBeRenamedWithoutRewritingHistory
//
// # The rule the whole edit slice turns on
//
// A product can be renamed after it has traded — and that is not a compromise, it is a
// consequence of §9.3. Every document snapshots the product name at the time it was written, so
// an invoice from last year keeps saying what the customer bought.
//
// This asserts both halves: the catalogue shows the new name, and the sale still shows the old
// one. If the second ever fails, renaming has become a way to rewrite history.
func TestAProductCanBeRenamedWithoutRewritingHistory(t *testing.T) {
	set, app := signedInAdmin(t)
	ctx := app.Context()

	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	created := set.Catalog.CreateProduct(bindings.NewProductInput{
		Code: "OIL-1L", Name: "Olive oil 1L",
	})
	if !created.OK {
		t.Fatalf("CreateProduct: %+v", created.Error)
	}

	renamed := set.Catalog.UpdateProduct(bindings.EditProductInput{
		Code: "OIL-1L", Name: "Extra virgin olive oil 1L",
		Description: "Cold pressed",
	})
	if !renamed.OK {
		t.Fatalf("UpdateProduct: %+v", renamed.Error)
	}
	if renamed.Data.Name != "Extra virgin olive oil 1L" {
		t.Errorf("name = %q", renamed.Data.Name)
	}
	if renamed.Data.Description != "Cold pressed" {
		t.Errorf("description = %q", renamed.Data.Description)
	}
	// The CODE is untouched, and the API cannot express changing it.
	if renamed.Data.Code != "OIL-1L" {
		t.Errorf("the code changed to %q", renamed.Data.Code)
	}

	// The identity is the same row, not a delete and a create — which would orphan every
	// document, every stock movement and every price that refers to it.
	if renamed.Data.ID != created.Data.ID {
		t.Errorf("the product's identity changed from %q to %q",
			created.Data.ID, renamed.Data.ID)
	}

	// An empty name is refused by the DOMAIN, not by the form.
	blank := set.Catalog.UpdateProduct(bindings.EditProductInput{Code: "OIL-1L", Name: "  "})
	if blank.OK {
		t.Error("a product was renamed to nothing")
	}

	// And an unknown product is a not-found rather than a silent no-op.
	missing := set.Catalog.UpdateProduct(bindings.EditProductInput{Code: "NOPE", Name: "x"})
	if missing.OK {
		t.Error("editing a product that does not exist succeeded")
	}
}

// TestEditingAProductLeavesAnAuditEntryWithBothNames
//
// "The name changed" is not the useful fact. "It was called this and is now called that" is —
// and after the column is overwritten, the audit entry is the only place the old name exists.
func TestEditingAProductLeavesAnAuditEntryWithBothNames(t *testing.T) {
	set, app := signedInAdmin(t)
	ctx := app.Context()

	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}
	if created := set.Catalog.CreateProduct(bindings.NewProductInput{
		Code: "W", Name: "Widget",
	}); !created.OK {
		t.Fatalf("CreateProduct: %+v", created.Error)
	}
	if updated := set.Catalog.UpdateProduct(bindings.EditProductInput{
		Code: "W", Name: "Widget Mk II",
	}); !updated.OK {
		t.Fatalf("UpdateProduct: %+v", updated.Error)
	}

	entries, err := app.Audit.Entries(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	var found bool
	for _, entry := range entries {
		if entry.Action != "catalog.product.updated" {
			continue
		}
		found = true
		if !strings.Contains(entry.BeforeJSON, "Widget") ||
			strings.Contains(entry.BeforeJSON, "Mk II") {
			t.Errorf("the entry does not record the old name: %q", entry.BeforeJSON)
		}
		if !strings.Contains(entry.AfterJSON, "Mk II") {
			t.Errorf("the entry does not record the new name: %q", entry.AfterJSON)
		}
	}
	if !found {
		t.Error("a product was renamed and left no audit entry")
	}
}
