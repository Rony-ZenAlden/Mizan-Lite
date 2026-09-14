package fxtest

import (
	"context"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/fx"
	"github.com/mizan-erp/mizan/internal/lite/fx/domain"
)

const n = int64(1_000_000_000)

// StoreContract is the behaviour every fx.Store must have. newStore returns a fresh, empty store.
func StoreContract(t *testing.T, newStore func(t *testing.T) fx.Store) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC)
	newID := func(t *testing.T) id.ID {
		t.Helper()
		v, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	rate := func(t *testing.T, seq, nano int64, source domain.Source, fetchID id.ID) domain.Rate {
		return domain.Rate{
			ID: newID(t), LocalCurrency: "SYP", Seq: seq, Nano: nano, Source: source, FetchID: fetchID,
			BusinessDate: "2026-09-14", RecordedAt: at.Add(time.Duration(seq) * time.Minute), Note: "ملاحظة",
		}
	}
	fetch := func(t *testing.T, seq int64, outcome domain.Outcome, against id.ID) domain.Fetch {
		f := domain.Fetch{
			ID: newID(t), LocalCurrency: "SYP", Seq: seq, AttemptedAt: at.Add(time.Duration(seq) * time.Second),
			BusinessDate: "2026-09-14", Outcome: outcome, AgainstRateID: against,
		}
		if outcome == domain.OutcomeFailed {
			f.ErrorCode = domain.CodeFetchOffline
		} else {
			f.Provider, f.Nano = "currency-api-jsdelivr", 13_007_535_500_000
		}
		return f
	}

	t.Run("nothing recorded is found=false, never a zero rate", func(t *testing.T) {
		s := newStore(t)
		if r, found, err := s.InForce(ctx, "SYP"); err != nil || found || r != (domain.Rate{}) {
			t.Fatalf("InForce = %+v %v %v", r, found, err)
		}
		if f, found, err := s.LastFetch(ctx, "SYP"); err != nil || found || f != (domain.Fetch{}) {
			t.Fatalf("LastFetch = %+v %v %v", f, found, err)
		}
		missing := newID(t)
		if _, err := s.Fetch(ctx, missing); errs.CodeOf(err) != domain.CodeFetchNotFound {
			t.Fatalf("Fetch(missing) = %v", err)
		}
	})

	t.Run("rates and fetches read back exactly; the highest place is in force whatever the clock says", func(t *testing.T) {
		s := newStore(t)
		first := rate(t, 1, 14_800*n, domain.SourceFirstRun, "")
		if err := s.AppendRate(ctx, first); err != nil {
			t.Fatal(err)
		}
		proposed := fetch(t, 1, domain.OutcomeProposed, first.ID)
		failed := fetch(t, 2, domain.OutcomeFailed, first.ID)
		applied := fetch(t, 3, domain.OutcomeApplied, first.ID)
		for _, f := range []domain.Fetch{proposed, failed, applied} {
			if err := s.AppendFetch(ctx, f); err != nil {
				t.Fatalf("AppendFetch(%s): %v", f.Outcome, err)
			}
		}
		second := rate(t, 2, 13_007_535_500_000, domain.SourceFetched, applied.ID)
		// Recorded with a clock set back a day: still the rate in force, because it holds the higher place.
		second.RecordedAt = at.Add(-24 * time.Hour)
		if err := s.AppendRate(ctx, second); err != nil {
			t.Fatal(err)
		}
		manual := rate(t, 3, 15_000*n, domain.SourceManual, "")
		manual.Note = ""
		// The newest place carries the OLDEST timestamp of all: a clock set back two days. It is still in force.
		manual.RecordedAt = at.Add(-48 * time.Hour)
		if err := s.AppendRate(ctx, manual); err != nil {
			t.Fatal(err)
		}

		got, found, err := s.InForce(ctx, "SYP")
		if err != nil || !found || !sameRate(got, manual) {
			t.Fatalf("InForce = %+v, %v\nwant %+v", got, err, manual)
		}
		rates, err := s.Rates(ctx, "SYP", 10)
		if err != nil || len(rates) != 3 || !sameRate(rates[0], manual) || !sameRate(rates[1], second) || !sameRate(rates[2], first) {
			t.Fatalf("Rates = %+v, %v", rates, err)
		}
		if limited, _ := s.Rates(ctx, "SYP", 2); len(limited) != 2 {
			t.Fatalf("limit not applied: %d", len(limited))
		}
		for _, want := range []domain.Fetch{proposed, failed, applied} {
			if got, err := s.Fetch(ctx, want.ID); err != nil || !sameFetch(got, want) {
				t.Errorf("Fetch = %+v, %v\nwant %+v", got, err, want)
			}
		}
		if last, found, _ := s.LastFetch(ctx, "SYP"); !found || !sameFetch(last, applied) {
			t.Fatalf("LastFetch = %+v", last)
		}
		if other, found, _ := s.InForce(ctx, "SYN"); found {
			t.Fatalf("another currency found %+v", other)
		}
	})

	t.Run("one place per rate and per fetch; a fetch applied once", func(t *testing.T) {
		s := newStore(t)
		first := rate(t, 1, 14_800*n, domain.SourceManual, "")
		if err := s.AppendRate(ctx, first); err != nil {
			t.Fatal(err)
		}
		if err := s.AppendRate(ctx, rate(t, 1, 15_000*n, domain.SourceManual, "")); err == nil {
			t.Error("two rates took place 1")
		}
		f := fetch(t, 1, domain.OutcomeApplied, first.ID)
		if err := s.AppendFetch(ctx, f); err != nil {
			t.Fatal(err)
		}
		if err := s.AppendFetch(ctx, fetch(t, 1, domain.OutcomeFailed, "")); err == nil {
			t.Error("two fetches took place 1")
		}
		if err := s.AppendRate(ctx, rate(t, 2, 13_000*n, domain.SourceFetched, f.ID)); err != nil {
			t.Fatal(err)
		}
		if err := s.AppendRate(ctx, rate(t, 3, 13_000*n, domain.SourceFetched, f.ID)); err == nil {
			t.Error("one fetch was applied twice")
		}
		if err := s.AppendRate(ctx, rate(t, 4, 13_000*n, domain.SourceFetched, "")); err == nil {
			t.Error("a fetched rate without its fetch was stored")
		}
		if err := s.AppendRate(ctx, rate(t, 5, 13_000*n, domain.SourceManual, f.ID)); err == nil {
			t.Error("a manual rate naming a fetch was stored")
		}
	})
}

func sameRate(a, b domain.Rate) bool {
	if !a.RecordedAt.Equal(b.RecordedAt) {
		return false
	}
	a.RecordedAt, b.RecordedAt = time.Time{}, time.Time{}
	return a == b
}

func sameFetch(a, b domain.Fetch) bool {
	if !a.AttemptedAt.Equal(b.AttemptedAt) {
		return false
	}
	a.AttemptedAt, b.AttemptedAt = time.Time{}, time.Time{}
	return a == b
}
