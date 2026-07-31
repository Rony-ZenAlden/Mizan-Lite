package currency_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/locale"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
	"github.com/mizan-erp/mizan/internal/modules/currency"
	"github.com/mizan-erp/mizan/internal/modules/currency/domain"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/i18n"
	"github.com/mizan-erp/mizan/internal/platform/metadata"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/migrations"
)

// ── harness ─────────────────────────────────────────────────────────────────────

var day = time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)

type harness struct {
	db    *database.Store
	clk   *clock.Fixed
	svc   *currency.Service
	mod   *currency.Module
	trans *i18n.Translations
	ctx   context.Context
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "currency_test.db")

	db, err := database.Open(database.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	clk := clock.NewFixed(day.Add(12 * time.Hour))
	mod := currency.NewModule(nil)

	// The migration runner is handed the platform's files AND the module's, merged — which is
	// what §10.3's "module-owned migrations, globally ordered" means in practice.
	runner, err := migrate.New(db, migrate.Options{
		FS:         migrate.Merge(migrations.SQLite(), mod.Migrations()),
		DBPath:     dbPath,
		SkipBackup: true,
		Clock:      clk,
	})
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	if _, upErr := runner.Up(ctx); upErr != nil {
		t.Fatalf("migrate: %v", upErr)
	}

	trans := i18n.NewTranslations(db, clk, nil)
	svc := currency.NewService(db, clk, trans)
	mod = currency.NewModule(svc)

	// Seed through the Step 0.5 metadata seeder, exactly as the bootstrap will.
	seeder := metadata.NewSeeder(db, clk)
	for _, spec := range mod.Metadata() {
		if _, seedErr := seeder.Seed(ctx, spec); seedErr != nil {
			t.Fatalf("seed %s: %v", spec.Table, seedErr)
		}
	}
	if seedErr := mod.SeedTranslations(ctx, svc, trans); seedErr != nil {
		t.Fatalf("seed translations: %v", seedErr)
	}

	// Settings must be bound for the currency-role handles to resolve.
	settings, err := config.Open(ctx, db, config.Options{Clock: clk})
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}
	return &harness{db: db, clk: clk, svc: svc, mod: mod, trans: trans, ctx: config.Bind(ctx, settings)}
}

func (h *harness) currency(t *testing.T, code string) money.Currency {
	t.Helper()
	c, err := h.svc.Get(h.ctx, code)
	if err != nil {
		t.Fatalf("currency %s: %v", code, err)
	}
	return c
}

// setRate records a rate valid from `from`, advancing the clock first so successive writes to
// the same date get distinct created_at values — which is what makes a correction resolvable.
func (h *harness) setRate(t *testing.T, from, to string, nano int64, validFrom time.Time) {
	t.Helper()
	h.clk.Advance(time.Second)
	if err := h.svc.SetRate(h.ctx, from, to, domain.RateTypeMarket,
		money.RateFromNano(nano), validFrom, "manual"); err != nil {
		t.Fatalf("SetRate %s→%s: %v", from, to, err)
	}
}

func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", want)
	}
	if got := errs.CodeOf(err); got != want {
		t.Fatalf("code = %q, want %q (err: %v)", got, want, err)
	}
}

// ── schema & seeds ──────────────────────────────────────────────────────────────

func TestModuleMigrationMergesWithThePlatformSchema(t *testing.T) {
	h := newHarness(t)
	for _, table := range []string{"currencies", "rate_types", "exchange_rates"} {
		var n int
		if err := h.db.WriterPool().QueryRowContext(h.ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("table %q was not created by the merged migration set", table)
		}
	}
}

func TestSeedsAreIdempotent(t *testing.T) {
	// The 0.5 guarantee, exercised by its first real consumer: a second run must change
	// nothing, or every launch would generate audit noise that looks like corruption.
	h := newHarness(t)
	seeder := metadata.NewSeeder(h.db, h.clk)

	for _, spec := range h.mod.Metadata() {
		rep, err := seeder.Seed(h.ctx, spec)
		if err != nil {
			t.Fatalf("re-seed %s: %v", spec.Table, err)
		}
		if rep.Inserted != 0 || rep.Updated != 0 {
			t.Errorf("re-seeding %s changed %d inserted / %d updated, want 0/0",
				spec.Table, rep.Inserted, rep.Updated)
		}
	}
}

func TestSeededCurrenciesHaveTheirRealScales(t *testing.T) {
	// SYP has ZERO decimal places. That is the case that matters: a kernel assuming two would
	// be wrong for the primary target market.
	h := newHarness(t)
	cases := map[string]uint8{"SYP": 0, "USD": 2, "EUR": 2, "TRY": 2}
	for code, decimals := range cases {
		t.Run(code, func(t *testing.T) {
			c := h.currency(t, code)
			if c.Decimals() != decimals {
				t.Errorf("%s decimals = %d, want %d", code, c.Decimals(), decimals)
			}
			if c.Rounding() != round.HalfAwayFromZero {
				t.Errorf("%s rounding = %v", code, c.Rounding())
			}
		})
	}
}

func TestArabicCurrencyNamesResolveThroughTranslations(t *testing.T) {
	// Adding a language to reference data is a data operation (§9.5) — this is 0.8's resolver
	// doing what it was built for.
	h := newHarness(t)

	english, err := h.svc.List(locale.WithLocale(h.ctx, "en"), false)
	if err != nil {
		t.Fatal(err)
	}
	arabic, err := h.svc.List(locale.WithLocale(h.ctx, "ar"), false)
	if err != nil {
		t.Fatal(err)
	}
	byCode := func(list []domain.Info, code string) string {
		for _, i := range list {
			if i.Code == code {
				return i.Name
			}
		}
		return ""
	}
	if got := byCode(english, "SYP"); got != "Syrian Pound" {
		t.Errorf("en SYP = %q", got)
	}
	if got := byCode(arabic, "SYP"); got != "ليرة سورية" {
		t.Errorf("ar SYP = %q, want the translated name", got)
	}
}

func TestRateTypesAreSeededAsSystemRows(t *testing.T) {
	h := newHarness(t)
	types, err := h.svc.RateTypes(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 4 {
		t.Fatalf("got %d rate types, want 4", len(types))
	}
	for _, rt := range types {
		if !rt.IsSystem {
			t.Errorf("rate type %q is not is_system; code could not depend on it existing", rt.Code)
		}
	}
}

// ── conversion ──────────────────────────────────────────────────────────────────

func TestConversionPaths(t *testing.T) {
	h := newHarness(t)
	syp := h.currency(t, "SYP")
	usd := h.currency(t, "USD")
	eur := h.currency(t, "EUR")

	// USD → SYP direct at 13,000. USD → EUR direct at 0.92.
	h.setRate(t, "USD", "SYP", 13_000*1_000_000_000, day)
	h.setRate(t, "USD", "EUR", 920_000_000, day)

	t.Run("identity needs no rate at all", func(t *testing.T) {
		amt := money.FromMinor(syp, 500)
		res, err := h.svc.Convert(h.ctx, amt, syp, day, domain.RateTypeMarket)
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if res.Amount.Minor() != 500 || res.Path != domain.SourceIdentity {
			t.Errorf("identity = %+v", res)
		}
	})

	t.Run("direct", func(t *testing.T) {
		res, err := h.svc.Convert(h.ctx, money.FromMinor(usd, 100), syp, day, domain.RateTypeMarket)
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		// $1.00 × 13,000 = 13,000 SYP, and SYP has no minor units.
		if res.Amount.Minor() != 13_000 {
			t.Errorf("amount = %d, want 13000", res.Amount.Minor())
		}
		if res.Path != domain.SourceDirect {
			t.Errorf("path = %q, want direct", res.Path)
		}
		if res.Stale {
			t.Error("a rate valid on the date was reported stale")
		}
	})

	t.Run("inverse", func(t *testing.T) {
		// Only USD→SYP is stored; SYP→USD must invert it rather than needing a second row.
		res, err := h.svc.Convert(h.ctx, money.FromMinor(syp, 26_000), usd, day, domain.RateTypeMarket)
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if res.Path != domain.SourceInverse {
			t.Errorf("path = %q, want inverse", res.Path)
		}
		if res.Amount.Minor() != 200 { // 26,000 / 13,000 = $2.00
			t.Errorf("amount = %d, want 200", res.Amount.Minor())
		}
	})

	t.Run("pivot through USD", func(t *testing.T) {
		// No SYP↔EUR rate exists; both legs go through the pivot.
		res, err := h.svc.Convert(h.ctx, money.FromMinor(eur, 100), syp, day, domain.RateTypeMarket)
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if res.Path != domain.SourcePivot {
			t.Errorf("path = %q, want pivot", res.Path)
		}
		// €1.00 → USD (÷0.92) → SYP (×13,000) ≈ 14,130 SYP.
		if res.Amount.Minor() < 14_000 || res.Amount.Minor() > 14_200 {
			t.Errorf("amount = %d, want roughly 14130", res.Amount.Minor())
		}
	})

	t.Run("unknown pair is an error, never a fabricated 1.0", func(t *testing.T) {
		try := h.currency(t, "TRY")
		// Remove the pivot so no path exists.
		_, err := h.svc.Convert(h.ctx, money.FromMinor(try, 100), h.currency(t, "TRY"), day, domain.RateTypeMarket)
		if err != nil {
			t.Fatalf("identity should still work: %v", err)
		}
		// TRY has no rates at all in either direction, and the pivot leg TRY→USD is missing.
		_, err = h.svc.Convert(h.ctx, money.FromMinor(try, 100), eur, day, domain.RateTypeMarket)
		assertCode(t, err, domain.CodeNoRate)
	})
}

func TestPivotRoundsOnceNotTwice(t *testing.T) {
	// The single-rounding rule the numeric kernel was built around, applied to the pivot.
	//
	// The hazard is NOT rounding the two rates — they are already at nano precision. It is
	// converting through an intermediate MONEY: EUR → USD would round to whole cents, and a
	// small amount rounds to zero before the second leg can restore its magnitude. Combining
	// the two rates first and applying them once avoids that entirely.
	h := newHarness(t)
	syp := h.currency(t, "SYP")
	eur := h.currency(t, "EUR")

	h.setRate(t, "EUR", "USD", 333_333_333, day) // ≈ 1/3
	h.setRate(t, "USD", "SYP", 3_000*1_000_000_000, day)

	// €0.01. Via an intermediate USD amount this is $0.0033 → rounds to $0.00 → 0 SYP.
	// Combining first gives a rate of ~999.999999, so €0.01 is ~10 SYP.
	res, err := h.svc.Convert(h.ctx, money.FromMinor(eur, 1), syp, day, domain.RateTypeMarket)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if res.Path != domain.SourcePivot {
		t.Fatalf("path = %q, want pivot", res.Path)
	}
	if res.Amount.Minor() != 10 {
		t.Errorf("amount = %d SYP, want 10 — the pivot was rounded at the intermediate currency, "+
			"destroying the value of a small amount", res.Amount.Minor())
	}
}

// ── THE CORRECTION DRILL (D3) ───────────────────────────────────────────────────

func TestCorrectionOnTheSameDateIsRecordedAndWins(t *testing.T) {
	// This is the test the approved UNIQUE (from, to, rate_type, valid_from) constraint would
	// have failed. A rate entered wrongly and corrected an hour later is the ordinary case,
	// and it must produce a second row rather than an error or an in-place update.
	h := newHarness(t)
	usd := h.currency(t, "USD")
	syp := h.currency(t, "SYP")

	h.setRate(t, "USD", "SYP", 13_000*1_000_000_000, day) // wrong
	h.setRate(t, "USD", "SYP", 15_000*1_000_000_000, day) // corrected, same valid_from

	var rows int
	if err := h.db.WriterPool().QueryRowContext(h.ctx,
		`SELECT COUNT(*) FROM exchange_rates WHERE from_currency='USD' AND to_currency='SYP'`).
		Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("stored %d rows, want 2 — the correction must be appended, not merged", rows)
	}

	res, err := h.svc.Convert(h.ctx, money.FromMinor(usd, 100), syp, day, domain.RateTypeMarket)
	if err != nil {
		t.Fatal(err)
	}
	if res.Amount.Minor() != 15_000 {
		t.Errorf("amount = %d, want 15000 — resolution must take the most recently recorded rate",
			res.Amount.Minor())
	}
}

func TestHistoricalRatesStayReproducible(t *testing.T) {
	// A document dated last week must convert at last week's rate, not today's — the whole
	// reason rates are temporal.
	h := newHarness(t)
	usd := h.currency(t, "USD")
	syp := h.currency(t, "SYP")

	lastWeek := day.AddDate(0, 0, -7)
	h.setRate(t, "USD", "SYP", 10_000*1_000_000_000, lastWeek)
	h.setRate(t, "USD", "SYP", 13_000*1_000_000_000, day)

	past, err := h.svc.Convert(h.ctx, money.FromMinor(usd, 100), syp, lastWeek, domain.RateTypeMarket)
	if err != nil {
		t.Fatal(err)
	}
	if past.Amount.Minor() != 10_000 {
		t.Errorf("historical conversion = %d, want 10000", past.Amount.Minor())
	}

	today, err := h.svc.Convert(h.ctx, money.FromMinor(usd, 100), syp, day, domain.RateTypeMarket)
	if err != nil {
		t.Fatal(err)
	}
	if today.Amount.Minor() != 13_000 {
		t.Errorf("today's conversion = %d, want 13000", today.Amount.Minor())
	}
}

func TestStaleRateIsUsedAndFlagged(t *testing.T) {
	// Offline is normal, not an error (§18.3) — but the caller must be told how old the rate
	// is, because "selling at a three-day-old rate during rapid movement is a real loss".
	h := newHarness(t)
	usd := h.currency(t, "USD")
	syp := h.currency(t, "SYP")

	old := day.AddDate(0, 0, -3)
	h.setRate(t, "USD", "SYP", 13_000*1_000_000_000, old)

	res, err := h.svc.Convert(h.ctx, money.FromMinor(usd, 100), syp, day, domain.RateTypeMarket)
	if err != nil {
		t.Fatalf("a missing current rate must not be an error: %v", err)
	}
	if !res.Stale {
		t.Error("a three-day-old rate was not reported as stale")
	}
	if res.Age < 71*time.Hour {
		t.Errorf("age = %v, want about 72h", res.Age)
	}
	if !res.AsOf.Equal(old) {
		t.Errorf("AsOf = %v, want %v", res.AsOf, old)
	}
}

func TestResultCarriesTheRateForStorage(t *testing.T) {
	// §18.1: every monetary row stores the rate used. A report run next year must not change
	// because a rate was later corrected, which is only possible if the caller got the rate.
	h := newHarness(t)
	usd := h.currency(t, "USD")
	syp := h.currency(t, "SYP")
	h.setRate(t, "USD", "SYP", 13_000*1_000_000_000, day)

	res, err := h.svc.Convert(h.ctx, money.FromMinor(usd, 100), syp, day, domain.RateTypeMarket)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rate.Nano() != 13_000*1_000_000_000 {
		t.Errorf("Rate = %d nano, want the rate actually applied", res.Rate.Nano())
	}
	if res.Source != "manual" {
		t.Errorf("Source = %q, want the stored origin", res.Source)
	}
}

// ── rate types & contexts ───────────────────────────────────────────────────────

func TestContextsResolveTheirOwnRateTypeIndependently(t *testing.T) {
	// A business under currency controls uses the official rate for tax and the market rate
	// for pricing. Without this they would pick one and adjust manually everywhere else.
	h := newHarness(t)
	usd := h.currency(t, "USD")
	syp := h.currency(t, "SYP")

	h.clk.Advance(time.Second)
	if err := h.svc.SetRate(h.ctx, "USD", "SYP", domain.RateTypeMarket,
		money.RateFromNano(15_000*1_000_000_000), day, "manual"); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(time.Second)
	if err := h.svc.SetRate(h.ctx, "USD", "SYP", domain.RateTypeOfficial,
		money.RateFromNano(13_000*1_000_000_000), day, "manual"); err != nil {
		t.Fatal(err)
	}

	amt := money.FromMinor(usd, 100)
	cases := map[currency.ContextKind]int64{
		currency.ContextPricing:    15_000, // market
		currency.ContextSales:      15_000,
		currency.ContextPurchasing: 15_000,
		currency.ContextAccounting: 13_000, // official
		currency.ContextReporting:  13_000,
		currency.ContextTax:        13_000,
	}
	for kind, want := range cases {
		t.Run(string(kind), func(t *testing.T) {
			res, err := h.svc.ConvertForContext(h.ctx, amt, syp, day, kind)
			if err != nil {
				t.Fatalf("ConvertForContext: %v", err)
			}
			if res.Amount.Minor() != want {
				t.Errorf("%s converted to %d, want %d", kind, res.Amount.Minor(), want)
			}
		})
	}
}

func TestUnknownRateTypeIsATypedError(t *testing.T) {
	h := newHarness(t)
	usd := h.currency(t, "USD")
	syp := h.currency(t, "SYP")
	_, err := h.svc.Convert(h.ctx, money.FromMinor(usd, 100), syp, day, "nonexistent")
	assertCode(t, err, domain.CodeUnknownRateType)
}

// ── redenomination ──────────────────────────────────────────────────────────────

// redenominate makes `from` succeed into `to` with a ×10⁹ factor.
func (h *harness) redenominate(t *testing.T, from, to string, factorNano int64) {
	t.Helper()
	if _, err := h.db.WriterPool().ExecContext(h.ctx,
		`UPDATE currencies SET succeeded_by_code = ?, redenomination_factor = ?, is_historical = 1
		  WHERE code = ?`, to, factorNano, from); err != nil {
		t.Fatalf("redenominate %s: %v", from, err)
	}
}

func TestRedenominationAppliesTheFixedFactor(t *testing.T) {
	// An old-unit invoice keeps its amount forever; this restates it for a report spanning
	// the changeover (§G.2).
	h := newHarness(t)
	// Pretend SYP was redenominated into TRY at 1/1000 (removing three zeros).
	h.redenominate(t, "SYP", "TRY", 1_000_000) // 0.001 × 10⁹

	syp := h.currency(t, "SYP")
	got, err := h.svc.ResolveRedenomination(h.ctx, money.FromMinor(syp, 500_000))
	if err != nil {
		t.Fatalf("ResolveRedenomination: %v", err)
	}
	if got.Currency().Code() != "TRY" {
		t.Errorf("currency = %q, want TRY", got.Currency().Code())
	}
	// 500,000 old units × 0.001 = 500 new units; TRY has 2 decimals so that is 50000 minor.
	if got.Minor() != 50_000 {
		t.Errorf("amount = %d minor, want 50000", got.Minor())
	}
}

func TestRedenominationChainWalksToTheEnd(t *testing.T) {
	h := newHarness(t)
	h.redenominate(t, "SYP", "EUR", 1_000_000_00) // ×0.1
	h.redenominate(t, "EUR", "TRY", 1_000_000_00) // ×0.1

	syp := h.currency(t, "SYP")
	got, err := h.svc.ResolveRedenomination(h.ctx, money.FromMinor(syp, 10_000))
	if err != nil {
		t.Fatalf("ResolveRedenomination: %v", err)
	}
	if got.Currency().Code() != "TRY" {
		t.Errorf("currency = %q, want TRY at the end of the chain", got.Currency().Code())
	}
}

func TestRedenominationLoopIsDetected(t *testing.T) {
	// A data-entry mistake making A succeed B and B succeed A would otherwise hang the report
	// that discovered it.
	h := newHarness(t)
	h.redenominate(t, "SYP", "TRY", 1_000_000_000)
	h.redenominate(t, "TRY", "SYP", 1_000_000_000)

	syp := h.currency(t, "SYP")
	_, err := h.svc.ResolveRedenomination(h.ctx, money.FromMinor(syp, 100))
	assertCode(t, err, domain.CodeRedenominationLoop)
}

func TestCurrencyWithoutSuccessorIsReturnedUnchanged(t *testing.T) {
	h := newHarness(t)
	usd := h.currency(t, "USD")
	original := money.FromMinor(usd, 12_345)
	got, err := h.svc.ResolveRedenomination(h.ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	if got.Minor() != original.Minor() || got.Currency().Code() != "USD" {
		t.Errorf("got %v, want the amount unchanged", got)
	}
}

// ── validation & properties ─────────────────────────────────────────────────────

func TestSetRateRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	cases := map[string]func() error{
		"unknown from currency": func() error {
			return h.svc.SetRate(h.ctx, "ZZZ", "USD", domain.RateTypeMarket,
				money.RateFromNano(1), day, "manual")
		},
		"unknown to currency": func() error {
			return h.svc.SetRate(h.ctx, "USD", "ZZZ", domain.RateTypeMarket,
				money.RateFromNano(1), day, "manual")
		},
		"same currency": func() error {
			return h.svc.SetRate(h.ctx, "USD", "USD", domain.RateTypeMarket,
				money.RateFromNano(1_000_000_000), day, "manual")
		},
		"non-positive rate": func() error {
			return h.svc.SetRate(h.ctx, "USD", "SYP", domain.RateTypeMarket,
				money.RateFromNano(0), day, "manual")
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			if err := fn(); err == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
}

func TestUnknownCurrencyIsATypedError(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.Get(h.ctx, "ZZZ")
	assertCode(t, err, domain.CodeUnknownCurrency)
}

func TestConversionIsExactBeyondFloatPrecision(t *testing.T) {
	// The property that actually catches a float: 2^53 is where float64 stops representing
	// consecutive integers, and a SYP amount passes it easily. At an exact integer rate the
	// result must be exact, with no drift at any magnitude.
	h := newHarness(t)
	usd := h.currency(t, "USD")
	syp := h.currency(t, "SYP")
	h.setRate(t, "USD", "SYP", 13_000*1_000_000_000, day)

	rapid.Check(t, func(rt *rapid.T) {
		// Up to ~$1e12, which converts to ~1.3e16 SYP — well past 2^53 (≈9.0e15).
		cents := rapid.Int64Range(1, 100_000_000_000_000).Draw(rt, "cents")
		res, err := h.svc.Convert(h.ctx, money.FromMinor(usd, cents), syp, day, domain.RateTypeMarket)
		if err != nil {
			rt.Fatalf("Convert: %v", err)
		}
		// USD has 2 decimals and SYP has 0, so cents × 13,000 / 100 = cents × 130, exactly.
		want := cents * 130
		if res.Amount.Minor() != want {
			rt.Fatalf("converted %d cents to %d, want exactly %d (drift of %d)",
				cents, res.Amount.Minor(), want, res.Amount.Minor()-want)
		}
	})
}

func TestRoundTripDriftIsBoundedByInverseRatePrecision(t *testing.T) {
	// Converting out and back does NOT return the exact original, and the reason is worth
	// pinning rather than wishing away: the inverse of 13,000 is 0.0000769230769…, which a
	// ×10⁹ integer can only hold as 76,923 — a relative error of about 1e-6. On a large
	// amount that is more than one minor unit.
	//
	// This is inherent to storing rates as fixed-point integers, and it is the right
	// trade-off: exactness of the STORED rate matters more than reversibility, because the
	// stored rate is what goes on the document. The test asserts the error stays proportional,
	// which is what would break if anything started accumulating error instead.
	h := newHarness(t)
	usd := h.currency(t, "USD")
	syp := h.currency(t, "SYP")
	h.setRate(t, "USD", "SYP", 13_000*1_000_000_000, day)

	rapid.Check(t, func(rt *rapid.T) {
		cents := rapid.Int64Range(1, 100_000_000_00).Draw(rt, "cents")
		original := money.FromMinor(usd, cents)

		toSYP, err := h.svc.Convert(h.ctx, original, syp, day, domain.RateTypeMarket)
		if err != nil {
			rt.Fatalf("to SYP: %v", err)
		}
		back, err := h.svc.Convert(h.ctx, toSYP.Amount, usd, day, domain.RateTypeMarket)
		if err != nil {
			rt.Fatalf("back to USD: %v", err)
		}

		diff := back.Amount.Minor() - cents
		if diff < 0 {
			diff = -diff
		}
		// 1e-5 relative is an order of magnitude above the ~6.5e-6 the inverse can lose, and
		// far below what any accumulating or float-based error would produce.
		allowed := cents/100_000 + 2
		if diff > allowed {
			rt.Fatalf("round trip drifted by %d minor units on %d cents (allowed %d)",
				diff, cents, allowed)
		}
	})
}

// ── module contract ─────────────────────────────────────────────────────────────

func TestModuleDeclaresItsSurface(t *testing.T) {
	h := newHarness(t)

	if h.mod.Name() != "currency" {
		t.Errorf("Name = %q", h.mod.Name())
	}
	if len(h.mod.DependsOn()) != 0 {
		t.Errorf("DependsOn = %v, want none", h.mod.DependsOn())
	}
	// Three roles + six context bindings.
	if got := len(h.mod.Settings()); got != 9 {
		t.Errorf("Settings = %d, want 9", got)
	}
	if got := len(h.mod.Metadata()); got != 2 {
		t.Errorf("Metadata = %d seed sets, want 2", got)
	}
	if err := h.mod.Subscribe(nil, nil); err != nil {
		t.Errorf("Subscribe = %v, want nil (no subscriber exists yet)", err)
	}
}

func TestRateProviderRegistryIsEmptyButUsable(t *testing.T) {
	// The port and registry exist; no network provider ships (D7).
	reg := currency.RateProviders()
	if reg.Len() != 0 {
		t.Errorf("registry has %d providers; none should ship", reg.Len())
	}
	if _, err := reg.Resolve("anything"); err == nil {
		t.Error("resolving an unregistered provider should be a typed error, not a zero value")
	}
}
