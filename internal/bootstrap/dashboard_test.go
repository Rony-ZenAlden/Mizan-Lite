package bootstrap_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/bootstrap"
)

// TestEveryTileNamesItsSourceAndItsRange
//
// DoD criterion 7. A dashboard is where somebody first notices a number is wrong, and "revenue
// 4,500" with no indication of what it counts or who counted it sends them looking through five
// screens.
//
// The RANGE matters as much as the source, and for a reason a screen makes concrete: stock value
// is a position as at now, while sales is a figure over a period. Rendering them side by side
// without saying which is which is how "stock value 40,000" gets read as this month's purchases.
func TestEveryTileNamesItsSourceAndItsRange(t *testing.T) {
	app, ctx, companyID := traded(t)

	board, err := app.Dashboard(ctx, companyID, "2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if len(board.Tiles) == 0 {
		t.Fatal("the dashboard has no tiles")
	}
	if board.From != "2026-08-01" || board.To != "2026-08-31" {
		t.Errorf("the board reports %q..%q, not the range asked for", board.From, board.To)
	}

	periodic, asAt := 0, 0
	for _, tile := range board.Tiles {
		if tile.Key == "" {
			t.Error("a tile has no key, so a screen cannot render or translate it")
		}
		if tile.Source == "" {
			t.Errorf("tile %q names no source", tile.Key)
		}
		if tile.Kind != "money" && tile.Kind != "count" {
			t.Errorf("tile %q has kind %q, which no screen knows how to render",
				tile.Key, tile.Kind)
		}
		// The key is prefixed with its module, so a screen routes on it without a lookup table.
		if !strings.HasPrefix(tile.Key, tile.Source+".") {
			t.Errorf("tile %q is not prefixed with its source %q", tile.Key, tile.Source)
		}
		if tile.Periodic {
			periodic++
		} else {
			asAt++
		}
	}

	// BOTH kinds are present, which is what makes the distinction load-bearing rather than a
	// field nobody sets differently.
	if periodic == 0 || asAt == 0 {
		t.Errorf("%d periodic tiles and %d as-at tiles — the distinction is not exercised, so "+
			"nothing here proves a screen can tell them apart", periodic, asAt)
	}
}

// TestTheDashboardReportsTheSameFiguresTheReportsDo
//
// One company, one truth. A dashboard that computed its own version of revenue would be a second
// implementation, and a shopkeeper comparing the home screen with the sales report would find two
// answers and no rule for choosing.
func TestTheDashboardReportsTheSameFiguresTheReportsDo(t *testing.T) {
	app, ctx, companyID := traded(t)

	board, err := app.Dashboard(ctx, companyID, "2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}

	analysis, err := app.Sales.SalesByPeriod(ctx, companyID, "2026-08-01", "2026-08-31", "day")
	if err != nil {
		t.Fatalf("SalesByPeriod: %v", err)
	}
	valuation, err := app.Inventory.Valuation(ctx, companyID)
	if err != nil {
		t.Fatalf("Valuation: %v", err)
	}

	byKey := make(map[string]bootstrap.Tile, len(board.Tiles))
	for _, tile := range board.Tiles {
		byKey[tile.Key] = tile
	}
	if byKey["sales.revenue"].AmountMinor != analysis.Total.RevenueMinor {
		t.Errorf("the dashboard says revenue is %d and the report says %d",
			byKey["sales.revenue"].AmountMinor, analysis.Total.RevenueMinor)
	}
	if byKey["inventory.stock_value"].AmountMinor != valuation.TotalMinor {
		t.Errorf("the dashboard says stock is worth %d and the valuation says %d",
			byKey["inventory.stock_value"].AmountMinor, valuation.TotalMinor)
	}
}

// TestOneBrokenTileDoesNotEmptyTheDashboard
//
// A home screen that renders nothing because one query broke fails entirely for a reason nobody
// can see. The tile is marked and the rest is shown — the same choice `search.Response.Failed`
// makes.
//
// The break is a range no module can answer, which every source rejects for the same reason and
// which needs no fake: `checkRange` refuses a backwards range in both sales and purchasing.
func TestOneBrokenTileDoesNotEmptyTheDashboard(t *testing.T) {
	app, ctx, companyID := traded(t)

	board, err := app.Dashboard(ctx, companyID, "2026-08-31", "2026-08-01")
	if err != nil {
		t.Fatalf("Dashboard with a backwards range: %v", err)
	}

	var failed, rendered int
	for _, tile := range board.Tiles {
		if tile.Failed {
			failed++
		} else {
			rendered++
		}
	}
	if failed == 0 {
		t.Error("a range no module can answer produced no failed tiles, so a broken query is " +
			"indistinguishable from a zero")
	}
	// Stock value does not depend on the range, so it still answers — which is the whole point
	// of marking tiles rather than failing the board.
	if rendered == 0 {
		t.Error("one broken source emptied the whole dashboard")
	}
}

// TestTheDashboardDoesNoCrossModuleArithmetic
//
// # The rule this step turns on, checked structurally
//
// Phase 8's analysis called the dashboard an ASSEMBLY: every tile is one module's own answer, and
// nothing combines two modules' data into a third number. A tile that multiplied two modules
// together would be a REPORT, belonging to whichever module owns the multiplication — and this
// file would have quietly become the reporting module the analysis refused.
//
// A comment saying so is not a check. This reads the FILE and requires every tile's amount to be
// a single selector expression — `analysis.Total.RevenueMinor`, not `x - y`. It is a blunt rule,
// and being blunt is what makes it hold: the moment a tile needs arithmetic, this fails and
// somebody has to decide where the work belongs rather than adding one more line.
func TestTheDashboardDoesNoCrossModuleArithmetic(t *testing.T) {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "dashboard.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing dashboard.go: %v", err)
	}

	var checked int
	ast.Inspect(parsed, func(node ast.Node) bool {
		composite, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		name, ok := composite.Type.(*ast.Ident)
		if !ok || name.Name != "Tile" {
			return true
		}
		for _, element := range composite.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := pair.Key.(*ast.Ident)
			if !ok || (key.Name != "AmountMinor" && key.Name != "Count") {
				continue
			}
			checked++
			switch pair.Value.(type) {
			case *ast.SelectorExpr, *ast.BasicLit, *ast.CallExpr:
				// A field, a literal, or one call. All read a single module's answer.
			default:
				t.Errorf("tile field %s is computed rather than read (%T) — arithmetic on two "+
					"modules' figures makes this a report, and a report belongs to the module "+
					"that owns it", key.Name, pair.Value)
			}
		}
		return true
	})

	// The scan found the tiles. A parser that matched nothing would pass this test while
	// checking no tile at all — 7.6's D162 in a different shape.
	if checked < 4 {
		t.Fatalf("only %d tile amounts were inspected; the dashboard sets more than that", checked)
	}
}
