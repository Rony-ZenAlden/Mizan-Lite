package bindings_test

import (
	"context"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/bindings"
	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/id"
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
