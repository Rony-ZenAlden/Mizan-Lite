package bootstrap_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	"github.com/mizan-erp/mizan/internal/modules/org"
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

// seedReceivables writes two posted invoices: one untouched, one OVER-SETTLED.
//
// The over-settled one is the entire point. With only ordinary invoices, `SUM(total) - SUM(paid)`
// and "sum of each invoice's clamped outstanding" give the SAME answer, and a drill built on that
// fixture cannot tell a correct implementation from one that nets debts against overpayments.
//
// A fixture that cannot distinguish the right answer from the wrong one is not evidence.
//
// Written straight to the tables because the service posts a payment and its allocation in one
// transaction that refuses to over-allocate — which is correct, and which means the state this
// guards against can only be reached the way it happens in the wild: a repair, an import, or a
// bug that has already run.
func seedReceivables(
	t *testing.T, app *bootstrap.App, ctx context.Context, org orgIDs,
) (owedMinor int64) {
	t.Helper()

	write := func(query string, args ...any) {
		t.Helper()
		if _, err := app.DB.Writer(ctx).ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	newID := func() string {
		t.Helper()
		made, err := id.New()
		if err != nil {
			t.Fatalf("id.New: %v", err)
		}
		return made.String()
	}

	invoice := func(number string, totalMinor int64) string {
		documentID := newID()
		write(`
			INSERT INTO sales_documents (
				id, company_id, branch_id, warehouse_id, document_type, status, document_number,
				partner_name, document_date, currency_code, exchange_rate_micro,
				net_minor, tax_minor, discount_minor, total_minor, cost_minor,
				created_at, updated_at
			) VALUES (?, ?, ?, ?, 'invoice', 'posted', ?, 'Customer', '2026-08-10', 'SYP',
			          1000000, ?, 0, 0, ?, 0, '2026-08-10', '2026-08-10')`,
			documentID, string(org.company), string(org.branch), string(org.warehouse),
			number, totalMinor, totalMinor)
		return documentID
	}

	settle := func(documentID string, amountMinor int64, number string) {
		paymentID := newID()
		write(`
			INSERT INTO sales_payments (
				id, company_id, branch_id, document_number, payment_date, method,
				currency_code, exchange_rate_micro, amount_minor, status, created_at, updated_at
			) VALUES (?, ?, ?, ?, '2026-08-11', 'cash', 'SYP', 1000000, ?, 'posted',
			          '2026-08-11', '2026-08-11')`,
			paymentID, string(org.company), string(org.branch), number, amountMinor)
		write(`
			INSERT INTO sales_payment_allocations (id, payment_id, document_id, amount_minor,
			                                       created_at)
			VALUES (?, ?, ?, ?, '2026-08-11')`,
			newID(), paymentID, documentID, amountMinor)
	}

	invoice("INV-OPEN", 10_000)

	// Paid 8,000 against a 5,000 invoice. Its outstanding is ZERO, never minus 3,000.
	overSettled := invoice("INV-OVER", 5_000)
	settle(overSettled, 8_000, "PAY-OVER")

	// 10,000 owed. An implementation that nets would say 7,000.
	return 10_000
}

// orgIDs is what a provisioned company is made of, for a seed that writes rows directly.
type orgIDs struct{ company, branch, warehouse id.ID }

// provisioned sets up a company and hands back every identity a direct insert needs.
func provisioned(t *testing.T) (*bootstrap.App, context.Context, orgIDs) {
	t.Helper()
	app := boot(t)
	ctx := app.Context()

	result, err := app.Org.Provision(ctx, org.ProvisionInput{
		Company: org.CompanyInput{
			Code: "KPI", Name: "KPI Co", CountryCode: "SA", FunctionalCurrency: "SYP",
		},
		Branch:               org.LocationInput{Code: "HQ", Name: "Head Office"},
		Warehouse:            org.LocationInput{Code: "WH1", Name: "Main"},
		FiscalYearStartMonth: time.January,
		FiscalYearStartYear:  2026,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err = app.Accounting.ApplyChart(ctx, result.CompanyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}
	return app, ctx, orgIDs{
		company: result.CompanyID, branch: result.BranchID, warehouse: result.WarehouseID,
	}
}

// TestTheReceivablesTileClampsEachInvoiceRatherThanTheTotal
//
// # The trap
//
// `settle.Outstanding` clamps each document at zero, and its comment says exactly why: "a
// negative outstanding would net against another document in any sum, hiding both."
//
// The company total IS that sum, and the obvious SQL — `SUM(total) - SUM(settled)` — breaks the
// rule silently. Here an invoice over-settled by 3,000 would cancel 3,000 of real debt on a
// different invoice: the tile would read 7,000 against 10,000 genuinely owed, and look entirely
// plausible doing it.
func TestTheReceivablesTileClampsEachInvoiceRatherThanTheTotal(t *testing.T) {
	app, ctx, ids := provisioned(t)
	expected := seedReceivables(t, app, ctx, ids)

	board, err := app.Dashboard(ctx, ids.company, "2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}

	var found bool
	for _, tile := range board.Tiles {
		if tile.Key != "sales.receivables" {
			continue
		}
		found = true
		if tile.Failed {
			t.Fatal("the receivables tile could not answer")
		}
		if tile.AmountMinor != expected {
			t.Errorf("the tile says %d is owed, want %d — an over-settled invoice has been "+
				"netted against a real debt", tile.AmountMinor, expected)
		}
		if tile.Periodic {
			t.Error("receivables is a position as at now, not a figure over the range")
		}
	}
	if !found {
		t.Error("there is no receivables tile")
	}
}

// TestTheCompanyTotalAgreesWithEachInvoicesOwnFigure
//
// One truth, two paths: the aggregate SQL and the per-document domain function. A shopkeeper
// comparing the home screen with an invoice must not find two answers.
func TestTheCompanyTotalAgreesWithEachInvoicesOwnFigure(t *testing.T) {
	app, ctx, ids := provisioned(t)
	seedReceivables(t, app, ctx, ids)

	documents, err := app.Sales.Documents(ctx, ids.company, "invoice", "posted")
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}
	if len(documents) < 2 {
		t.Fatalf("the fixture wrote %d invoices; it is meant to write two", len(documents))
	}

	var summed int64
	for _, document := range documents {
		owed, owedErr := app.Sales.Outstanding(ctx, document.ID)
		if owedErr != nil {
			t.Fatalf("Outstanding: %v", owedErr)
		}
		summed += owed
	}

	receivables, err := app.Sales.OutstandingTotal(ctx, ids.company)
	if err != nil {
		t.Fatalf("OutstandingTotal: %v", err)
	}
	if receivables.TotalMinor != summed {
		t.Errorf("the company total is %d; the invoices themselves add up to %d",
			receivables.TotalMinor, summed)
	}
}

// TestOutOfStockSeesTheShelvesTheValuationHides
//
// # The bug this was written after finding
//
// The first version counted the valuation's lines at or below zero. `ValuedLevels` EXCLUDES rows
// at zero — deliberately, so a valuation does not bury ninety meaningful lines under nine hundred
// worthless ones — so that count could only ever see NEGATIVE levels.
//
// The products a shop most needs to reorder are the ones sitting at exactly zero. The tile would
// have read zero on a shop with empty shelves, and looked perfectly healthy doing it.
//
// So the fixture holds one of each: in stock, run out, and gone negative. Only an implementation
// that reads the levels directly, at or below zero, gets it right.
func TestOutOfStockSeesTheShelvesTheValuationHides(t *testing.T) {
	app, ctx, ids := provisioned(t)
	seedStockLevels(t, app, ctx, ids)

	// The valuation cannot see the zero line. That is the premise, asserted rather than assumed:
	// if it ever starts including zeros, this test's reasoning needs revisiting.
	valuation, err := app.Inventory.Valuation(ctx, ids.company)
	if err != nil {
		t.Fatalf("Valuation: %v", err)
	}
	if len(valuation.Lines) != 2 {
		t.Errorf("the valuation shows %d lines; it is expected to hide the one at zero",
			len(valuation.Lines))
	}

	board, err := app.Dashboard(ctx, ids.company, "2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}

	var found bool
	for _, tile := range board.Tiles {
		if tile.Key != "inventory.out_of_stock" {
			continue
		}
		found = true
		if tile.Failed {
			t.Fatal("the out-of-stock tile could not answer")
		}
		if tile.Kind != "count" {
			t.Errorf("kind = %q, want count — it is a number of lines, not money", tile.Kind)
		}
		// The empty shelf AND the negative one. Two, not one.
		if tile.Count != 2 {
			t.Errorf("the tile counts %d lines out of stock, want 2 — one at zero and one "+
				"negative", tile.Count)
		}
	}
	if !found {
		t.Error("there is no out-of-stock tile")
	}
}

// seedStockLevels writes three levels: in stock, run out, and negative.
//
// The zero row is the one that matters — it is invisible to the valuation, and it is the ordinary
// state of a product a shop has sold out of. The negative row is a data-integrity fault: stock
// went out that was never booked in.
func seedStockLevels(t *testing.T, app *bootstrap.App, ctx context.Context, ids orgIDs) {
	t.Helper()

	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}

	write := func(query string, args ...any) {
		t.Helper()
		if _, err := app.DB.Writer(ctx).ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seeding stock: %v", err)
		}
	}

	for index, quantity := range []int64{5_000_000, 0, -2_000_000} {
		label := string(rune('A' + index))
		// The default variant comes back alongside the product, so the level row can name it
		// without a lookup — §A.1's "every product has at least one variant" made useful.
		created, variant, err := app.Catalog.CreateProduct(ctx, catalog.NewProductInput{
			CompanyID: ids.company, Code: "SKU" + label, Name: "Product " + label,
			// Named explicitly: the service resolves unit CODES, and leaving them blank is
			// refused rather than defaulted — a wrong unit being worse than a clear failure.
			StockUnit: "PCS", SalesUnit: "PCS", PurchaseUnit: "PCS",
		})
		if err != nil {
			t.Fatalf("CreateProduct: %v", err)
		}

		levelID, err := id.New()
		if err != nil {
			t.Fatalf("id.New: %v", err)
		}
		write(`
			INSERT INTO stock_levels (id, company_id, warehouse_id, product_id, variant_id,
			                          qty_on_hand_micro, avg_cost_micro, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, 1000000, '2026-08-10', '2026-08-10')`,
			levelID.String(), string(ids.company), string(ids.warehouse),
			string(created.ID), string(variant.ID), quantity)
	}
}
