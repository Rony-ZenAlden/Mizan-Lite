package sqlite_test

import (
	"context"
	"errors"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/sales"
	"github.com/mizan-erp/mizan/internal/lite/sales/domain"
	"github.com/mizan-erp/mizan/internal/lite/sales/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/sales/salestest"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// TestSalesAreUpdatedOnlyToVoidAndLinesNever reads the store's source.
func TestSalesAreUpdatedOnlyToVoidAndLinesNever(t *testing.T) {
	raw, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatal(err)
	}
	src := strings.ToUpper(string(raw))
	updates := regexp.MustCompile(`UPDATE\s+SALES\s+SET\s+([^\n]*)`).FindAllStringSubmatch(src, -1)
	if len(updates) != 1 || !strings.HasPrefix(updates[0][1], "STATUS = 'VOIDED'") {
		t.Fatalf("the store updates sales other than to void them: %v", updates)
	}
	for _, forbidden := range []string{`UPDATE\s+SALE_LINES`, `DELETE\s+FROM\s+SALE`, `REPLACE\s+INTO`, `ON\s+CONFLICT`} {
		if regexp.MustCompile(forbidden).MatchString(src) {
			t.Errorf("the store rewrites sales: %s", forbidden)
		}
	}
	if !strings.Contains(src, "AND STATUS = 'POSTED'") {
		t.Error("the void's UPDATE does not require the sale to be posted")
	}
}

type row map[string]any

func (r row) with(changes row) row {
	out := row{}
	for k, v := range r {
		out[k] = v
	}
	for k, v := range changes {
		out[k] = v
	}
	return out
}

func insert(db *database.Store, table string, r row) error {
	columns := make([]string, 0, len(r))
	for c := range r {
		columns = append(columns, c)
	}
	sort.Strings(columns)
	args := make([]any, len(columns))
	for i, c := range columns {
		args[i] = r[c]
	}
	_, err := db.Writer(context.Background()).ExecContext(context.Background(),
		`INSERT INTO `+table+` (`+strings.Join(columns, ", ")+`) VALUES (`+strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ")+`)`, args...)
	return err
}

func uid(t *testing.T) string {
	v, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return v.String()
}

// TestACheckRefusesAnImpossibleSale inserts, for each constraint in 0005_till.sql on sales and sale_lines, a row only
// that constraint forbids — NULL cases included — and asserts which constraint refused it (L2 R3).
func TestACheckRefusesAnImpossibleSale(t *testing.T) {
	db := litetest.OpenMigrated(t)
	rate, product := insertRate(t, db).String(), insertProduct(t, db).String()
	receipt := 0
	sale := func() row {
		receipt++
		return row{"id": uid(t), "receipt_no": receipt, "business_date": "2026-09-14", "sold_at": "x", "status": "posted", "payment": "cash",
			"local_currency": "SYP", "fx_rate_id": rate, "local_per_usd_nano": 15_000_000_000_000, "rate_recorded_at": "x",
			"settlement_currency": "SYP", "lines_local_minor": 132_400, "lines_usd_minor": 883, "discount_local_minor": 0,
			"discount_usd_minor": 0, "cash_increment_minor": 500, "rounding_minor": 100, "total_minor": 132_500,
			"tendered_currency": "USD", "tendered_minor": 1_000, "change_currency": "SYP", "change_minor": 17_500,
			"cost_usd_minor": 750, "shop_name_snapshot": "المونة", "created_at": "x"}
	}
	accepted := map[string]row{
		"pounds total, dollars handed, pounds change": sale(),
		"a dollar settlement":                         sale().with(row{"settlement_currency": "USD", "rounding_minor": 0, "total_minor": 883, "change_currency": "USD", "change_minor": 117}),
		"a voided sale":                               sale().with(row{"status": "voided", "voided_at": "x", "void_business_date": "2026-09-15", "void_reason": "خطأ"}),
		"credit (schema only)":                        sale().with(row{"payment": "credit"}),
		"a discount on the sale":                      sale().with(row{"discount_local_minor": 32_400, "discount_usd_minor": 216, "rounding_minor": 0, "total_minor": 100_000}),
	}
	for name, r := range accepted {
		if err := insert(db, "sales", r); err != nil {
			t.Errorf("refused %s: %v", name, err)
		}
	}
	for _, c := range []struct {
		name       string
		r          row
		constraint string
	}{
		{"total ≠ lines − discount + rounding", sale().with(row{"total_minor": 132_600}), "ck_sale_total_is_lines_less_discount_plus_rounding"},
		{"a discount larger than the lines", sale().with(row{"discount_local_minor": 132_401, "rounding_minor": 132_501, "total_minor": 132_500}), "ck_sale_discount_within_lines"},
		{"dollars cash-rounded", sale().with(row{"settlement_currency": "USD", "rounding_minor": 17, "total_minor": 900}), "ck_sale_dollars_are_not_cash_rounded"},
		{"settled in a third currency", sale().with(row{"settlement_currency": "EUR"}), "ck_sale_settles_in_its_two_currencies"},
		{"tendered in a third currency", sale().with(row{"tendered_currency": "EUR"}), "ck_sale_tender_in_its_two_currencies"},
		{"change in a third currency", sale().with(row{"change_currency": "EUR"}), "ck_sale_change_in_its_two_currencies"},
		{"USD as the local currency", sale().with(row{"local_currency": "USD", "settlement_currency": "USD", "rounding_minor": 0, "total_minor": 883}), "ck_sale_local_is_not_usd"},
		{"voided without a reason (NULL)", sale().with(row{"status": "voided", "voided_at": "x", "void_business_date": "2026-09-15"}), "ck_sale_void_is_complete"},
		{"voided without a day (NULL)", sale().with(row{"status": "voided", "voided_at": "x", "void_reason": "خطأ"}), "ck_sale_void_is_complete"},
		{"posted with a void date", sale().with(row{"void_business_date": "2026-09-15"}), "ck_sale_void_is_complete"},
		{"a rate that does not exist", sale().with(row{"fx_rate_id": uid(t)}), "FOREIGN KEY"},
		{"an unknown payment", sale().with(row{"payment": "card"}), "payment IN"},
		{"a note of zero", sale().with(row{"cash_increment_minor": 0}), "cash_increment_minor >= 1"},
		{"NULL rounding", sale().with(row{"rounding_minor": nil}), "NOT NULL"},
		{"a negative change", sale().with(row{"change_minor": -1}), "change_minor >= 0"},
		{"receipt number 0", sale().with(row{"receipt_no": 0}), "receipt_no >= 1"},
		{"a receipt number used twice", sale().with(row{"receipt_no": 1}), "UNIQUE"},
	} {
		if err := insert(db, "sales", c.r); err == nil || !strings.Contains(err.Error(), c.constraint) {
			t.Errorf("%s: %v, want refused by %s", c.name, err, c.constraint)
		}
	}

	parent := sale()
	if err := insert(db, "sales", parent); err != nil {
		t.Fatal(err)
	}
	lineNo := 0
	line := func() row {
		lineNo++
		return row{"id": uid(t), "sale_id": parent["id"], "line_no": lineNo, "product_id": product, "name_ar_snapshot": "زيت",
			"unit_code_snapshot": "l", "quantity_micro": 2_000_000, "price_currency": "USD", "unit_price_micro": 6_500_000,
			"gross_local_minor": 195_000, "gross_usd_minor": 1_300, "discount_percent_micro": 100_000, "discount_local_minor": 19_500,
			"discount_usd_minor": 130, "unit_cost_usd_micro": 4_680_000, "cost_known": 1, "cost_usd_minor": 936, "cost_local_minor": 140_400}
	}
	for name, r := range map[string]row{
		"a discounted line":                 line(),
		"a line with unknown cost":          line().with(row{"cost_known": 0, "unit_cost_usd_micro": 0, "cost_usd_minor": 0, "cost_local_minor": 0}),
		"a percent that rounded to nothing": line().with(row{"discount_percent_micro": 1, "discount_local_minor": 0, "discount_usd_minor": 0}),
	} {
		if err := insert(db, "sale_lines", r); err != nil {
			t.Errorf("refused %s: %v", name, err)
		}
	}
	for _, c := range []struct {
		name       string
		r          row
		constraint string
	}{
		{"unknown cost carrying a cost", line().with(row{"cost_known": 0}), "ck_sale_line_unknown_cost_is_zero"},
		{"a discount with no percent", line().with(row{"discount_percent_micro": 0}), "ck_sale_line_no_percent_no_discount"},
		{"a discount beyond the line", line().with(row{"discount_local_minor": 195_001}), "ck_sale_line_discount_within_gross"},
		{"a percent over 100", line().with(row{"discount_percent_micro": 1_000_001}), "discount_percent_micro BETWEEN"},
		{"zero quantity", line().with(row{"quantity_micro": 0}), "quantity_micro > 0"},
		{"line 0", line().with(row{"line_no": 0}), "line_no >= 1"},
		{"a sale that does not exist", line().with(row{"sale_id": uid(t)}), "FOREIGN KEY"},
		{"NULL name (NULL)", line().with(row{"name_ar_snapshot": nil}), "NOT NULL"},
		{"two lines in one place", line().with(row{"line_no": 1}), "UNIQUE"},
	} {
		if err := insert(db, "sale_lines", c.r); err == nil || !strings.Contains(err.Error(), c.constraint) {
			t.Errorf("%s: %v, want refused by %s", c.name, err, c.constraint)
		}
	}
}

// failingStock fails the first sale movement, after the sale and its lines were inserted.
type failingStock struct{ *salestest.Stock }

var errStock = errors.New("the stock movement failed")

func (failingStock) RecordSale(context.Context, sales.StockLine) error { return errStock }

// TestACheckoutThatFailsAfterInsertingLeavesNothing: with the stock movement failing after the sale was written, the
// real transaction leaves no sale, no lines and no used receipt number.
func TestACheckoutThatFailsAfterInsertingLeavesNothing(t *testing.T) {
	ctx := context.Background()
	db := litetest.OpenMigrated(t)
	catalogue, stock, rates := salestest.NewCatalogue(), salestest.NewStock(), salestest.NewRates()
	rates.Rate.ID = insertRate(t, db)
	jar := catalogue.Add(domain.Product{ID: insertProduct(t, db), NameAR: "دبس", UnitCode: "jar", PriceCurrency: "SYP", PriceMicro: 45_000_000_000, Active: true}, "")
	store := sqlite.NewStore(db, clock.System())
	clk := clock.NewFixed(time.Date(2026, 9, 14, 7, 0, 0, 0, time.UTC))
	in := domain.CartInput{Lines: []domain.LineInput{{ProductID: jar.ID, Quantity: "1"}}}

	failing := sales.NewService(db, store, catalogue, failingStock{stock}, rates, salestest.NewSettings(), &salestest.Gate{}, clk, time.UTC)
	q, err := failing.Quote(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = failing.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token}); !errors.Is(err, errStock) {
		t.Fatalf("err = %v", err)
	}
	var sales64, lines int64
	if err = db.Reader(ctx).QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM sales), (SELECT COUNT(*) FROM sale_lines)`).Scan(&sales64, &lines); err != nil {
		t.Fatal(err)
	}
	if sales64 != 0 || lines != 0 {
		t.Fatalf("%d sales and %d lines survived a failed checkout", sales64, lines)
	}

	ok := sales.NewService(db, store, catalogue, stock, rates, salestest.NewSettings(), &salestest.Gate{}, clk, time.UTC)
	sale, err := ok.Checkout(ctx, sales.CheckoutInput{Cart: in, Token: q.Token})
	if err != nil || sale.ReceiptNo != 1 {
		t.Fatalf("the next checkout = %+v, %v — a rolled-back checkout used a receipt number", sale, err)
	}
	if got, err := store.Get(ctx, sale.ID); err != nil || got.TotalMinor != 45_000 || len(got.Lines) != 1 {
		t.Fatalf("stored = %+v, %v", got, err)
	}
}
