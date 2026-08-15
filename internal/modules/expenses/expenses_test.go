package expenses_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting"
	"github.com/mizan-erp/mizan/internal/modules/audit"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/expenses"
	"github.com/mizan-erp/mizan/internal/modules/identity"
	"github.com/mizan-erp/mizan/internal/modules/org"
	"github.com/mizan-erp/mizan/internal/modules/partner"
	"github.com/mizan-erp/mizan/internal/modules/profile"
	"github.com/mizan-erp/mizan/internal/modules/sales"
	"github.com/mizan-erp/mizan/internal/modules/tax"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/internal/platform/numbering"
	"github.com/mizan-erp/mizan/migrations"
)

type fixture struct {
	svc        *expenses.Service
	accounting *accounting.Service
	audit      *audit.Service
	bus        *eventbus.Bus
	store      *database.Store
	ctx        context.Context

	companyID id.ID
	branchID  id.ID
	partnerID id.ID
}

// fixedTax taxes everything at a flat rate, so the tests are about the EXPENSE and not about the
// tax engine — which has its own tests in Phase 2.
type fixedTax struct{ rateMicro int64 }

func (t fixedTax) TaxFor(
	_ context.Context, q expenses.TaxQuery,
) (expenses.ResolvedTax, error) {
	return expenses.ResolvedTax{
		AmountMinor: q.NetMinor * t.rateMicro / 1_000_000,
		RateMicro:   t.rateMicro, Code: "VAT",
	}, nil
}

type platformNumbering struct{ alloc *numbering.Allocator }

func (n platformNumbering) Allocate(
	ctx context.Context, branchID id.ID, seriesCode string,
) (string, error) {
	return n.alloc.Allocate(ctx, branchID, seriesCode)
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")

	store, err := database.Open(database.Config{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	merged := migrate.Merge(
		migrations.SQLite(),
		currency.NewModule(nil).Migrations(),
		org.NewModule(nil).Migrations(),
		audit.NewModule(nil).Migrations(),
		identity.NewModule(nil).Migrations(),
		profile.NewModule(nil).Migrations(),
		accounting.NewModule(nil).Migrations(),
		tax.NewModule(nil).Migrations(),
		partner.NewModule(nil).Migrations(),
		expenses.NewModule(nil).Migrations(),
	)
	runner, err := migrate.New(store, migrate.Options{FS: merged, DBPath: path, SkipBackup: true})
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	if _, err = runner.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	identifier, _ := id.New()
	now := clock.Format(clock.System().Now())
	// A TWO-DECIMAL currency, deliberately. 6.6's defect survived two phases because every test
	// that consumed a costed value used a currency with no minor unit — at scale 0 a conversion
	// that forgets the scale is indistinguishable from one that does not.
	if _, err = store.Writer(ctx).ExecContext(ctx,
		`INSERT INTO currencies (id, code, name, symbol, decimal_places, created_at, updated_at)
		 VALUES (?, 'SAR', 'Saudi Riyal', 'SAR', 2, ?, ?)`,
		string(identifier), now, now); err != nil {
		t.Fatalf("seeding currency: %v", err)
	}

	bus := eventbus.New(eventbus.Options{})
	auditSvc := audit.NewService(store, clock.System(), nil)
	if err = audit.NewModule(auditSvc).Subscribe(bus, nil); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	orgSvc := org.NewService(store, clock.System(), bus)
	provisioned, err := orgSvc.Provision(ctx, org.ProvisionInput{
		Company: org.CompanyInput{
			Code: "MAIN", Name: "Demo", CountryCode: "SA", FunctionalCurrency: "SAR",
		},
		Branch:               org.LocationInput{Code: "HQ", Name: "Head Office"},
		Warehouse:            org.LocationInput{Code: "WH1", Name: "Main"},
		FiscalYearStartMonth: time.January,
		FiscalYearStartYear:  2026,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	books, err := accounting.NewService(store, accounting.Options{
		Clock: clock.System(), Bus: bus,
	})
	if err != nil {
		t.Fatalf("accounting.NewService: %v", err)
	}
	if err = accounting.NewModule(books).Subscribe(bus, nil); err != nil {
		t.Fatalf("subscribe accounting: %v", err)
	}
	if err = books.ApplyChart(ctx, provisioned.CompanyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}
	if err = books.ApplyRules(ctx, provisioned.CompanyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyRules: %v", err)
	}

	svc := expenses.NewService(store, expenses.Options{
		Clock: clock.System(), Bus: bus,
		Tax:     fixedTax{rateMicro: 150_000},
		Numbers: platformNumbering{alloc: numbering.New(store, clock.System())},
	})
	if err = svc.ApplyCategories(ctx, provisioned.CompanyID, "generic_trading"); err != nil {
		t.Fatalf("ApplyCategories: %v", err)
	}

	salesSvc := sales.NewService(store, sales.Options{Clock: clock.System(), Bus: bus})
	if _, err = salesSvc.CreateSeries(ctx, sales.NewSeriesInput{
		CompanyID: provisioned.CompanyID, Code: expenses.SeriesExpense,
		Prefix: "EXP-", Padding: 6,
	}); err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}

	partnerID, _ := id.New()
	if _, err = store.Writer(ctx).ExecContext(ctx, `
		INSERT INTO partners (
			id, company_id, code, name, is_customer, is_supplier, partner_type,
			payment_terms_days, credit_limit_minor, is_active, has_history,
			row_version, created_at, updated_at
		) VALUES (?, ?, 'LAND', 'The landlord', 0, 1, 'company', 30, 0, 1, 0, 1, ?, ?)`,
		string(partnerID), string(provisioned.CompanyID), now, now); err != nil {
		t.Fatalf("creating a partner: %v", err)
	}

	return fixture{
		svc: svc, accounting: books, audit: auditSvc, bus: bus, store: store, ctx: ctx,
		companyID: provisioned.CompanyID, branchID: provisioned.BranchID,
		partnerID: partnerID,
	}
}

func (f fixture) category(t *testing.T, code string) id.ID {
	t.Helper()
	categories, err := f.svc.Categories(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	for _, category := range categories {
		if category.Code == code {
			return category.ID
		}
	}
	t.Fatalf("no category %q", code)
	return ""
}

func (f fixture) balances(t *testing.T) map[string]int64 {
	t.Helper()
	years, err := f.accounting.Years(f.ctx, f.companyID)
	if err != nil {
		t.Fatalf("Years: %v", err)
	}
	periods, err := f.accounting.Periods(f.ctx, years[0].ID)
	if err != nil {
		t.Fatalf("Periods: %v", err)
	}
	out := map[string]int64{}
	for _, period := range periods {
		rows, tbErr := f.accounting.TrialBalance(f.ctx, f.companyID, period.ID)
		if tbErr != nil {
			t.Fatalf("TrialBalance: %v", tbErr)
		}
		for _, row := range rows {
			out[row.AccountCode] += row.Debit - row.Credit
		}
	}
	return out
}
