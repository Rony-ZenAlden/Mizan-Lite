package api_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/lite/api"
	catalogdomain "github.com/mizan-erp/mizan/internal/lite/catalog/domain"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	stockdomain "github.com/mizan-erp/mizan/internal/lite/stock/domain"
)

func create(t *testing.T, set *api.Set, nameAR, unit string) api.ProductDTO {
	t.Helper()
	r := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: nameAR, UnitCode: unit, PriceCurrency: "USD", Price: "1"})
	if !r.OK {
		t.Fatalf("CreateProduct(%s) = %+v", nameAR, r.Error)
	}
	return r.Data
}

func elevate(t *testing.T, set *api.Set) {
	t.Helper()
	if r := set.Owner.Elevate(api.PINInput{PIN: testPIN}); !r.OK || r.Data.ElevatedSeconds == 0 {
		t.Fatalf("Elevate = %+v", r)
	}
}

// TestLevelsCarryNoCost holds the quantity DTO free of cost fields, whatever a later edit adds (Q-L2.4).
func TestLevelsCarryNoCost(t *testing.T) {
	typ := reflect.TypeFor[api.StockLevelDTO]()
	for i := range typ.NumField() {
		f := typ.Field(i)
		name := strings.ToLower(f.Name + f.Tag.Get("json"))
		for _, forbidden := range []string{"cost", "value", "price", "avg", "average"} {
			if strings.Contains(name, forbidden) {
				t.Errorf("StockLevelDTO.%s carries %s to every screen", f.Name, forbidden)
			}
		}
	}
	if typ.NumField() != 2 {
		t.Fatalf("StockLevelDTO has %d fields; a new one must be checked against Q-L2.4 and this test updated", typ.NumField())
	}
}

func TestStockThroughTheBindings(t *testing.T) {
	set, _ := attached(t, litetest.Logger())
	firstRun(t, set)
	bulgur := create(t, set, "برغل", "kg")

	opening := set.Stock.Opening(api.ReceiveInput{ProductID: bulgur.ID, Quantity: "40", Cost: "48", Currency: "USD"})
	if !opening.OK || opening.Data != (api.StockLevelDTO{ProductID: bulgur.ID, OnHand: "40.000"}) {
		t.Fatalf("Opening = %+v", opening)
	}
	received := set.Stock.Receive(api.ReceiveInput{ProductID: bulgur.ID, Quantity: "٢٥", CostMode: "total", Cost: "450000",
		Currency: "SYP", Rate: "15000", Note: "أبو خليل"})
	if !received.OK || received.Data.OnHand != "65.000" {
		t.Fatalf("Receive = %+v", received)
	}
	if noRate := set.Stock.Receive(api.ReceiveInput{ProductID: bulgur.ID, Quantity: "1", Cost: "18000", Currency: "SYP"}); codeOf(t, noRate) != stockdomain.CodeRateRequired {
		t.Fatal("a pound receipt without its rate was accepted")
	}
	if levels := set.Stock.Levels(); !levels.OK || len(levels.Data) != 1 || levels.Data[0].OnHand != "65.000" {
		t.Fatalf("Levels = %+v", levels)
	}

	// Without owner mode, since 2026-09-16: the costs are on screen and the owner's reads answer. The shop asked for its
	// own figures without a PIN (owner.ReservedActs), so the boundary that used to strip them no longer does.
	if v := set.Stock.Valuation(); !v.OK || v.Data.Total != "78.00" {
		t.Fatalf("Valuation without owner mode = %+v", v)
	}
	if v := set.Stock.Verify(); !v.OK || len(v.Data) != 0 {
		t.Fatalf("Verify without owner mode = %+v", v)
	}
	history := set.Stock.Movements(api.MovementsQueryDTO{ProductID: bulgur.ID, Limit: 10})
	if !history.OK || !history.Data.CostsVisible || len(history.Data.Movements) != 2 {
		t.Fatalf("Movements = %+v", history)
	}
	newest := history.Data.Movements[0]
	if newest.UnitCost != "1.20" || newest.EnteredUnitCost != "18000" || newest.EnteredCurrency != "SYP" {
		t.Fatalf("costs were withheld without owner mode: %+v", newest)
	}
	if newest.Kind != "receipt" || newest.Quantity != "25.000" || newest.Note != "أبو خليل" || history.Data.ReversibleID != newest.ID {
		t.Fatalf("newest = %+v, reversible %s", newest, history.Data.ReversibleID)
	}
	lower := api.CountInput{ProductID: bulgur.ID, Counted: "63.5"}

	elevate(t, set)
	history = set.Stock.Movements(api.MovementsQueryDTO{ProductID: bulgur.ID, Limit: 10})
	newest = history.Data.Movements[0]
	if !history.Data.CostsVisible || newest.UnitCost != "1.20" || newest.EnteredCurrency != "SYP" || newest.EnteredUnitCost != "18000" ||
		newest.Rate != "15000" || newest.AverageCostBefore != "1.20" || newest.AverageCostAfter != "1.20" {
		t.Fatalf("owner-mode movement = %+v", newest)
	}
	valuation := set.Stock.Valuation()
	if !valuation.OK || valuation.Data.Total != "78.00" || valuation.Data.Lines[0] != (api.ValuationLineDTO{ProductID: bulgur.ID, OnHand: "65.000", AverageCost: "1.20", Value: "78.00", ValueLocal: "1170000"}) {
		t.Fatalf("Valuation = %+v", valuation)
	}
	if r := set.Stock.Count(lower); !r.OK || r.Data.OnHand != "63.500" {
		t.Fatalf("Count = %+v", r)
	}
	if r := set.Stock.ReverseReceipt(api.ReverseReceiptInput{MovementID: newest.ID}); codeOf(t, r) != stockdomain.CodeNotNewest {
		t.Fatal("a receipt with a later count was reversed")
	}
	if r := set.Stock.Adjust(api.AdjustInput{ProductID: bulgur.ID, Direction: "out", Quantity: "0.5", Reason: "gift", Note: "عينة"}); !r.OK || r.Data.OnHand != "63.000" {
		t.Fatalf("Adjust = %+v", r)
	}
	if r := set.Stock.CorrectCost(api.CorrectCostInput{ProductID: bulgur.ID, AverageCost: "1.25", Note: "فاتورة صحيحة"}); !r.OK || r.Data.OnHand != "63.000" {
		t.Fatalf("CorrectCost = %+v", r)
	}
	if r := set.Stock.Verify(); !r.OK || len(r.Data) != 0 {
		t.Fatalf("Verify = %+v", r)
	}
	if r := set.Stock.ReverseReceipt(api.ReverseReceiptInput{MovementID: "nonsense"}); codeOf(t, r) != stockdomain.CodeMovementNotFound {
		t.Fatal("an unparsable movement id")
	}
	if r := set.Stock.Movements(api.MovementsQueryDTO{ProductID: "nonsense"}); codeOf(t, r) != catalogdomain.CodeNotFound {
		t.Fatal("an unparsable product id")
	}
}

func TestPackagesThroughTheBindings(t *testing.T) {
	set, _ := attached(t, litetest.Logger())
	firstRun(t, set)
	tin := create(t, set, "تنكة زيت ١٦ لتر", "tin")
	oil := create(t, set, "زيت زيتون فرط", "l")

	linked := set.Catalog.SetPackage(api.SetPackageInput{PackageProductID: tin.ID, ContentProductID: oil.ID, ContentQuantity: "16"})
	if !linked.OK || linked.Data.PackageContentID != oil.ID || linked.Data.PackageContentQuantity != "16.000" {
		t.Fatalf("SetPackage = %+v", linked)
	}
	if listed := set.Catalog.Products(api.ProductQueryDTO{Text: "تنكة"}); !listed.OK || listed.Data[0].PackageContentQuantity != "16.000" {
		t.Fatalf("Products does not carry the link: %+v", listed)
	}
	if r := set.Catalog.SetPackage(api.SetPackageInput{PackageProductID: tin.ID, ContentProductID: ""}); codeOf(t, r) != catalogdomain.CodePackageContentRequired {
		t.Fatal("a link to no product")
	}

	if r := set.Stock.Opening(api.ReceiveInput{ProductID: tin.ID, Quantity: "3", Cost: "285", Currency: "USD"}); !r.OK {
		t.Fatalf("Opening = %+v", r.Error)
	}
	opened := set.Stock.OpenPackage(api.OpenPackageInput{PackageProductID: tin.ID, Packages: "1"})
	if !opened.OK || len(opened.Data) != 2 || opened.Data[0] != (api.StockLevelDTO{ProductID: tin.ID, OnHand: "2"}) ||
		opened.Data[1] != (api.StockLevelDTO{ProductID: oil.ID, OnHand: "16.000"}) {
		t.Fatalf("OpenPackage = %+v", opened)
	}
	elevate(t, set)
	history := set.Stock.Movements(api.MovementsQueryDTO{ProductID: oil.ID})
	if !history.OK || history.Data.Movements[0].Kind != "content_in" || history.Data.Movements[0].UnitCost != "5.9375" ||
		history.Data.Movements[0].PairID == "" || history.Data.ReversibleID != "" {
		t.Fatalf("content history = %+v", history)
	}

	cleared := set.Catalog.ClearPackage(tin.ID)
	if !cleared.OK || cleared.Data.PackageContentID != "" || cleared.Data.PackageContentQuantity != "" {
		t.Fatalf("ClearPackage = %+v", cleared)
	}
	if r := set.Stock.OpenPackage(api.OpenPackageInput{PackageProductID: tin.ID, Packages: "1"}); codeOf(t, r) != stockdomain.CodeNotAPackage {
		t.Fatal("a product with no link was opened")
	}
}
