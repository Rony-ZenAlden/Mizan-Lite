package bootstrap_test

import (
	"testing"
	"time"
)

// TestAYearOfSalesReportsInTime is L6 §12.1's measure of D-L6.18: a year of a busy pantry — 36,500 sales and 109,500 lines,
// generated in SQL — and a month's report under 300 ms, a year of products under 2 s. Ten times the bound under the race
// detector, whose instrumentation of the driver is what it would measure. Were it too slow, the facts ports would gain
// summed queries; the domain would stay as it is.
func TestAYearOfSalesReportsInTime(t *testing.T) {
	if testing.Short() {
		t.Skip("a year of sales")
	}
	s := openShop(t, 66)
	ctx, db := s.ctx, s.app.DB
	var rateID string
	if err := db.Reader(ctx).QueryRowContext(ctx, `SELECT id FROM fx_rates ORDER BY seq DESC LIMIT 1`).Scan(&rateID); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
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
		args := []any{rateID}[:countPlaceholders(statement)]
		if _, err := db.Writer(ctx).ExecContext(ctx, statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	var sales, lines int
	_ = db.Reader(ctx).QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM sales), (SELECT COUNT(*) FROM sale_lines)`).Scan(&sales, &lines)
	if sales != 36_500 || lines != 109_500 {
		t.Fatalf("%d sales, %d lines", sales, lines)
	}
	scale := time.Duration(1)
	if raceEnabled {
		scale = 10
	}
	s.owner()
	measure := func(name string, bound time.Duration, fn func() error) {
		t.Helper()
		start := time.Now()
		if err := fn(); err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)
		t.Logf("%s: %s", name, elapsed)
		if elapsed > bound*scale {
			t.Errorf("%s took %s, over %s", name, elapsed, bound*scale)
		}
	}
	measure("a month", 300*time.Millisecond, func() error {
		m, err := s.app.Reports.Month(ctx, "2026-08")
		if err == nil && m.Total.Profit.Sales != 3_100 {
			t.Errorf("August holds %d sales", m.Total.Profit.Sales)
		}
		return err
	})
	measure("a year of products", 2*time.Second, func() error {
		p, err := s.app.Reports.Products(ctx, "2025-09-15", "2026-09-14")
		if err == nil && (p.Total.Sales != 36_500 || p.Total.RevenueLocal != 36_500*45_000) {
			t.Errorf("the year = %+v", p.Total)
		}
		return err
	})
	measure("a day", 300*time.Millisecond, func() error {
		_, err := s.app.Reports.Day(ctx, "2026-03-01")
		return err
	})
}

func countPlaceholders(statement string) int {
	n := 0
	for _, r := range statement {
		if r == '?' {
			n++
		}
	}
	return n
}
