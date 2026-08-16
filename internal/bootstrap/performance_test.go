package bootstrap_test

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/bootstrap"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/catalog"
	catalogdomain "github.com/mizan-erp/mizan/internal/modules/catalog/domain"
	"github.com/mizan-erp/mizan/internal/modules/partner"
	"github.com/mizan-erp/mizan/internal/modules/sales"
)

// # What this file measures, and what it deliberately does not
//
// There is no production install to profile and nobody complaining about a slow screen.
// Optimising against a guess is how a codebase acquires an index nobody needs and a cache that
// goes stale — so this MEASURES, and 10.2 changes only what a measurement finds.
//
// The bound is deliberately generous. A test that fails on a 10% regression fails on a busy build
// machine; one that fails when a report takes ten times longer catches the change that matters:
// an accidental N+1, a dropped index, a projection replaced by a live scan.

// reportBound is what every report must complete within, over a year of a busy shop's data.
//
// # The number, and what it can and cannot catch
//
// Measured on 14,600 documents: every report completes in **under 50ms** uninstrumented. Under
// `make ci` — which runs `-race -covermode=atomic` — the same reports take over a second, a
// twenty-fold difference that comes entirely from the instrumentation.
//
// A first attempt set the ceiling at one second, twenty times the clean measurement, and it FAILED
// under CI. That is the design's own warning arriving in practice: *a test that fails on a small
// regression fails on a busy build machine.* The ceiling has to clear the instrumented figure,
// not the clean one.
//
// Fifteen seconds clears it by roughly ten times. What that catches is the N+1 the drill
// simulated, which took the same report past the ceiling in seconds rather than finishing.
//
// What it CANNOT catch is drift. A report that goes from 44ms to five seconds is a hundred times
// worse and still passes. That is a real limit, stated rather than papered over — the elapsed
// times are LOGGED for exactly that reason, so a person reading a CI run can watch a report climb
// long before the ceiling notices.
const reportBound = 15 * time.Second

// busyShopDays is how much history the fixture generates.
//
// A year, at a rate a real shop reaches: enough rows that a missing index shows, few enough that
// the fixture builds in seconds rather than minutes. A test nobody runs because it takes four
// minutes is a test that does not exist.
const busyShopDays = 365

// TestEveryReportCompletesWithinItsBoundOverAYearOfHistory
//
// DoD criterion 2.
func TestEveryReportCompletesWithinItsBoundOverAYearOfHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("generating a year of history is not a short test")
	}

	app, ctx, companyID := traded(t)
	generated := generateYear(t, app, ctx, companyID)
	t.Logf("generated %d sales documents over %d days", generated, busyShopDays)

	// # Why each report runs under a DEADLINE and not just a stopwatch
	//
	// The first version timed each report and compared afterwards. A drill that made the sales
	// analysis re-query per row did not fail it — it HUNG, past a ten-minute timeout, because a
	// stopwatch cannot stop the thing it is timing.
	//
	// A pathological regression is exactly the case this test exists for, and hanging CI is the
	// worst way to report it: the run is killed, the output is truncated, and nobody learns which
	// report was slow. The deadline turns it into a named failure in bounded time.
	deadlined := func(run func(context.Context) error) (time.Duration, error) {
		bounded, cancel := context.WithTimeout(ctx, reportBound)
		defer cancel()

		started := time.Now()
		err := run(bounded)
		return time.Since(started), err
	}

	for _, report := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"profit and loss", func(ctx context.Context) error {
			_, err := app.Accounting.ProfitAndLoss(ctx, companyID, "2026-01-01", "2026-12-31")
			return err
		}},
		{"balance sheet", func(ctx context.Context) error {
			_, err := app.Accounting.BalanceSheet(ctx, companyID, "2026-12-31")
			return err
		}},
		{"sales by day", func(ctx context.Context) error {
			_, err := app.Sales.SalesByPeriod(
				ctx, companyID, "2026-01-01", "2026-12-31", sales.ByDay)
			return err
		}},
		{"sales by product", func(ctx context.Context) error {
			_, err := app.Sales.SalesByProduct(ctx, companyID, "2026-01-01", "2026-12-31")
			return err
		}},
		{"sales by partner", func(ctx context.Context) error {
			_, err := app.Sales.SalesByPartner(ctx, companyID, "2026-01-01", "2026-12-31")
			return err
		}},
		{"stock valuation", func(ctx context.Context) error {
			_, err := app.Inventory.Valuation(ctx, companyID)
			return err
		}},
		{"search", func(ctx context.Context) error {
			_, err := app.Search.Search(ctx, companyID, "wid", 10)
			return err
		}},
		{"dashboard", func(ctx context.Context) error {
			_, err := app.Dashboard(ctx, companyID, "2026-01-01", "2026-12-31")
			return err
		}},
	} {
		elapsed, err := deadlined(report.run)

		// LOGGED as well as asserted. The ceiling catches a structural regression; the log is
		// what a person reads to notice a report drifting from 44ms towards it, which no
		// assertion at this granularity can see.
		t.Logf("%-20s %v", report.name, elapsed.Round(time.Millisecond))

		if err != nil {
			t.Errorf("%s took at least %v over a year of history and was cut off at the %v "+
				"ceiling — something structural changed: an index, a projection, or a query "+
				"that now runs per row (%v)",
				report.name, elapsed.Round(time.Millisecond), reportBound, err)
		}
	}
}

// generateYear fills a company with a year of trading.
//
// # Written straight to the tables, and that is the right call here
//
// Every other fixture in this codebase goes through the services, because a row inserted around
// them skips the invariants they keep (9.4's whole design). This one does not, for one reason: it
// exists to make the DATABASE big, and posting fifteen thousand documents through the full
// pipeline would take minutes and measure the pipeline rather than the reports.
//
// The rows it writes are shaped exactly as the services shape them — same columns, same statuses,
// same snapshot fields — because a report reading differently-shaped rows would measure nothing
// useful. That is the trade, and it is stated rather than hidden.
func generateYear(
	t *testing.T, app *bootstrap.App, ctx context.Context, companyID id.ID,
) int {
	t.Helper()

	var branchID, warehouseID id.ID
	if err := app.DB.Reader(ctx).QueryRowContext(ctx,
		`SELECT b.id, w.id FROM branches b JOIN warehouses w ON w.branch_id = b.id
		  WHERE b.company_id = ? LIMIT 1`, string(companyID),
	).Scan(&branchID, &warehouseID); err != nil {
		t.Fatalf("finding the branch and warehouse: %v", err)
	}

	// A catalogue and a customer list, so the by-product and by-partner reports have something to
	// group by. Twenty products and fifty partners is a small shop's spread.
	products := seedProducts(t, app, ctx, companyID, 20)
	partners := seedPartners(t, app, ctx, companyID, 50)

	documents := 0
	err := app.DB.Do(ctx, func(txCtx context.Context) error {
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for day := range busyShopDays {
			date := start.AddDate(0, 0, day).Format(time.DateOnly)
			// Forty sales a day is a busy till.
			for n := range 40 {
				if err := insertSale(
					txCtx, app, companyID, branchID, warehouseID,
					date, products[(day+n)%len(products)], partners[(day*3+n)%len(partners)],
					documents,
				); err != nil {
					return err
				}
				documents++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("generating a year: %v", err)
	}
	return documents
}

type seededProduct struct {
	productID id.ID
	variantID id.ID
	name      string
	sku       string
}

func seedProducts(
	t *testing.T, app *bootstrap.App, ctx context.Context, companyID id.ID, count int,
) []seededProduct {
	t.Helper()
	if err := app.Catalog.ApplyUnits(ctx, "standard"); err != nil {
		t.Fatalf("ApplyUnits: %v", err)
	}

	out := make([]seededProduct, 0, count)
	for i := range count {
		code := "PERF-" + itoaTest(i)
		product, variant, err := app.Catalog.CreateProduct(ctx, catalog.NewProductInput{
			CompanyID: companyID, Code: code, Name: "Widget " + code,
			Type: catalogdomain.TypeGoods, StockUnit: "PCS",
		})
		if err != nil {
			t.Fatalf("CreateProduct(%s): %v", code, err)
		}
		out = append(out, seededProduct{
			productID: product.ID, variantID: variant.ID,
			name: product.Name, sku: variant.SKU,
		})
	}
	return out
}

func seedPartners(
	t *testing.T, app *bootstrap.App, ctx context.Context, companyID id.ID, count int,
) []id.ID {
	t.Helper()
	out := make([]id.ID, 0, count)
	for i := range count {
		created, err := app.Partner.CreatePartner(ctx, partner.NewPartnerInput{
			CompanyID: companyID, Code: "CUST-" + itoaTest(i),
			Name: "Customer " + itoaTest(i), IsCustomer: true,
		})
		if err != nil {
			t.Fatalf("CreatePartner: %v", err)
		}
		out = append(out, created.ID)
	}
	return out
}

// insertSale writes one posted invoice and its line, shaped as the sales service shapes them.
func insertSale(
	ctx context.Context, app *bootstrap.App,
	companyID, branchID, warehouseID id.ID,
	date string, product seededProduct, partnerID id.ID, sequence int,
) error {
	documentID, err := id.New()
	if err != nil {
		return err
	}
	lineID, err := id.New()
	if err != nil {
		return err
	}

	const price, cost = 10_000, 6_000
	if _, err = app.DB.Writer(ctx).ExecContext(ctx, `
		INSERT INTO sales_documents (
			id, company_id, branch_id, warehouse_id, document_type, status, document_number,
			partner_id, partner_name, document_date, currency_code, exchange_rate_micro,
			net_minor, tax_minor, discount_minor, total_minor, cost_minor,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, 'invoice', 'posted', ?, ?, 'Customer', ?, 'SYP', 1000000,
		          ?, 0, 0, ?, ?, ?, ?)`,
		string(documentID), string(companyID), string(branchID), string(warehouseID),
		"PERF-"+itoaTest(sequence), string(partnerID), date,
		price, price, cost, date, date); err != nil {
		return err
	}

	_, err = app.DB.Writer(ctx).ExecContext(ctx, `
		INSERT INTO sales_lines (
			id, document_id, line_number, product_id, variant_id,
			product_name, variant_sku, uom_code, uom_id,
			quantity_micro, quantity_stock_micro, unit_price_minor,
			discount_minor, tax_rate_micro, tax_amount_minor,
			net_minor, total_minor, cost_micro, created_at, updated_at
		) VALUES (?, ?, 1, ?, ?, ?, ?, 'PCS',
		          (SELECT id FROM units_of_measure LIMIT 1),
		          1000000, 1000000, ?, 0, 0, 0, ?, ?, ?, ?, ?)`,
		string(lineID), string(documentID), string(product.productID),
		string(product.variantID), product.name, product.sku,
		price, price, price, cost*1_000_000/1_000_000, date, date)
	return err
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
