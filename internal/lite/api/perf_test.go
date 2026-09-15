package api_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/lite/api"
	"github.com/mizan-erp/mizan/internal/lite/documents"
	"github.com/mizan-erp/mizan/internal/lite/typeset"
)

// TestAYearOfSalesHistoryExportsInTime is L7 §12's measure: a year of a busy pantry — 36,500 sales and 109,500 lines, generated
// in SQL as L6's measure is — exported to Excel under 3 s and to PDF under 10 s, and a receipt rendered under 100 ms. Ten times the
// bound under the race detector.
func TestAYearOfSalesHistoryExportsInTime(t *testing.T) {
	if testing.Short() {
		t.Skip("a year of sales")
	}
	set, oil := tillShop(t)
	for _, name := range []string{"رز", "سكر", "شاي"} {
		if r := set.Catalog.CreateProduct(api.CreateProductInput{NameAR: name, UnitCode: "kg", PriceCurrency: "SYP", Price: "15000"}); !r.OK {
			t.Fatal(r.Error)
		}
	}
	app := api.Graph(set)
	ctx := t.Context()
	var rateID string
	if err := app.DB.Reader(ctx).QueryRowContext(ctx, `SELECT id FROM fx_rates ORDER BY seq DESC LIMIT 1`).Scan(&rateID); err != nil {
		t.Fatal(err)
	}
	for i, statement := range []string{
		`WITH RECURSIVE d(n) AS (SELECT 0 UNION ALL SELECT n + 1 FROM d WHERE n < 364),
		              k(n) AS (SELECT 0 UNION ALL SELECT n + 1 FROM k WHERE n < 99)
		 INSERT INTO sales (id, receipt_no, business_date, sold_at, status, payment, local_currency, fx_rate_id, local_per_usd_nano,
		                    rate_recorded_at, settlement_currency, lines_local_minor, lines_usd_minor, discount_local_minor, discount_usd_minor,
		                    cash_increment_minor, rounding_minor, total_minor, tendered_currency, tendered_minor, change_currency, change_minor,
		                    cost_usd_minor, shop_name_snapshot, created_at)
		 SELECT printf('00000000-%04d-7000-8000-%012d', k.n, d.n), d.n * 100 + k.n + 1, date('2025-09-15', '+' || d.n || ' days'),
		        date('2025-09-15', '+' || d.n || ' days') || 'T08:00:00.000Z', 'posted', 'cash', 'SYP', ?, 15000000000000,
		        '2025-09-15T06:00:00.000Z', 'SYP', 45000, 300, 0, 0, 500, 0, 45000, 'SYP', 45000, 'SYP', 0, 210, 'x', 't'
		   FROM d, k`,
		`WITH RECURSIVE l(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM l WHERE n < 3),
		              p(id, k) AS (SELECT id, ROW_NUMBER() OVER (ORDER BY id) - 1 FROM products)
		 INSERT INTO sale_lines (id, sale_id, line_no, product_id, name_ar_snapshot, name_en_snapshot, unit_code_snapshot, quantity_micro,
		                         price_currency, unit_price_micro, gross_local_minor, gross_usd_minor, discount_percent_micro,
		                         discount_local_minor, discount_usd_minor, unit_cost_usd_micro, cost_known, cost_usd_minor, cost_local_minor)
		 SELECT substr(s.id, 1, 14) || printf('700%d', l.n) || substr(s.id, 19), s.id, l.n, p.id,
		        'منتج', 'product', 'kg', 1000000, 'SYP', 15000000000, 15000, 100, 0, 0, 0, 700000, 1, 70, 10500
		   FROM sales s, l JOIN p ON p.k = (s.receipt_no + l.n) % 4`,
	} {
		args := []any{}
		if i == 0 {
			args = append(args, rateID)
		}
		if _, err := app.DB.Writer(ctx).ExecContext(ctx, statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	var sales, lines int
	_ = app.DB.Reader(ctx).QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM sales), (SELECT COUNT(*) FROM sale_lines)`).Scan(&sales, &lines)
	if sales != 36_500 || lines != 109_500 {
		t.Fatalf("%d sales, %d lines", sales, lines)
	}

	scale := time.Duration(1)
	if raceEnabled {
		scale = 10
	}
	measure := func(name string, bound time.Duration, fn func()) {
		t.Helper()
		start := time.Now()
		fn()
		elapsed := time.Since(start)
		t.Logf("%s: %s", name, elapsed)
		if elapsed > bound*scale {
			t.Errorf("%s took %s, over %s", name, elapsed, bound*scale)
		}
	}
	files := &fakeFiles{}
	set.SetFiles(files)
	elevate(t, set)
	for _, c := range []struct {
		format string
		bound  time.Duration
	}{{"xlsx", 3 * time.Second}, {"pdf", 10 * time.Second}} {
		files.save = filepath.Join(t.TempDir(), "year."+c.format)
		measure("a year of sales history to "+c.format, c.bound, func() {
			r := set.Export.SalesHistory(api.ExportRangeInput{From: "2025-09-15", To: "2026-09-14", Format: c.format})
			if !r.OK || r.Data.Cancelled {
				t.Fatalf("export %+v", r)
			}
		})
		info, err := os.Stat(files.save)
		if err != nil || info.Size() == 0 {
			t.Fatalf("%s: %v", files.save, err)
		}
		t.Logf("%s: %d bytes", c.format, info.Size())
	}

	sale := saleOnTheTill(t, set, oil, api.CartInput{Lines: []api.CartLineInput{{ProductID: oil.ID, Quantity: "2"}}})
	ts, err := typeset.Default()
	if err != nil {
		t.Fatal(err)
	}
	measure("a receipt", 100*time.Millisecond, func() {
		doc, err := api.PrintDocument(set, "sale", sale.ID)
		if err != nil {
			t.Fatal(err)
		}
		if img := documents.Raster(ts, doc, documents.Receipt80); img.Bounds().Dx() != 576 {
			t.Fatalf("receipt %v wide", img.Bounds())
		}
	})
}
