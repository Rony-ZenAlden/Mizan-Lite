// Package settingstest is the in-memory settings store and the contract every store must meet.
//
// # Why a fake AND a contract
//
// A fake store makes the service's rules testable without a database: fast, and able to inject a
// failure on demand. But a fake is only as trustworthy as its resemblance to the real thing, and
// nothing keeps that resemblance by default — the classic failure is a mock that returns what the
// test hoped for while the SQL is wrong.
//
// So StoreContract is written once and run against BOTH the fake and the SQLite store. If the
// fake ever drifts from the database's behaviour, the contract fails on one of them.
package settingstest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/lite/settings"
)

// ErrInjected is what a Fake returns once a failure has been armed.
var ErrInjected = errors.New("settingstest: injected failure")

// Fake is an in-memory settings.Store.
type Fake struct {
	mu        sync.Mutex
	rows      map[string]string
	failLoad  bool
	failSave  bool
	saveCalls int
}

// NewFake returns an empty store.
func NewFake() *Fake { return &Fake{rows: map[string]string{}} }

// Seed writes rows directly, bypassing validation — the way damaged or foreign data arrives.
func (f *Fake) Seed(rows map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, v := range rows {
		f.rows[k] = v
	}
}

// FailLoad makes every Load fail.
func (f *Fake) FailLoad() { f.mu.Lock(); f.failLoad = true; f.mu.Unlock() }

// FailSave makes every Save fail.
func (f *Fake) FailSave() { f.mu.Lock(); f.failSave = true; f.mu.Unlock() }

// SaveCalls reports how many times Save was called, successful or not.
func (f *Fake) SaveCalls() int { f.mu.Lock(); defer f.mu.Unlock(); return f.saveCalls }

// Load implements settings.Store. It returns a copy, as the database does: a caller mutating the
// map it received must not be able to change what is stored.
func (f *Fake) Load(context.Context) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failLoad {
		return nil, ErrInjected
	}
	out := make(map[string]string, len(f.rows))
	for k, v := range f.rows {
		out[k] = v
	}
	return out, nil
}

// Save implements settings.Store.
func (f *Fake) Save(_ context.Context, key, value string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveCalls++
	if f.failSave {
		return ErrInjected
	}
	f.rows[key] = value
	return nil
}

// Immediate is a Transactor that runs fn with no transaction, for service tests over a Fake.
type Immediate struct{}

// Do implements settings.Transactor.
func (Immediate) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

// StoreContract is the behaviour every settings.Store must have. newStore returns a fresh, empty
// store for each subtest.
func StoreContract(t *testing.T, newStore func(t *testing.T) settings.Store) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)

	t.Run("an empty store loads no rows, not nil", func(t *testing.T) {
		rows, err := newStore(t).Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if rows == nil || len(rows) != 0 {
			t.Fatalf("Load = %#v, want an empty non-nil map", rows)
		}
	})

	t.Run("a saved row loads back", func(t *testing.T) {
		s := newStore(t)
		if err := s.Save(ctx, "ui.locale", "en", at); err != nil {
			t.Fatalf("Save: %v", err)
		}
		rows, err := s.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if rows["ui.locale"] != "en" || len(rows) != 1 {
			t.Fatalf("Load = %#v", rows)
		}
	})

	t.Run("saving an existing key replaces it rather than adding a second row", func(t *testing.T) {
		s := newStore(t)
		for _, v := range []string{"en", "ar", "en"} {
			if err := s.Save(ctx, "ui.locale", v, at); err != nil {
				t.Fatalf("Save %q: %v", v, err)
			}
		}
		rows, err := s.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(rows) != 1 || rows["ui.locale"] != "en" {
			t.Fatalf("Load = %#v, want exactly one row holding the last value", rows)
		}
	})

	t.Run("values are stored exactly, including Arabic and empty strings", func(t *testing.T) {
		s := newStore(t)
		want := map[string]string{"a": "ميزان لايت", "b": "", "c": "line\nbreak"}
		for k, v := range want {
			if err := s.Save(ctx, k, v, at); err != nil {
				t.Fatalf("Save %q: %v", k, err)
			}
		}
		rows, err := s.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		for k, v := range want {
			if rows[k] != v {
				t.Errorf("row %q = %q, want %q", k, rows[k], v)
			}
		}
	})

	t.Run("mutating a loaded map does not change the store", func(t *testing.T) {
		s := newStore(t)
		if err := s.Save(ctx, "ui.locale", "en", at); err != nil {
			t.Fatalf("Save: %v", err)
		}
		rows, err := s.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		rows["ui.locale"] = "tampered"
		again, err := s.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if again["ui.locale"] != "en" {
			t.Fatalf("store changed through a loaded map: %q", again["ui.locale"])
		}
	})
}
