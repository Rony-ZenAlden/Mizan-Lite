package api_test

import (
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
)

// TestACostPriceAndItsMarginThroughTheBindings is the owner's request of 2026-09-16 at the boundary the form calls: a cost
// price on the product, a sell price worked out from a margin, and the margin read back beside them.
func TestACostPriceAndItsMarginThroughTheBindings(t *testing.T) {
	set, _ := attached(t, litetest.Logger())
	firstRun(t, set)

	// Created with a cost and a margin: the price is computed, not typed. $2.00 + 25% = $2.50.
	made := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "سمنة", UnitCode: "kg", PriceCurrency: "USD",
		Price: "0.01", CostPrice: "2.00", MarginPercent: "25"})
	if !made.OK {
		t.Fatal(made.Error)
	}
	if made.Data.Price != "2.50" || made.Data.CostPrice != "2.00" || made.Data.MarginAmount != "0.50" || made.Data.MarginPercent != "25.0" {
		t.Fatalf("created with a margin = %+v", made.Data)
	}

	// The price typed instead, and the margin follows from it: $3.00 on a $2.00 cost is 50%.
	repriced := set.Catalog.SetPrice(api.SetPriceInput{ID: made.Data.ID, RowVersion: made.Data.RowVersion,
		PriceCurrency: "USD", Price: "3.00"})
	if !repriced.OK || repriced.Data.MarginPercent != "50.0" || repriced.Data.MarginAmount != "1.00" {
		t.Fatalf("the margin follows a typed price = %+v", repriced.Data)
	}

	// A margin in money, the other direction: $2.00 + $0.20 = $2.20.
	byAmount := set.Catalog.SetPrice(api.SetPriceInput{ID: made.Data.ID, RowVersion: repriced.Data.RowVersion,
		PriceCurrency: "USD", Price: "3.00", MarginAmount: "0.20"})
	if !byAmount.OK || byAmount.Data.Price != "2.20" {
		t.Fatalf("a margin in money = %+v", byAmount.Data)
	}

	// Both at once is refused: a margin is one or the other.
	both := set.Catalog.SetPrice(api.SetPriceInput{ID: made.Data.ID, RowVersion: byAmount.Data.RowVersion,
		PriceCurrency: "USD", Price: "3.00", MarginPercent: "10", MarginAmount: "0.20"})
	if codeOf(t, both) != catalogdomain.CodeMarginInvalid {
		t.Fatalf("a percentage and an amount together = %+v", both)
	}

	// Taken off again: the product stops claiming a cost, and its margin goes with it.
	cleared := set.Catalog.SetPrice(api.SetPriceInput{ID: made.Data.ID, RowVersion: byAmount.Data.RowVersion,
		PriceCurrency: "USD", Price: "3.00", CostPrice: "-"})
	if !cleared.OK || cleared.Data.CostPrice != "" || cleared.Data.MarginPercent != "" {
		t.Fatalf("a cleared cost = %+v", cleared.Data)
	}

	// A product with no cost has no margin, and says so with an empty string rather than a nought.
	plain := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "خبز", UnitCode: "piece", PriceCurrency: "SYP", Price: "3000"})
	if !plain.OK || plain.Data.CostPrice != "" || plain.Data.MarginAmount != "" || plain.Data.MarginPercent != "" {
		t.Fatalf("a product with no cost = %+v", plain.Data)
	}

	// Selling below cost is shown, not refused: the shop needs to see the minus.
	below := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "سكر", UnitCode: "kg", PriceCurrency: "USD",
		Price: "1.50", CostPrice: "2.00"})
	if !below.OK || below.Data.MarginAmount != "-0.50" || below.Data.MarginPercent != "-25.0" {
		t.Fatalf("a product sold below cost = %+v", below.Data)
	}
}

// TestASuppliersDiscountOnTheCostThroughTheBindings (0.10.0): the product form's discount comes off the cost typed with it,
// in the new pound as in the old, and never off a stored cost that may carry one already.
func TestASuppliersDiscountOnTheCostThroughTheBindings(t *testing.T) {
	set, _ := attached(t, litetest.Logger())
	firstRun(t, set)
	setDisplay(t, set, "new")

	// 200 new pounds less 10% is 180; a 25% margin on it is 225.
	made := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: "سمنة", UnitCode: "kg", PriceCurrency: "SYP",
		Price: "1", CostPrice: "200", CostDiscount: "10", MarginPercent: "25"})
	if !made.OK || made.Data.CostPrice != "180" || made.Data.Price != "225" {
		t.Fatalf("created with a supplier's discount = %+v", made)
	}
	// A discount with no cost typed beside it is refused, under the discount.
	alone := set.Catalog.SetPrice(api.SetPriceInput{ID: made.Data.ID, RowVersion: made.Data.RowVersion, PriceCurrency: "SYP",
		Price: "225", CostDiscount: "10"})
	if codeOf(t, alone) != catalogdomain.CodeCostDiscountInvalid {
		t.Fatalf("a discount off the stored cost = %+v", alone)
	}
	retyped := set.Catalog.SetPrice(api.SetPriceInput{ID: made.Data.ID, RowVersion: made.Data.RowVersion, PriceCurrency: "SYP",
		Price: "225", CostPrice: "200", CostDiscount: "5"})
	if !retyped.OK || retyped.Data.CostPrice != "190" {
		t.Fatalf("a new cost with its discount = %+v", retyped)
	}
}
