package reports_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/reports"
	"github.com/mizan-erp/mizan/internal/lite/reports/domain"
	"github.com/mizan-erp/mizan/internal/lite/reports/reportstest"
)

var ctx = context.Background()

func newService() (*reports.Service, *reportstest.Facts, *reportstest.Gate) {
	f := &reportstest.Facts{Local: "SYP", RateList: []domain.Rate{{Seq: 1, Nano: 15_000_000_000_000, BusinessDate: "2026-09-01"}}}
	g := &reportstest.Gate{}
	clk := clock.NewFixed(time.Date(2026, 9, 14, 22, 0, 0, 0, time.UTC)) // the 15th in Damascus
	svc := reports.NewService(f, f, f, f, f, reportstest.Cash{F: f}, g, clk, time.FixedZone("Damascus", 3*3600))
	svc.UsePayables(f)
	return svc, f, g
}

func TestReportsAreTheOwners(t *testing.T) {
	svc, _, g := newService()
	checks := map[string]func() error{
		"day":      func() error { _, err := svc.Day(ctx, ""); return err },
		"month":    func() error { _, err := svc.Month(ctx, ""); return err },
		"products": func() error { _, err := svc.Products(ctx, "", ""); return err },
		"stock":    func() error { _, err := svc.Stock(ctx, "", ""); return err },
	}
	for name, check := range checks {
		if err := check(); errs.CodeOf(err) != reports.CodeOwnerRequired {
			t.Errorf("%s outside owner mode: %v", name, err)
		}
	}
	g.Elevated = true
	for name, check := range checks {
		if err := check(); err != nil {
			t.Errorf("%s in owner mode: %v", name, err)
		}
	}
}

func TestDatesDefaultToTheBusinessDayAndAreChecked(t *testing.T) {
	svc, f, g := newService()
	g.Elevated = true
	if d, _ := svc.Day(ctx, ""); d.Date != "2026-09-15" {
		t.Fatalf("today is %s", d.Date)
	}
	if m, _ := svc.Month(ctx, ""); m.From != "2026-09-01" || m.To != "2026-09-30" {
		t.Fatalf("this month %s..%s", m.From, m.To)
	}
	f.Asked = nil
	if p, _ := svc.Products(ctx, "", ""); p.From != "2026-09-01" || p.To != "2026-09-15" || f.Asked[0] != [2]string{"2026-09-01", "2026-09-15"} {
		t.Fatalf("the month to date: %s..%s", p.From, p.To)
	}
	for name, err := range map[string]error{
		"day":    func() error { _, err := svc.Day(ctx, "2026-02-30"); return err }(),
		"month":  func() error { _, err := svc.Month(ctx, "2026-13"); return err }(),
		"range":  func() error { _, err := svc.Products(ctx, "2026-09-10", "2026-09-01"); return err }(),
		"stock":  func() error { _, err := svc.Stock(ctx, "x", ""); return err }(),
		"drawer": func() error { _, err := svc.Drawer(ctx, "14-09-2026"); return err }(),
	} {
		if err == nil || errs.CategoryOf(err) != errs.CategoryValidation {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTheDrawerIsTheCountersWithoutTheOwnersDetails(t *testing.T) {
	svc, f, g := newService()
	f.CashList = []domain.CashEntry{
		{Seq: 1, BusinessDate: "2026-09-14", Kind: domain.CashCount, Currency: "SYP", AmountMinor: 100_000, ExpectedMinor: 100_000},
		{Seq: 2, BusinessDate: "2026-09-15", Kind: domain.CashExpense, Currency: "SYP", AmountMinor: 30_000, Category: "electricity", FromDrawer: true, Note: "فاتورة"},
		{Seq: 3, BusinessDate: "2026-09-15", Kind: domain.CashWithdrawal, Currency: "SYP", AmountMinor: 20_000},
		{Seq: 4, BusinessDate: "2026-09-15", Kind: domain.CashCount, Currency: "SYP", AmountMinor: 50_000, ExpectedMinor: 50_000},
	}
	f.SaleList = []domain.Sale{{BusinessDate: "2026-09-13", SettlementCurrency: "SYP", TotalMinor: 999_000, TenderedCurrency: "SYP", TenderedMinor: 999_000}}
	counter, err := svc.Drawer(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	syp := counter.Currencies[1]
	if counter.OwnerView || syp.ExpensesOut != 0 || syp.WithdrawalOut != 50_000 || syp.Opening != 100_000 || syp.Expected != 50_000 || len(counter.Entries) != 1 {
		t.Fatalf("counter view %+v", counter)
	}
	g.Elevated = true
	owner, _ := svc.Drawer(ctx, "2026-09-15")
	if !owner.OwnerView || owner.Currencies[1].ExpensesOut != 30_000 || owner.Currencies[1].WithdrawalOut != 20_000 || len(owner.Entries) != 3 {
		t.Fatalf("owner view %+v", owner)
	}
	if got, _ := svc.ExpectedCash(ctx, "2026-09-15", "SYP"); got != 50_000 {
		t.Fatalf("expected cash %d", got)
	}
	if got, _ := svc.ExpectedCash(ctx, "2026-09-15", "USD"); got != 0 {
		t.Fatalf("expected dollars %d", got)
	}
	// A supplier paid out of the drawer after the count (0.10.0): the counter sees it as they see a withdrawal, as a figure.
	f.SupplierList = []domain.SupplierCash{{BusinessDate: "2026-09-16", Currency: "SYP", PaidOutMinor: 45_000}}
	if got, _ := svc.ExpectedCash(ctx, "2026-09-16", "SYP"); got != 5_000 {
		t.Fatalf("expected cash after paying a supplier %d", got)
	}
	if next, _ := svc.Drawer(ctx, "2026-09-16"); next.Currencies[1].SuppliersOut != 45_000 {
		t.Fatalf("the supplier's line %+v", next.Currencies[1])
	}
	// The count of the 14th closed that day: the sale of the 13th is not read again.
	f.Asked = nil
	_, _ = svc.Drawer(ctx, "2026-09-15")
	if f.Asked[0][0] != "0001-01-01" {
		t.Fatalf("dollars were never counted, so every day counts: %v", f.Asked)
	}
}

// TestTheReportsModuleWritesNothing scans the module for a call that writes (L6 §9.3, lite-reports-read-only).
func TestTheReportsModuleWritesNothing(t *testing.T) {
	writing := regexp.MustCompile(`\.(Insert|Update|Delete|Record\w*|Void|Checkout|Append|Set\w*|Create|Reverse\w*|Charge|WriteOff|Refund|Opening|Count|Require)\(`)
	files := 0
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		files++
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for n, line := range strings.Split(string(raw), "\n") {
			if writing.MatchString(line) {
				t.Errorf("%s:%d calls a writing method: %s", path, n+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil || files < 5 {
		t.Fatalf("scanned %d files: %v", files, err)
	}
}

// TestTheLossReportIsTheOwners (0.10.0): costs, so it asks for owner mode; the month to date when no range is given; and
// the goods that arrived damaged come with it.
func TestTheLossReportIsTheOwners(t *testing.T) {
	svc, f, g := newService()
	f.MoveList = []domain.Movement{{ID: "m1", ProductID: "p1", BusinessDate: "2026-09-14", Kind: domain.MoveAdjustment,
		Reason: domain.ReasonExpired, QuantityMicro: -2_000_000, UnitCostMicro: 1_000_000}}
	f.ArrivalList = []domain.ArrivalDamage{{BusinessDate: "2026-09-02", PurchaseNo: 3, DamagedMicro: 1_000_000, Currency: "USD", ValueMinor: 250}}
	if _, err := svc.Losses(ctx, "", ""); errs.CodeOf(err) != reports.CodeOwnerRequired {
		t.Fatalf("the losses outside owner mode: %v", err)
	}
	g.Elevated = true
	r, err := svc.Losses(ctx, "", "")
	if err != nil || r.From != "2026-09-01" || r.To != "2026-09-15" || len(r.Lines) != 1 || r.Total.USD != 200 || len(r.Arrival) != 1 {
		t.Fatalf("Losses = %+v, %v", r, err)
	}
	if _, err := svc.Losses(ctx, "2026-09-10", "2026-09-01"); errs.CategoryOf(err) != errs.CategoryValidation {
		t.Fatalf("a range backwards: %v", err)
	}
}
