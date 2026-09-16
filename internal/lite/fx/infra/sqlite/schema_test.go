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
	"github.com/mizan-erp/mizan/internal/lite/fx"
	"github.com/mizan-erp/mizan/internal/lite/fx/domain"
	"github.com/mizan-erp/mizan/internal/lite/fx/fxtest"
	"github.com/mizan-erp/mizan/internal/lite/fx/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// TestRatesAreInsertOnly reads the store's source: the only statements against either table are INSERT and SELECT.
func TestRatesAreInsertOnly(t *testing.T) {
	raw, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatal(err)
	}
	src := strings.ToUpper(string(raw))
	if !strings.Contains(src, "INSERT INTO FX_RATES") || !strings.Contains(src, "INSERT INTO FX_FETCHES") {
		t.Fatal("the scan found no inserts; it is not reading the store")
	}
	for _, forbidden := range []string{`UPDATE\s+FX_`, `DELETE\s+FROM\s+FX_`, `REPLACE\s+INTO\s+FX_`, `ON\s+CONFLICT`} {
		if regexp.MustCompile(forbidden).MatchString(src) {
			t.Errorf("the store rewrites rates: %s", forbidden)
		}
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

func uid() string {
	v, err := id.New()
	if err != nil {
		panic(err) //nolint:forbidigo // entropy failure ends the test run
	}
	return v.String()
}

// TestACheckRefusesAnImpossibleRate inserts, for each constraint in 0004_fx.sql, a row only that constraint forbids —
// NULL cases included — and asserts WHICH constraint refused it (L2 R3).
func TestACheckRefusesAnImpossibleRate(t *testing.T) {
	db := litetest.OpenMigrated(t)
	seq := 0
	next := func() int { seq++; return seq }

	baseRate := func() row {
		return row{"id": uid(), "local_currency": "SYP", "seq": next(), "local_per_usd_nano": int64(15_000_000_000_000),
			"source": "manual", "fetch_id": nil, "business_date": "2026-09-14", "recorded_at": "2026-09-14T06:00:00.000Z", "note": nil}
	}
	baseFetch := func() row {
		return row{"id": uid(), "local_currency": "SYP", "seq": next(), "attempted_at": "2026-09-14T06:00:00.000Z",
			"business_date": "2026-09-14", "outcome": "applied", "provider": "currency-api-jsdelivr",
			"local_per_usd_nano": int64(13_007_535_500_000), "against_rate_id": nil, "error_code": nil}
	}
	failedFetch := row{"outcome": "failed", "provider": nil, "local_per_usd_nano": nil, "error_code": "lite.fx.fetch_offline"}

	rate := baseRate()
	if err := insert(db, "fx_rates", rate); err != nil {
		t.Fatalf("a valid manual rate: %v", err)
	}
	fetch := baseFetch().with(row{"against_rate_id": rate["id"]})
	if err := insert(db, "fx_fetches", fetch); err != nil {
		t.Fatalf("a valid applied fetch: %v", err)
	}
	for name, r := range map[string]row{
		"a fetched rate naming its fetch": baseRate().with(row{"source": "fetched", "fetch_id": fetch["id"]}),
		"a first-run rate with a note":    baseRate().with(row{"source": "first_run", "note": "أول تشغيل"}),
	} {
		if err := insert(db, "fx_rates", r); err != nil {
			t.Errorf("accepts %s: %v", name, err)
		}
	}
	for name, r := range map[string]row{
		"a failed fetch with its code": baseFetch().with(failedFetch),
		"a proposal against no rate":   baseFetch().with(row{"outcome": "proposed"}),
		"a held fetch":                 baseFetch().with(row{"outcome": "held_today", "against_rate_id": rate["id"]}),
	} {
		if err := insert(db, "fx_fetches", r); err != nil {
			t.Errorf("accepts %s: %v", name, err)
		}
	}

	for _, tc := range []struct {
		name, table string
		r           row
		constraint  string
	}{
		{"a USD local currency", "fx_rates", baseRate().with(row{"local_currency": "USD"}), "ck_fx_rates_local_is_not_usd"},
		{"a currency that does not exist", "fx_rates", baseRate().with(row{"local_currency": "EUR"}), "FOREIGN KEY"},
		{"a zero rate", "fx_rates", baseRate().with(row{"local_per_usd_nano": 0}), "local_per_usd_nano > 0"},
		{"a NULL rate", "fx_rates", baseRate().with(row{"local_per_usd_nano": nil}), "NOT NULL"},
		{"place zero", "fx_rates", baseRate().with(row{"seq": 0}), "seq >= 1"},
		{"two rates in one place", "fx_rates", baseRate().with(row{"seq": rate["seq"]}), "UNIQUE"},
		{"an unknown source", "fx_rates", baseRate().with(row{"source": "guessed"}), "source IN"},
		{"a fetched rate without its fetch (NULL)", "fx_rates", baseRate().with(row{"source": "fetched"}), "ck_fx_rates_fetched_names_its_fetch"},
		{"a manual rate naming a fetch", "fx_rates", baseRate().with(row{"fetch_id": fetch["id"]}), "ck_fx_rates_fetched_names_its_fetch"},
		{"a fetch applied twice", "fx_rates", baseRate().with(row{"source": "fetched", "fetch_id": fetch["id"]}), "UNIQUE"},
		{"a fetch that does not exist", "fx_rates", baseRate().with(row{"source": "fetched", "fetch_id": uid()}), "FOREIGN KEY"},
		{"an unknown outcome", "fx_fetches", baseFetch().with(row{"outcome": "guessed"}), "outcome IN"},
		{"an answer with no provider (NULL)", "fx_fetches", baseFetch().with(row{"provider": nil}), "ck_fx_fetch_answered"},
		{"an answer with no rate (NULL)", "fx_fetches", baseFetch().with(row{"local_per_usd_nano": nil}), "ck_fx_fetch_answered"},
		{"an answer at a zero rate", "fx_fetches", baseFetch().with(row{"local_per_usd_nano": 0}), "ck_fx_fetch_answered"},
		{"an answer carrying an error code", "fx_fetches", baseFetch().with(row{"error_code": "lite.fx.fetch_failed"}), "ck_fx_fetch_answered"},
		{"a failure with no code (NULL)", "fx_fetches", baseFetch().with(failedFetch).with(row{"error_code": nil}), "ck_fx_fetch_answered"},
		{"a failure naming a provider", "fx_fetches", baseFetch().with(failedFetch).with(row{"provider": "p"}), "ck_fx_fetch_answered"},
		{"a failure carrying a rate", "fx_fetches", baseFetch().with(failedFetch).with(row{"local_per_usd_nano": 1}), "ck_fx_fetch_answered"},
		{"two fetches in one place", "fx_fetches", baseFetch().with(row{"seq": fetch["seq"]}), "UNIQUE"},
		{"a comparison with a rate that does not exist", "fx_fetches", baseFetch().with(row{"against_rate_id": uid()}), "FOREIGN KEY"},
	} {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			err := insert(db, tc.table, tc.r)
			if err == nil {
				t.Fatalf("an impossible row was stored: %v", tc.r)
			}
			if !strings.Contains(err.Error(), tc.constraint) {
				t.Fatalf("refused, but not by %s: %v", tc.constraint, err)
			}
		})
	}
}

// failingRates fails AppendRate: in Refresh, after the fetch was appended.
type failingRates struct{ *sqlite.Store }

var errRate = errors.New("the rate write failed")

func (failingRates) AppendRate(context.Context, domain.Rate) error { return errRate }

// TestAnAppliedFetchAndItsRateCommitTogether: with the rate write failing after the fetch was logged, the real
// transaction leaves neither.
func TestAnAppliedFetchAndItsRateCommitTogether(t *testing.T) {
	ctx := context.Background()
	db := litetest.OpenMigrated(t)
	store := sqlite.NewStore(db)
	settings, gate, source := fxtest.NewSettings(), &fxtest.Gate{Elevated: true}, &fxtest.Source{}
	settings.Mode = domain.ModeAutomatic // so a fetch applies a rate
	clk := clock.NewFixed(time.Date(2026, 9, 13, 6, 0, 0, 0, time.UTC))
	ok := fx.NewService(db, store, settings, gate, source, clk, time.UTC)
	if _, err := ok.SetRate(ctx, fx.SetRateInput{Rate: "13000"}); err != nil {
		t.Fatal(err)
	}
	// Typing a rate now puts the shop in manual mode (2026-09-17). This test is about a fetch and its rate committing
	// together, so the shop is put back into automatic to let one apply.
	settings.Mode = domain.ModeAutomatic
	clk.Advance(24 * time.Hour)
	source.Answer("p", 13_100_000_000_000)

	failing := fx.NewService(db, failingRates{store}, settings, gate, source, clk, time.UTC)
	if _, err := failing.Refresh(ctx); !errors.Is(err, errRate) {
		t.Fatalf("err = %v", err)
	}
	if _, found, _ := store.LastFetch(ctx, "SYP"); found {
		t.Fatal("the fetch survived a failed rate write")
	}
	got, err := ok.Refresh(ctx)
	if err != nil || got.Outcome != domain.OutcomeApplied {
		t.Fatalf("refresh = %+v, %v", got, err)
	}
	if r, _, _ := store.InForce(ctx, "SYP"); r.FetchID != got.ID || r.Seq != 2 {
		t.Fatalf("in force = %+v", r)
	}
}
