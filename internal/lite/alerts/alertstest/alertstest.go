// Package alertstest is the engine's in-memory snapshot store and the contract every Store must meet — run against this
// fake AND the real SQLite store, so the fake cannot quietly behave differently from the database it stands in for.
package alertstest

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/lite/alerts"
	"github.com/mizan-erp/mizan/internal/lite/alerts/domain"
)

// Fake is an in-memory alerts.Store.
type Fake struct {
	mu    sync.Mutex
	days  map[string]domain.Snapshot
	taken map[string]time.Time
}

// NewFake is an empty store.
func NewFake() *Fake { return &Fake{days: map[string]domain.Snapshot{}, taken: map[string]time.Time{}} }

func (f *Fake) Put(_ context.Context, s domain.Snapshot, takenAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.days[s.BusinessDate], f.taken[s.BusinessDate] = s, takenAt.UTC().Truncate(time.Millisecond)
	return nil
}

func (f *Fake) Get(_ context.Context, date string) (domain.Snapshot, time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.days[date]
	return s, f.taken[date], ok, nil
}

func (f *Fake) OnOrBefore(_ context.Context, date string) (domain.Snapshot, bool, error) {
	for i, d := range f.sorted() {
		_ = i
		if d > date {
			continue
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.days[d], true, nil
	}
	return domain.Snapshot{}, false, nil
}

func (f *Fake) First(_ context.Context) (domain.Snapshot, bool, error) {
	dates := f.sorted()
	if len(dates) == 0 {
		return domain.Snapshot{}, false, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.days[dates[len(dates)-1]], true, nil
}

// sorted is every date, newest first.
func (f *Fake) sorted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.days))
	for d := range f.days {
		out = append(out, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

// StoreContract is what every alerts.Store must do.
func StoreContract(t *testing.T, newStore func(t *testing.T) alerts.Store) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	day := func(date string, stock int64) domain.Snapshot {
		return domain.Snapshot{BusinessDate: date, RateNano: 15_000_000_000_000, LocalCurrency: "SYP", StockUSDMinor: stock,
			CashUSDMinor: 1_000, CashLocalMinor: -500, OwedUSDMinor: 250, OwedLocalMinor: 75_000,
			PayableUSDMinor: 4_200, PayableLocalMinor: -30_000}
	}

	t.Run("a day reads back exactly, with when it was taken", func(t *testing.T) {
		s := newStore(t)
		want := day("2026-09-20", 123_456)
		if err := s.Put(ctx, want, at); err != nil {
			t.Fatal(err)
		}
		got, taken, found, err := s.Get(ctx, "2026-09-20")
		if err != nil || !found || got != want || !taken.Equal(at) {
			t.Fatalf("Get = %+v, %v, %v, %v", got, taken, found, err)
		}
		// A negative drawer is kept as it is: an owner who took out more than the drawer held is a fact.
		if got.CashLocalMinor != -500 {
			t.Fatalf("a negative drawer read back as %d", got.CashLocalMinor)
		}
	})

	t.Run("a day is rewritten, never duplicated", func(t *testing.T) {
		s := newStore(t)
		_ = s.Put(ctx, day("2026-09-20", 100), at)
		_ = s.Put(ctx, day("2026-09-20", 200), at.Add(time.Hour))
		got, taken, _, _ := s.Get(ctx, "2026-09-20")
		if got.StockUSDMinor != 200 || !taken.Equal(at.Add(time.Hour)) {
			t.Fatalf("the rewrite was lost: %+v at %v", got, taken)
		}
	})

	t.Run("the newest on or before a date, and the first ever", func(t *testing.T) {
		s := newStore(t)
		for _, d := range []string{"2026-08-01", "2026-08-20", "2026-09-10"} {
			_ = s.Put(ctx, day(d, 1), at)
		}
		for date, want := range map[string]string{"2026-08-25": "2026-08-20", "2026-08-20": "2026-08-20", "2026-12-31": "2026-09-10"} {
			got, found, err := s.OnOrBefore(ctx, date)
			if err != nil || !found || got.BusinessDate != want {
				t.Errorf("OnOrBefore(%s) = %s, %v, %v; want %s", date, got.BusinessDate, found, err, want)
			}
		}
		if _, found, _ := s.OnOrBefore(ctx, "2026-07-31"); found {
			t.Error("a snapshot was found before the first one")
		}
		if first, found, _ := s.First(ctx); !found || first.BusinessDate != "2026-08-01" {
			t.Errorf("First = %s, %v", first.BusinessDate, found)
		}
	})

	t.Run("an empty store has nothing", func(t *testing.T) {
		s := newStore(t)
		if _, _, found, _ := s.Get(ctx, "2026-09-20"); found {
			t.Error("Get found a day in an empty store")
		}
		if _, found, _ := s.First(ctx); found {
			t.Error("First found a day in an empty store")
		}
	})
}
