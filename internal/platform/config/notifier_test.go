// External test package so the notifier can be exercised against the real event bus,
// closing the ChangeNotifier port left open in Step 0.5.
package config_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/platform/config"
	"github.com/mizan-erp/mizan/internal/platform/database"
	"github.com/mizan-erp/mizan/internal/platform/eventbus"
	"github.com/mizan-erp/mizan/internal/platform/migrate"
	"github.com/mizan-erp/mizan/migrations"
)

func migratedStore(t *testing.T) *database.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "notifier_test.db")
	st, err := database.Open(database.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	runner, err := migrate.New(st, migrate.Options{
		FS: migrations.SQLite(), DBPath: dbPath, SkipBackup: true,
	})
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	if _, err := runner.Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func TestSettingWriteReachesTheDomainBus(t *testing.T) {
	// This is what makes "changing the language takes effect without a restart" real
	// (ARCHITECTURE_v1 §16.3) rather than aspirational.
	ctx := context.Background()
	st := migratedStore(t)

	bus := eventbus.New(eventbus.Options{})
	var got config.SettingChangedEvent
	calls := 0
	if err := eventbus.Subscribe(bus, "locale-reload",
		func(_ context.Context, e config.SettingChangedEvent) error {
			got = e
			calls++
			return nil
		}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	reg := config.NewRegistry()
	reg.DeclareString(config.Def{
		Key: "ui.language", Default: "en",
		Scopes: []config.Scope{config.ScopeSystem, config.ScopeUser},
	})
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry: %v", err)
	}

	settings, err := config.Open(ctx, st, config.Options{
		Registry: reg,
		Notifier: config.NewBusNotifier(bus, nil),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := settings.Set(config.Bind(ctx, settings), config.ScopeSystem, "", "ui.language", "ar"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if calls != 1 {
		t.Fatalf("subscriber invoked %d times, want 1", calls)
	}
	if got.Key != "ui.language" || got.Scope != config.ScopeSystem {
		t.Errorf("event = %+v, want key ui.language at system scope", got)
	}
	if got.EventType() != "config.setting_changed" {
		t.Errorf("event type = %q", got.EventType())
	}
}

func TestFlagWriteAlsoNotifies(t *testing.T) {
	ctx := context.Background()
	st := migratedStore(t)

	bus := eventbus.New(eventbus.Options{})
	calls := 0
	if err := eventbus.Subscribe(bus, "nav-refresh",
		func(context.Context, config.SettingChangedEvent) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}

	reg := config.NewRegistry()
	flag := reg.DeclareFlag(config.FlagDef{
		Key: "accounting.ui", Scopes: []config.Scope{config.ScopeSystem},
	})
	settings, err := config.Open(ctx, st, config.Options{
		Registry: reg, Notifier: config.NewBusNotifier(bus, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.SetFlag(config.Bind(ctx, settings), config.ScopeSystem, "", flag.Key(), true); err != nil {
		t.Fatalf("SetFlag: %v", err)
	}
	if calls != 1 {
		t.Errorf("subscriber invoked %d times, want 1", calls)
	}
}

func TestSubscriberFailureDoesNotUndoTheWrite(t *testing.T) {
	// The port returns nothing on purpose: a UI component failing to refresh must not roll
	// back a perfectly valid settings change.
	ctx := context.Background()
	st := migratedStore(t)

	bus := eventbus.New(eventbus.Options{})
	if err := eventbus.Subscribe(bus, "broken",
		func(context.Context, config.SettingChangedEvent) error {
			return errors.New("the view is not mounted")
		}); err != nil {
		t.Fatal(err)
	}

	reg := config.NewRegistry()
	handle := reg.DeclareString(config.Def{Key: "ui.language", Default: "en",
		Scopes: []config.Scope{config.ScopeSystem}})
	settings, err := config.Open(ctx, st, config.Options{
		Registry: reg, Notifier: config.NewBusNotifier(bus, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	bound := config.Bind(ctx, settings)

	if err := settings.Set(bound, config.ScopeSystem, "", "ui.language", "ar"); err != nil {
		t.Fatalf("a failing subscriber rolled back the write: %v", err)
	}
	if got := handle.Get(bound); got != "ar" {
		t.Errorf("value = %q, want the write to stand at %q", got, "ar")
	}
}

func TestSettingChangedCarriesScopeID(t *testing.T) {
	ctx := context.Background()
	st := migratedStore(t)

	bus := eventbus.New(eventbus.Options{})
	var seen config.SettingChangedEvent
	if err := eventbus.Subscribe(bus, "spy",
		func(_ context.Context, e config.SettingChangedEvent) error { seen = e; return nil }); err != nil {
		t.Fatal(err)
	}

	company := id.ID("01890000-0000-7000-8000-00000000c001")
	reg := config.NewRegistry()
	reg.DeclareString(config.Def{Key: "sales.terms", Default: "",
		Scopes: []config.Scope{config.ScopeSystem, config.ScopeCompany}})
	settings, err := config.Open(ctx, st, config.Options{
		Registry: reg,
		Scopes:   config.FixedScopes(company, "", ""),
		Notifier: config.NewBusNotifier(bus, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(config.Bind(ctx, settings), config.ScopeCompany, company, "sales.terms", "net 30"); err != nil {
		t.Fatal(err)
	}
	if seen.ScopeID != company {
		t.Errorf("scope id = %q, want %q — a subscriber must know which company changed", seen.ScopeID, company)
	}
	if seen.AggregateID() != company {
		t.Errorf("AggregateID = %q, want the scope id", seen.AggregateID())
	}
}
